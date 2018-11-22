package filer

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"hash"
	"io"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"

	log "github.com/sirupsen/logrus"
	pb "git.stingr.net/stingray/advfiler/protos"
)

type UploadStatus struct {
	Digests map[string]string
	Size    int64
}

type Backend interface {
	Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error)
	List(ctx context.Context, path string) ([]string, error)
	Download(ctx context.Context, path string, limit int64) (DownloadResult, error)
	Delete(ctx context.Context, path string) error
}

type allHashes struct {
	sha1, md5 hash.Hash
}

func (s *allHashes) Write(p []byte) (n int, err error) {
	if n, err = s.md5.Write(p); err != nil {
		return n, err
	}
	return s.sha1.Write(p)
}

func (s *allHashes) toDigests() *pb.Digests {
	return &pb.Digests{
		Md5:  s.md5.Sum(nil),
		Sha1: s.sha1.Sum(nil),
	}
}

func newHashes() *allHashes {
	return &allHashes{
		sha1: sha1.New(),
		md5:  md5.New(),
	}
}

type badgerFiler struct {
	db  *badger.DB
	seq *badger.Sequence
}

func NewBadger(db *badger.DB) (Backend, error) {
	var err error
	result := badgerFiler{db: db}
	result.seq, err = db.GetSequence([]byte{66}, 10000)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

type FileInfo struct {
	Name          string
	ContentLength int64
	ModuleType    string
	RecvDigests   map[string]string
}

func makeChunkKey(id uint64) []byte {
	result := make([]byte, binary.MaxVarintLen64+1)
	result[0] = 1
	n := binary.PutUvarint(result[1:], id)
	return result[:n-1]
}

func makePrefixedKey(prefix byte, suffix string) []byte {
	result := make([]byte, len(suffix)+1)
	result[0] = prefix
	copy(result[1:], suffix)
	return result
}

func makeTempKey(name string) []byte { return makePrefixedKey(2, name) }
func makePermKey(name string) []byte { return makePrefixedKey(3, name) }

func (s *badgerFiler) insertChunk(iKey []byte, fi pb.FileInfo64, b []byte) (uint64, error) {
	next, err := s.seq.Next()
	if err != nil {
		return 0, err
	}
	fi.Chunks = append(fi.Chunks, next)
	chunkKey := makeChunkKey(next)

	iValue, err := proto.Marshal(&fi)
	if err != nil {
		return 0, nil
	}

	err = s.db.Update(func(tx *badger.Txn) error {
		if err := tx.Set(chunkKey, b); err != nil {
			return err
		}
		return tx.Set(iKey, iValue)
	})
	return next, err
}

func deleteBadgerChunks(tx *badger.Txn, chunks []uint64) error {
	for _, v := range chunks {
		if err := tx.Delete(makeChunkKey(v)); err != nil {
			return err
		}
	}
	return nil
}

func (s *badgerFiler) deleteTemp(key []byte, chunks []uint64) error {
	return s.db.Update(func(tx *badger.Txn) error {
		if err := tx.Delete(key); err != nil {
			return err
		}
		return deleteBadgerChunks(tx, chunks)
	})
}

func maybeDeletePrevBadger(tx *badger.Txn, key []byte) error {
	var prev pb.FileInfo64
	if err := getFileInfo64(tx, key, &prev); err != nil {
		if err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	}
	return deleteBadgerChunks(tx, prev.Chunks)
}

func (s *badgerFiler) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error) {
	log.Infof("upload: %v", info)
	fi := pb.FileInfo64{
		ModuleType: info.ModuleType,
	}
	hashes := newHashes()
	const chunksize = 64 * 1024

	iKey := makeTempKey(info.Name)

	if info.ContentLength > 0 && info.ContentLength < 12*1024 {
		fi.InlineData = make([]byte, int(info.ContentLength))
		n, err := io.ReadFull(body, fi.InlineData)
		if err != nil {
			return UploadStatus{}, err
		}
		fi.Size_ += int64(n)
		hashes.Write(fi.InlineData)

		if info.ContentLength >= 0 && fi.Size_ != info.ContentLength {
			return UploadStatus{}, nil
		}
		fi.Digests = hashes.toDigests()
		stDigests := common.DigestsToMap(fi.Digests)
		if !common.CheckDigests(info.RecvDigests, stDigests) {
			return UploadStatus{}, fmt.Errorf("checksum mismatch")
		}
		fk := makePermKey(info.Name)
		fkValue, err := proto.Marshal(&fi)
		if err != nil {
			return UploadStatus{}, err
		}

		if err = s.db.Update(func(tx *badger.Txn) error {
			if err := maybeDeletePrevBadger(tx, fk); err != nil {
				return err
			}
			return tx.Set(fk, fkValue)
		}); err != nil {
			return UploadStatus{}, err
		}
		return UploadStatus{
			Digests: stDigests,
			Size:    fi.Size_,
		}, nil
	}

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				s.deleteTemp(iKey, fi.Chunks)
				return UploadStatus{}, err
			}
			break
		}
		buf = buf[0:n]

		fid, err := s.insertChunk(iKey, fi, buf)
		if err != nil {
			s.deleteTemp(iKey, fi.Chunks)
			return UploadStatus{}, err
		}
		hashes.Write(buf)
		fi.Size_ += int64(n)
		fi.Chunks = append(fi.Chunks, fid)
	}

	if info.ContentLength >= 0 && fi.Size_ != info.ContentLength {
		s.deleteTemp(iKey, fi.Chunks)
		return UploadStatus{}, nil
	}
	fi.Digests = hashes.toDigests()
	stDigests := common.DigestsToMap(fi.Digests)
	if !common.CheckDigests(info.RecvDigests, stDigests) {
		s.deleteTemp(iKey, fi.Chunks)
		return UploadStatus{}, fmt.Errorf("checksum mismatch")
	}

	fk := makePermKey(info.Name)
	fkValue, err := proto.Marshal(&fi)
	if err != nil {
		return UploadStatus{}, err
	}
	err = s.db.Update(func(tx *badger.Txn) error {
		if err := maybeDeletePrevBadger(tx, fk); err != nil {
			return err
		}
		if err := tx.Set(fk, fkValue); err != nil {
			return err
		}
		return tx.Delete(iKey)
	})
	if err != nil {
		s.deleteTemp(iKey, fi.Chunks)
		return UploadStatus{}, err
	}

	return UploadStatus{
		Digests: stDigests,
		Size:    fi.Size_,
	}, nil
}

