package badgerbackend

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

type Filer struct {
	db  *badger.DB
	seq *badger.Sequence
}

func NewFiler(db *badger.DB) (*Filer, error) {
	var err error
	result := Filer{db: db}
	result.seq, err = db.GetSequence([]byte{66}, 10000)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func makeChunkKey(id uint64) []byte {
	result := make([]byte, binary.MaxVarintLen64+1)
	result[0] = 1
	n := binary.PutUvarint(result[1:], id)
	return result[:n+1]
}

func makePrefixedKey(prefix byte, suffix string) []byte {
	result := make([]byte, len(suffix)+1)
	result[0] = prefix
	copy(result[1:], suffix)
	return result
}

func makeTempKey(name string) []byte { return makePrefixedKey(2, name) }
func makePermKey(name string) []byte { return makePrefixedKey(3, name) }

func (s *Filer) insertChunk(iKey []byte, temp pb.ChunkList, b []byte) (uint64, error) {
	next, err := s.seq.Next()
	if err != nil {
		return 0, err
	}
	temp.Chunks = append(temp.Chunks, next)
	chunkKey := makeChunkKey(next)

	iValue, err := proto.Marshal(&temp)
	if err != nil {
		return 0, nil
	}

	err = s.db.Update(func(tx *badger.Txn) error {
		if len(temp.Chunks) == 1 {
			item, err := tx.Get(iKey)
			if err != nil {
				if err != badger.ErrKeyNotFound {
					return err
				}
			} else {
				var prev pb.ChunkList
				item.Value(func(v []byte) error {
					return proto.Unmarshal(v, &prev)
				})
				deleteBadgerChunks(tx, prev.Chunks)
			}
		}
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

func (s *Filer) deleteTemp(key []byte, chunks []uint64) error {
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

func (s *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	fi := pb.FileInfo64{
		ModuleType: info.ModuleType,
	}
	if info.ModuleType != "" {
		log.Infof("%v", info.ModuleType)
	}
	hashes := common.NewHashes()
	const chunksize = 64 * 1024

	fk := makePermKey(info.Name)

	var stDigests map[string]string

	if info.ContentLength > 0 && info.ContentLength < 12*1024 {
		fi.InlineData = make([]byte, int(info.ContentLength))
		n, err := io.ReadFull(body, fi.InlineData)
		if err != nil {
			return common.UploadStatus{}, err
		}
		fi.Size_ += int64(n)
		hashes.Write(fi.InlineData)

		if info.ContentLength >= 0 && fi.Size_ != info.ContentLength {
			return common.UploadStatus{}, nil
		}
		if fi.Size_ != 0 {
			fi.Digests = hashes.Digests()
			stDigests = common.DigestsToMap(fi.Digests)
			if !common.CheckDigests(info.RecvDigests, stDigests) {
				return common.UploadStatus{}, fmt.Errorf("checksum mismatch")
			}
		}
		fk := makePermKey(info.Name)
		fkValue, err := proto.Marshal(&fi)
		if err != nil {
			return common.UploadStatus{}, err
		}

		if err = s.db.Update(func(tx *badger.Txn) error {
			if err := maybeDeletePrevBadger(tx, fk); err != nil {
				return err
			}
			return tx.Set(fk, fkValue)
		}); err != nil {
			return common.UploadStatus{}, err
		}
		return common.UploadStatus{
			Digests: stDigests,
			Size:    fi.Size_,
		}, nil
	}

	iKey := makeTempKey(info.Name)
	var temp pb.ChunkList
	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				s.deleteTemp(iKey, fi.Chunks)
				return common.UploadStatus{}, err
			}
			break
		}
		buf = buf[0:n]

		fid, err := s.insertChunk(iKey, temp, buf)
		if err != nil {
			s.deleteTemp(iKey, temp.Chunks)
			return common.UploadStatus{}, err
		}
		hashes.Write(buf)
		fi.Size_ += int64(n)
		temp.Chunks = append(temp.Chunks, fid)
	}
	fi.Chunks = temp.Chunks
	if info.ContentLength >= 0 && fi.Size_ != info.ContentLength {
		s.deleteTemp(iKey, temp.Chunks)
		return common.UploadStatus{}, nil
	}
	if fi.Size_ != 0 {
		fi.Digests = hashes.Digests()
		stDigests = common.DigestsToMap(fi.Digests)
		if !common.CheckDigests(info.RecvDigests, stDigests) {
			s.deleteTemp(iKey, fi.Chunks)
			return common.UploadStatus{}, fmt.Errorf("checksum mismatch")
		}
	}

	fkValue, err := proto.Marshal(&fi)
	if err != nil {
		return common.UploadStatus{}, err
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
		return common.UploadStatus{}, err
	}

	return common.UploadStatus{
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

func (f *Filer) Delete(ctx context.Context, path string) error {
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

type downloadResult struct {
	fi pb.FileInfo64
	f  *Filer
}

func (r downloadResult) Size() int64          { return r.fi.Size_ }
func (r downloadResult) ModuleType() string   { return r.fi.ModuleType }
func (r downloadResult) Digests() *pb.Digests { return r.fi.Digests }
func (r downloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, &r.fi, limit)
}

func (f *Filer) writeChunks(ctx context.Context, w io.Writer, fi *pb.FileInfo64, limit int64) error {
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

func (f *Filer) Download(ctx context.Context, path string) (common.DownloadResult, error) {
	fileKey := makePermKey(path)
	result := downloadResult{
		f: f,
	}
	if err := f.db.View(func(tx *badger.Txn) error {
		return getFileInfo64(tx, fileKey, &result.fi)
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (f *Filer) List(ctx context.Context, path string) ([]string, error) {
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
