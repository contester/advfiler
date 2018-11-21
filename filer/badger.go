package filer

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"hash"
	"io"

	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
)

type Backend interface {
	Upload(ctx context.Context, info FileInfo, body io.Reader) error	
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
}

func (s *badgerFiler) insertChunk(iKey []byte, fi pb.FileInfo64, b []byte) (int64, error) {
	next, err := s.seq.Next()
	if err != nil {
		return 0, err
	}
	inext := int64(next)

	fi.Chunks = append(fi.Chunks, inext)

	chunkKey := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(chunkKey, inext)
	chunkKey = chunkKey[:n]
	ckStr := "c|" + string(chunkKey)

	iValue, err := proto.Marshal(&fi)
	if err != nil {
		return 0, nil
	}

	err = s.db.Update(func(tx *badger.Txn) error {
		if err := tx.Set([]byte(ckStr), b); err != nil {
			return err
		}
		return tx.Set(iKey, iValue)
	})
	return inext, err
}

func (s *badgerFiler) Upload(ctx context.Context, info FileInfo, body io.Reader) error {
	fi := pb.FileInfo64{
		ModuleType: info.ModuleType,
	}
	hashes := newHashes()
	const chunksize = 256 * 1024

	iKey := []byte("p|" + info.Name)

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				// f.deleteChunks(ctx, fi.Chunks)
				return err
			}
			break
		}
		buf = buf[0:n]

		fid, err := s.insertChunk(iKey, fi, buf)
		if err != nil {
			// f.deleteChunks(ctx, fi.Chunks)
			return err
		}
		hashes.Write(buf)
		fi.Size_ += int64(n)
		fi.Chunks = append(fi.Chunks, fid)
	}

	fkStr := "f|" + info.Name
	fkValue, err := proto.Marshal(&fi)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *badger.Txn) error {
		if err := tx.Set([]byte(fkStr), fkValue); err != nil {
			return err
		}
		return tx.Delete(iKey)
	})
}
