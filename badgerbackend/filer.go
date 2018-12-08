package badgerbackend

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"

	pb "git.stingr.net/stingray/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

var _ = log.Info

type Filer struct {
	db                 *badger.DB
	seq, iseq          *badger.Sequence
	stopChan, doneChan chan struct{}
}

func NewFiler(db *badger.DB) (*Filer, error) {
	var err error
	result := Filer{db: db, stopChan: make(chan struct{}, 1), doneChan: make(chan struct{}, 1)}
	result.seq, err = db.GetSequence([]byte{66}, 10000)
	if err != nil {
		return nil, err
	}
	result.iseq, err = db.GetSequence([]byte{67}, 10000)
	if err != nil {
		return nil, err
	}
	result.iseq.Next()
	go result.run()
	return &result, nil
}

func (f *Filer) Close() {
	close(f.stopChan)
	<-f.doneChan
}

func (f *Filer) run() {
	defer close(f.doneChan)
	for {
		select {
		case <-f.stopChan:
			return
		case <-time.After(time.Minute * 60):
			err := f.db.RunValueLogGC(0.5)
			if err != nil {
				log.Infof("value log GC successful")
			} else {
				log.Infof("value log GC failed, %v", err)
			}
		}
	}
}

func makeChunkKey(id uint64) []byte {
	return makeUintKey(1, id)
}

func makeInodeKey(id uint64) []byte {
	return makeUintKey(5, id)
}

func makeIVKey(id uint64) []byte { return makeUintKey(6, id) }

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

func maybeWrapKNF(key []byte, err error) error {
	if err == badger.ErrKeyNotFound {
		return fmt.Errorf("key not found: %q", key)
	}
	return err
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
	log.Infof("value key: %q", key)
	var prev pb.FileInfo64
	if err := getValue(tx, key, &prev); err != nil {
		if err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	}
	if prev.Hardlink != 0 {
		var ivattr pb.InodeVolatileAttributes
		ivkey := makeIVKey(prev.Hardlink)
		if err := getValue(tx, ivkey, &ivattr); err != nil {
			return err
		}

		if ivattr.ReferenceCount <= 1 {
			var leafNode pb.FileInfo64
			leafNodeKey := makeInodeKey(prev.Hardlink)
			log.Infof("inode key: %d", prev.Hardlink)
			if err := getValue(tx, leafNodeKey, &leafNode); err != nil {
				return err
			}

			checksum := leafNode.GetDigests().GetSha256()
			if len(checksum) == 0 {
				hasher := sha256.New()
				io.Copy(hasher, snappy.NewReader(bytes.NewReader(leafNode.InlineData)))
				checksum = hasher.Sum(nil)
			}
			checksumKey := makeChecksumKey(checksum)
			prev.Chunks = leafNode.Chunks
			if err := tx.Delete(leafNodeKey); err != nil {
				return err
			}
			if err := tx.Delete(checksumKey); err != nil {
				return err
			}
			if err := tx.Delete(ivkey); err != nil {
				return err
			}
		} else {
			ivattr.ReferenceCount--
			return setValue(tx, ivkey, &ivattr)
		}
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

func (s *Filer) linkToExistingFileByName(tx *badger.Txn, checksumKey []byte, existingFileName string, newFileInfo *pb.FileInfo64) (uint64, error) {
	inode, err := s.iseq.Next()
	if err != nil {
		return 0, err
	}
	existingFileKey := makePermKey(existingFileName)
	var existingFileInfo pb.FileInfo64
	if err = getValue(tx, existingFileKey, &existingFileInfo); err != nil {
		return 0, err
	}
	existingFileTimestamp := existingFileInfo.LastModifiedTimestamp
	inodeKey := makeInodeKey(inode)
	ivKey := makeIVKey(inode)
	existingFileInfo.LastModifiedTimestamp = 0
	if err = setValue(tx, checksumKey, &pb.ThisChecksum{Hardlink: inode}); err != nil {
		return 0, err
	}
	if err = setValue(tx, inodeKey, &existingFileInfo); err != nil {
		return 0, err
	}
	if err = setValue(tx, ivKey, &pb.InodeVolatileAttributes{ReferenceCount: 2}); err != nil {
		return 0, err
	}
	if err = setValue(tx, existingFileKey, &pb.FileInfo64{Hardlink: inode, LastModifiedTimestamp: existingFileTimestamp}); err != nil {
		return 0, err
	}
	return inode, nil
}

func (s *Filer) linkToExistingFile(tx *badger.Txn, checksumKey []byte, cv *pb.ThisChecksum, fi *pb.FileInfo64) ([]byte, error) {
	var inode uint64
	log.Infof("link to: %v %v", cv, fi)
	if cv.Filename != "" {
		var err error
		if inode, err = s.linkToExistingFileByName(tx, checksumKey, cv.Filename, fi); err != nil {
			return nil, err
		}
	} else {
		inode = cv.Hardlink
		var ivAttr pb.InodeVolatileAttributes
		ivKey := makeIVKey(inode)
		if err := getValue(tx, ivKey, &ivAttr); err != nil {
			return nil, err
		}
		ivAttr.ReferenceCount++
		if err := setValue(tx, ivKey, &ivAttr); err != nil {
			return nil, err
		}
	}
	if err := deleteBadgerChunks(tx, fi.Chunks); err != nil {
		return nil, err
	}
	return proto.Marshal(&pb.FileInfo64{
		LastModifiedTimestamp: fi.LastModifiedTimestamp,
		Hardlink:              inode,
	})
}

func (s *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	if info.Name == "" {
		return common.UploadStatus{}, errors.New("can't upload to empty file")
	}
	if strings.HasSuffix(info.Name, "/") {
		return common.UploadStatus{}, errors.New("can't upload to directory")
	}
	permKey := makePermKey(info.Name)

	if hdigest := info.RecvDigests["SHA-256"]; hdigest != "" && info.ContentLength != 0 {
		xdigest, err := base64.StdEncoding.DecodeString(hdigest)
		if err == nil {
			checksumKey := makeChecksumKey(xdigest)
			var hardlinked bool
			fi := pb.FileInfo64{
				ModuleType:            info.ModuleType,
				Size_:                 info.ContentLength,
				Compression:           pb.CT_SNAPPY,
				LastModifiedTimestamp: info.TimestampUnix,
			}
			err = updateWithRetry(s.db, func(tx *badger.Txn) error {
				hardlinked = false
				var cv pb.ThisChecksum
				err := getValue(tx, checksumKey, &cv)
				if err != nil && err != badger.ErrKeyNotFound {
					return err
				}

				if err == nil {
					if err := maybeDeletePrevBadger(tx, permKey); err != nil {
						return err
					}
					xvalue, err := s.linkToExistingFile(tx, checksumKey, &cv, &fi)
					if err != nil {
						return err
					}
					if err := tx.Set(permKey, xvalue); err != nil {
						return err
					}
					hardlinked = true
				}
				return nil
			})
			if hardlinked {
				return common.UploadStatus{
					Digests:    info.RecvDigests,
					Size:       info.ContentLength,
					Hardlinked: true,
				}, nil
			}
		}
	}

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
		ModuleType:            info.ModuleType,
		Size_:                 n,
		InlineData:            cw.inlineData,
		Chunks:                cw.chunks.Chunks,
		Compression:           pb.CT_SNAPPY,
		LastModifiedTimestamp: info.TimestampUnix,
	}

	stDigests := hashes.Digests()
	var checksumKey []byte
	if len(fi.Chunks) != 0 {
		fi.Digests = stDigests
	}

	if len(fi.InlineData) != 0 {
		checksumKey = makeChecksumKey(stDigests.Sha256)
	}

	fkValue, err := proto.Marshal(&fi)
	if err != nil {
		return common.UploadStatus{}, err
	}
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
		Digests:    common.DigestsToMap(stDigests),
		Size:       n,
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
	log.Infof("delete: %q", path)
	fileKey := makePermKey(path)
	return f.db.Update(func(tx *badger.Txn) error {
		if err := maybeDeletePrevBadger(tx, fileKey); err != nil {
			return err
		}
		return tx.Delete(fileKey)
	})
}

type downloadResult struct {
	fi   pb.FileInfo64
	f    *Filer
	body io.Reader
}

type chunkResultReader struct {
	f      *Filer
	buf    []byte
	chunks []uint64
}

func (r *downloadResult) Size() int64                  { return r.fi.Size_ }
func (r *downloadResult) LastModifiedTimestamp() int64 { return r.fi.LastModifiedTimestamp }
func (r *downloadResult) ModuleType() string           { return r.fi.ModuleType }
func (r *downloadResult) Digests() *pb.Digests         { return r.fi.Digests }
func (r *downloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, &r.fi, limit)
}
func (r *downloadResult) Body() io.Reader {
	return r.body
}