func getFileInfo64(tx *badger.Txn, key []byte, fi *pb.FileInfo64) error {
	item, err := tx.Get(key)
	if err != nil {
		return err
	}
	return item.Value(func(v []byte) error {
		return proto.Unmarshal(v, fi)
	})
}

func (f *badgerFiler) Delete(ctx context.Context, path string) error {
	fileKey := makePermKey(path)
	return f.db.Update(func(tx *badger.Txn) error {
		var fi pb.FileInfo64
		if err := getFileInfo64(tx, fileKey, &fi); err != nil {
			return err
		}
		if err := tx.Delete(fileKey); err != nil {
			return err
		}
		return deleteBadgerChunks(tx, fi.Chunks)
	})
}

type badgerDownloadResult struct {
	fi pb.FileInfo64
	f  *badgerFiler
}

func (r badgerDownloadResult) Size() int64          { return r.fi.Size_ }
func (r badgerDownloadResult) ModuleType() string   { return r.fi.ModuleType }
func (r badgerDownloadResult) Digests() *pb.Digests { return r.fi.Digests }
func (r badgerDownloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, &r.fi, limit)
}

func (f *badgerFiler) writeChunks(ctx context.Context, w io.Writer, fi *pb.FileInfo64, limit int64) error {
	if fi.Size_ == 0 || limit == 0 {
		return nil
	}
	if len(fi.InlineData) > 0 {
		var id []byte
		if limit != -1 && limit < int64(len(fi.InlineData)) {
			id = fi.InlineData[:limit]
		} else {
			id = fi.InlineData
		}
		n, err := w.Write(id)
		if err != nil {
			return err
		}
		if limit != -1 {
			limit -= int64(n)
		}
		if limit == 0 {
			return nil
		}
	}
	for _, ch := range fi.Chunks {
		var data []byte
		err := f.db.View(func(tx *badger.Txn) error {
			item, err := tx.Get(makeChunkKey(ch))
			if err != nil {
				return err
			}
			data, err = item.ValueCopy(data)
			return err
		})
		if err != nil {
			return err
		}
		if limit != -1 && limit < int64(len(data)) {
			data = data[:limit]
		}
		n, err := w.Write(data)
		if limit != -1 {
			limit -= int64(n)
		}
		if err != nil {
			return err
		}
		if limit == 0 {
			return nil
		}
	}
	return nil
}

func (f *badgerFiler) Download(ctx context.Context, path string, limit int64) (DownloadResult, error) {
	fileKey := makePermKey(path)
	result := badgerDownloadResult{
		f: f,
	}
	if err := f.db.View(func(tx *badger.Txn) error {
		return getFileInfo64(tx, fileKey, &result.fi)
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (f *badgerFiler) List(ctx context.Context, path string) ([]string, error) {
	pfx := makePermKey(path)
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	var res []string
	if err := f.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()
		for iter.Seek(pfx); iter.ValidForPrefix(pfx); iter.Next() {
			res = append(res, string(iter.Item().Key()[1:]))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}
