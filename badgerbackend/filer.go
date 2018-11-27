package badgerbackend

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"

	pb "git.stingr.net/stingray/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

var _ = log.Info

type Filer struct {
	db        *badger.DB
	seq, iseq *badger.Sequence
}

func NewFiler(db *badger.DB) (*Filer, error) {
	var err error
	result := Filer{db: db}
	result.seq, err = db.GetSequence([]byte{66}, 10000)
	if err != nil {
		return nil, err
	}
	result.iseq, err = db.GetSequence([]byte{67}, 10000)
	if err != nil {
		return nil, err
	}
	result.iseq.Next()
	go func() {
		log.Errorf("%v", result.db.RunValueLogGC(0.5))
	}()
	return &result, nil
}

func makeChunkKey(id uint64) []byte {
	return makeUintKey(1, id)
}

func makeInodeKey(id uint64) []byte {
	return makeUintKey(5, id)
}

func makeUintKey(pfx byte, id uint64) []byte {
	result := make([]byte, binary.MaxVarintLen64+1)
	result[0] = pfx
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

func makeChecksumKey(csum []byte) []byte {
	result := make([]byte, len(csum)+1)
	result[0] = 4
	copy(result[1:], csum)
	return result
}

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

	err = updateWithRetry(s.db, func(tx *badger.Txn) error {
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
	if err := getValue(tx, key, &prev); err != nil {
		if err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	}
	return deleteBadgerChunks(tx, prev.Chunks)
}

type chunkingWriter struct {
	f          *Filer
	tempKey    []byte
	inlineData []byte
	chunks     pb.ChunkList
}

func (s *chunkingWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if len(s.inlineData) == 0 {
		s.inlineData = append(s.inlineData, b...)
		return len(b), nil
	}
	fid, err := s.f.insertChunk(s.tempKey, s.chunks, b)
	if err != nil {
		if len(s.chunks.Chunks) > 1 {
			s.f.deleteTemp(s.tempKey, s.chunks.Chunks)
		}
		return 0, err
	}
	s.chunks.Chunks = append(s.chunks.Chunks, fid)
	return len(b), nil
}

func (s *Filer) linkToExistingFile(tx *badger.Txn, checksumKey []byte, cv *pb.ThisChecksum, fi *pb.FileInfo64) ([]byte, error) {
	var leafNode pb.FileInfo64
	var inode uint64
	var inodeKey []byte
	if cv.Filename != "" {
		var err error
		inode, err = s.iseq.Next()
		if err != nil {
			return nil, err
		}
		leafNode = *fi
		leafNode.ReferenceCount = 2
		if err = setValue(tx, checksumKey, &pb.ThisChecksum{
			Hardlink: inode,
		}); err != nil {
			return nil, err
		}
		inodeKey = makeInodeKey(inode)
	} else {
		inode = cv.Hardlink
		inodeKey = makeInodeKey(cv.Hardlink)
		if err := getValue(tx, inodeKey, &leafNode); err != nil {
			return nil, err
		}
		leafNode.ReferenceCount++
	}
	xvalue, err := proto.Marshal(&pb.FileInfo64{
		Hardlink: inode,
	})
	if err != nil {
		return nil, err
	}
	if err = tx.Set(makePermKey(cv.Filename), xvalue); err != nil {
		return nil, err
	}
	if err = setValue(tx, inodeKey, &leafNode); err != nil {
		return nil, err
	}
	if err = deleteBadgerChunks(tx, fi.Chunks); err != nil {
		return nil, err
	}
	return xvalue, nil
}

func (s *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	cw := chunkingWriter{
		f:       s,
		tempKey: makeTempKey(info.Name),
	}
	xw := bufio.NewWriterSize(&cw, 63*1024)

	bw := snappy.NewBufferedWriter(xw)
	hashes := common.NewHashes()
	mw := io.MultiWriter(bw, hashes)
	buf := make([]byte, 48*1024)
	n, err := io.CopyBuffer(mw, body, buf)
	if err != nil {
		return common.UploadStatus{}, err
	}
	if err = bw.Flush(); err != nil {
		return common.UploadStatus{}, err
	}
	if err = bw.Close(); err != nil {
		return common.UploadStatus{}, err
	}
	if err = xw.Flush(); err != nil {
		return common.UploadStatus{}, err
	}

	fi := pb.FileInfo64{
		ModuleType:  info.ModuleType,
		Size_:       n,
		InlineData:  cw.inlineData,
		Chunks:      cw.chunks.Chunks,
		Compression: pb.CT_SNAPPY,
	}

	var checksumKey []byte
	if len(fi.Chunks) != 0 || len(fi.InlineData) != 0 {
		fi.Digests = hashes.Digests()
		checksumKey = makeChecksumKey(fi.Digests.Sha256)
	}

	fkValue, err := proto.Marshal(&fi)
	if err != nil {
		return common.UploadStatus{}, err
	}
	permKey := makePermKey(info.Name)
	var hardlinked bool

	err = updateWithRetry(s.db, func(tx *badger.Txn) error {
		hardlinked = false
		xvalue := fkValue
		if err := maybeDeletePrevBadger(tx, permKey); err != nil {
			return err
		}
		if len(fi.InlineData) > 1 && len(checksumKey) != 0 {
			var cv pb.ThisChecksum
			err := getValue(tx, checksumKey, &cv)
			if err != nil && err != badger.ErrKeyNotFound {
				return err
			}

			if err == nil {
				xvalue, err = s.linkToExistingFile(tx, checksumKey, &cv, &fi)
				if err != nil {
					return err
				}
				hardlinked = true
			} else {
				cv.Filename = info.Name
				dupb, _ := proto.Marshal(&cv)
				if err = tx.Set(checksumKey, dupb); err != nil {
					return err
				}
			}
		}
		if err := tx.Set(permKey, xvalue); err != nil {
			return err
		}
		if len(fi.Chunks) != 0 {
			if err = tx.Delete(cw.tempKey); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return common.UploadStatus{}, err
	}

	return common.UploadStatus{
		Digests: common.DigestsToMap(hashes.Digests()),
		Size:    n,
		Hardlinked: hardlinked,
	}, nil

}

func getValue(tx *badger.Txn, key []byte, msg proto.Message) error {
	item, err := tx.Get(key)
	if err != nil {
		return err
	}
	return item.Value(func(v []byte) error {
		return proto.Unmarshal(v, msg)
	})
}

func setValue(tx *badger.Txn, key []byte, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return tx.Set(key, b)
}

func (f *Filer) Delete(ctx context.Context, path string) error {
	fileKey := makePermKey(path)
	return f.db.Update(func(tx *badger.Txn) error {
		var fi pb.FileInfo64
		if err := getValue(tx, fileKey, &fi); err != nil {
			return err
		}
		if err := tx.Delete(fileKey); err != nil {
			return err
		}
		return deleteBadgerChunks(tx, fi.Chunks)
	})
}

type downloadResult struct {
	fi  pb.FileInfo64
	f   *Filer
	buf []byte
}

func (r *downloadResult) Size() int64          { return r.fi.Size_ }
func (r *downloadResult) ModuleType() string   { return r.fi.ModuleType }
func (r *downloadResult) Digests() *pb.Digests { return r.fi.Digests }
func (r *downloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, &r.fi, limit)
}

func (r *downloadResult) Read(p []byte) (int, error) {
	// log.Infof("r: %d", len(p))
	if len(r.buf) == 0 && len(r.fi.Chunks) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) == 0 && len(r.fi.Chunks) != 0 {
		var err error
		r.buf, err = r.f.readChunk(r.fi.Chunks[0], nil)
		if err != nil {
			return 0, err
		}
		r.fi.Chunks = r.fi.Chunks[1:]
	}
	if len(r.buf) != 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}
	return 0, io.EOF
}

func (f *Filer) readChunk(chunk uint64, data []byte) ([]byte, error) {
	ck := makeChunkKey(chunk)
	err := f.db.View(func(tx *badger.Txn) error {
		item, err := tx.Get(ck)
		if err != nil {
			return err
		}
		data, err = item.ValueCopy(data)
		return err
	})
	return data, err
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
		data, err := f.readChunk(ch, nil)
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
		if err := getValue(tx, fileKey, &result.fi); err != nil {
			return err
		}
		if result.fi.Hardlink != 0 {
			if err := getValue(tx, makeInodeKey(result.fi.Hardlink), &result.fi); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	result.buf = result.fi.InlineData
	// log.Infof("%q: id %d, chunks %d", path, len(result.buf), len(result.fi.Chunks))
	return &result, nil
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