func (r *chunkResultReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 && len(r.chunks) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) == 0 && len(r.chunks) != 0 {
		var err error
		r.buf, err = r.f.readChunk(r.chunks[0], nil)
		if err != nil {
			return 0, err
		}
		r.chunks = r.chunks[1:]
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

func (f *Filer) Download(ctx context.Context, path string, options common.DownloadOptions) (common.DownloadResult, error) {
	fileKey := makePermKey(path)
	result := downloadResult{
		f: f,
	}
	if err := f.db.View(func(tx *badger.Txn) error {
		if err := getValue(tx, fileKey, &result.fi); err != nil {
			return err
		}
		if result.fi.Hardlink != 0 {
			lms := result.fi.LastModifiedTimestamp
			if err := getValue(tx, makeInodeKey(result.fi.Hardlink), &result.fi); err != nil {
				return err
			}
			result.fi.LastModifiedTimestamp = lms
		}
		return nil
	}); err != nil {
		return nil, err
	}

	cdr := chunkResultReader{
		f:      f,
		buf:    result.fi.InlineData,
		chunks: result.fi.Chunks,
	}

	result.body = snappy.NewReader(&cdr)

	if result.fi.Digests == nil && len(result.fi.InlineData) > 0 {
		hashes := common.NewHashes()
		cr := snappy.NewReader(bytes.NewReader(result.fi.InlineData))
		if _, err := io.Copy(hashes, cr); err == nil {
			result.fi.Digests = hashes.Digests()
		}
	}
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
			if xv := string(iter.Item().Key()[1:]); xv != "" {
				res = append(res, xv)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func (f *Filer) DebugList(w http.ResponseWriter, r *http.Request) {
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	f.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()
		for ; iter.Valid(); iter.Next() {
			log.Infof("iter: %q", iter.Item().Key())
		}
		return nil
	})
}
