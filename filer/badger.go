package filer

import (
	"context"
	"fmt"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"hash"
	"io"

	"github.com/dgraph-io/badger"
	"git.stingr.net/stingray/advfiler/common"
	"github.com/golang/protobuf/proto"

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

type FileInfo struct {
	Name          string
	ContentLength int64
	ModuleType    string
	RecvDigests map[string]string
}

func makeChunkKey(id uint64) []byte {
	result := make([]byte, binary.MaxVarintLen64 + 1)
	result[0] = 1
	n := binary.PutUvarint(result[1:], id)
	return result[:n-1]
}

func makePrefixedKey(prefix byte, suffix string) []byte {
	result := make([]byte, len(suffix) + 1)
	result[0] = prefix
	copy(result[1:], suffix)
	return result
}

func makeTempKey(name string) []byte { return makePrefixedKey(2, name)}
func makePermKey(name string) []byte { return makePrefixedKey(3, name)}

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

func (s *badgerFiler) deleteTemp(key []byte, chunks []uint64) error {
	return s.db.Update(func(tx *badger.Txn) error {
		if err := tx.Delete(key); err != nil { return err }
		for _, v := range chunks {
			if err := tx.Delete(makeChunkKey(v)); err != nil { return err }
		}
		return nil
	})
}

func (s *badgerFiler) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error) {
	fi := pb.FileInfo64{
		ModuleType: info.ModuleType,
	}
	hashes := newHashes()
	const chunksize = 256 * 1024

	iKey := makeTempKey(info.Name)

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

