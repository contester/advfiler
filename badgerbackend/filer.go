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
	defer func() {
		if f.seq != nil { f.seq.Release() }
		if f.iseq != nil { f.iseq.Release() }
	}()
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

/*

Key prefixes:
1 - data chunks
2 - temporary chunk list
3 - directory entry
4 - checksum -> inode
5 - inode
6 - inode volatile attributes

*/

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

func unlinkInode(tx *badger.Txn, inode uint64, checksumKey []byte) error {
	ivKey := makeIVKey(inode)
	var ivAttr pb.InodeVolatileAttributes
	if err := getValue(tx, ivKey, &ivAttr); err != nil && err != badger.ErrKeyNotFound { return err }
	log.Infof("decref %d %d", inode, ivAttr.ReferenceCountMinus_1)
	if ivAttr.ReferenceCountMinus_1 == 0 {
		inodeKey := makeInodeKey(inode)
		var inodeValue pb.Inode
		if err := getValue(tx, inodeKey, &inodeValue); err != nil { return err }
		if len(checksumKey) == 0 {
			checksum := inodeValue.GetDigests().GetSha256()
			if len(checksum) == 0 {
				hasher := sha256.New()
				io.Copy(hasher, snappy.NewReader(bytes.NewReader(inodeValue.InlineData)))
				checksum = hasher.Sum(nil)
			}
			checksumKey = makeChecksumKey(checksum)
		}
		if err := tx.Delete(checksumKey); err != nil { return err }
		if err := tx.Delete(inodeKey); err != nil { return err }
		return deleteBadgerChunks(tx, inodeValue.Chunks)
	}
	ivAttr.ReferenceCountMinus_1--
	if ivAttr.ReferenceCountMinus_1 == 0 {
		return tx.Delete(ivKey)
	}
	return setValue(tx, ivKey, &ivAttr)
}

func linkInode(tx *badger.Txn, inode uint64) error {
	ivKey := makeIVKey(inode)
	var ivAttr pb.InodeVolatileAttributes
	if err := getValue(tx, ivKey, &ivAttr); err != nil && err != badger.ErrKeyNotFound { return err }
	ivAttr.ReferenceCountMinus_1++
	log.Infof("incref: %d %d", inode, ivAttr.ReferenceCountMinus_1)
	return setValue(tx, ivKey, &ivAttr)
}

// If we have inode with given checksumKey, unlink prev and link next.
func tryLink(tx *badger.Txn, permKey, checksumKey []byte, prev, next pb.DirectoryEntry) (bool, error) {
	var cv pb.ThisChecksum
	found, err := getValueEx(tx, checksumKey, &cv)
	if !found { return false, err }

	next.Inode = cv.Hardlink
	if prev.Inode != 0 {
		if prev.Inode == cv.Hardlink {
			log.Infof("same inode detected, not linking")
			if prev != next {
				if err := setValue(tx, permKey, &next); err != nil {
					return false, err
				}
			}
			return true, nil
		}
		if err := unlinkInode(tx, prev.Inode, nil); err != nil {
			return false, err
		}
	}

	// if cv.Hardlink == 0 fail

	if err := linkInode(tx, cv.Hardlink); err != nil {
		return false, err
	}
	next.Inode = cv.Hardlink
	if err := setValue(tx, permKey, &next); err != nil {
			return false, err
	}
	return true, nil
}

func (s *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	if info.Name == "" {
		return common.UploadStatus{}, errors.New("can't upload to empty file")
	}
	if strings.HasSuffix(info.Name, "/") {
		return common.UploadStatus{}, errors.New("can't upload to directory")
	}
	permKey := makePermKey(info.Name)
	dentryValue := pb.DirectoryEntry{
		LastModifiedTimestamp: info.TimestampUnix,
		ModuleType:            info.ModuleType,
	}


	if hdigest := info.RecvDigests["SHA-256"]; hdigest != "" && info.ContentLength != 0 {
		xdigest, err := base64.StdEncoding.DecodeString(hdigest)
		if err == nil {
			checksumKey := makeChecksumKey(xdigest)
			var hardlinked bool
			err = updateWithRetry(s.db, func(tx *badger.Txn) error {
				var prev pb.DirectoryEntry
				if _, err := getValueEx(tx, permKey, &prev); err != nil { return err }
				var err error
				hardlinked, err = tryLink(tx, permKey, checksumKey, prev, dentryValue)
				return err
			})
			if err != nil { return common.UploadStatus{}, err}
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

	inodeValue := pb.Inode{
		Size_:      n,
		InlineData: cw.inlineData,
		Chunks:     cw.chunks.Chunks,
	}

	stDigests := hashes.Digests()
	var checksumKey []byte
	if len(inodeValue.Chunks) != 0 {
		inodeValue.Digests = stDigests
	}

	if n != 0 {
		checksumKey = makeChecksumKey(stDigests.Sha256)
	}

	var hardlinked bool

	err = updateWithRetry(s.db, func(tx *badger.Txn) error {
		hardlinked = false
		var prev pb.DirectoryEntry
		_, err := getValueEx(tx, permKey, &prev)
		if err != nil { return err }

		if inodeValue.Size_ == 0 {
				if prev.Inode != 0 {
					if err = unlinkInode(tx, prev.Inode, nil); err != nil {
						return err
					}
				}
				if prev != dentryValue {
					if err = setValue(tx, permKey, &dentryValue); err != nil {
						return err
					}
				}
				return nil
		}

		hardlinked, err = tryLink(tx, permKey, checksumKey, prev, dentryValue)
		if err != nil { return err }

		if hardlinked {
			if len(inodeValue.Chunks) != 0 {
				if err = tx.Delete(cw.tempKey); err != nil {
					return err
				}
				return deleteBadgerChunks(tx, inodeValue.Chunks)
			}
			return nil
		}

		if prev.Inode != 0 {
			if err := unlinkInode(tx, prev.Inode, nil); err != nil {
				return err
			}
		}
		inode, err := s.iseq.Next()
		if err != nil {
			return err
		}
		if err := setValue(tx, makeInodeKey(inode), &inodeValue); err != nil {
			return err
		}
		ld := dentryValue
		ld.Inode = inode
		if err := setValue(tx, permKey, &ld); err != nil {
			return err
		}
		if err := setValue(tx, checksumKey, &pb.ThisChecksum{Hardlink: inode}); err != nil {
			return err
		}
		if len(inodeValue.Chunks) != 0 {
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

func getValueEx(tx *badger.Txn, key []byte, msg proto.Message) (bool, error) {
	switch err := getValue(tx, key, msg); err {
	case nil: return true, nil
	case badger.ErrKeyNotFound: return false, nil
	default:
		return false, err
	}
}

func setValue(tx *badger.Txn, key []byte, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return tx.Set(key, b)
}

func (f *Filer) Delete(ctx context.Context, path string) error {
	// log.Infof("delete: %q", path)
	fileKey := makePermKey(path)
	return f.db.Update(func(tx *badger.Txn) error {
		var dentry pb.DirectoryEntry
		if err := getValue(tx, fileKey, &dentry); err != nil { return err }
		log.Infof("delete: %q %d", path, dentry.Inode)
		if dentry.Inode != 0 {
			if err := unlinkInode(tx, dentry.Inode, nil); err != nil { return err }
		}
		return tx.Delete(fileKey)
	})
}

type downloadResult struct {
	dentry pb.DirectoryEntry
	inode  pb.Inode
	f    *Filer
	body io.Reader
}

type chunkResultReader struct {
	f      *Filer
	buf    []byte
	chunks []uint64
}

func (r *downloadResult) Size() int64                  { return r.inode.Size_ }
func (r *downloadResult) LastModifiedTimestamp() int64 { return r.dentry.LastModifiedTimestamp }
func (r *downloadResult) ModuleType() string           { return r.dentry.ModuleType }
func (r *downloadResult) Digests() *pb.Digests         { return r.inode.Digests }
func (r *downloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return nil
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

func (f *Filer) Download(ctx context.Context, path string, options common.DownloadOptions) (common.DownloadResult, error) {
	fileKey := makePermKey(path)
	result := downloadResult{
		f: f,
	}
	if err := f.db.View(func(tx *badger.Txn) error {
		if err := getValue(tx, fileKey, &result.dentry); err != nil {
			return err
		}
		if result.dentry.Inode != 0 {
			if err := getValue(tx, makeInodeKey(result.dentry.Inode), &result.inode); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	cdr := chunkResultReader{
		f:      f,
		buf:    result.inode.InlineData,
		chunks: result.inode.Chunks,
	}

	result.body = snappy.NewReader(&cdr)

	if result.inode.Digests == nil && len(result.inode.InlineData) > 0 {
		hashes := common.NewHashes()
		cr := snappy.NewReader(bytes.NewReader(result.inode.InlineData))
		if _, err := io.Copy(hashes, cr); err == nil {
			result.inode.Digests = hashes.Digests()
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
	ctx := r.Context()
	f.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()
		for iter.Rewind(); iter.Valid(); iter.Next() {
			log.Infof("iter: %q", iter.Item().Key())
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		return nil
	})
}
