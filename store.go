package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/dgraph-io/badger/v4"
	pb "github.com/contester/advfiler/protos"
	"google.golang.org/protobuf/proto"
)

// Key prefixes
const (
	prefixDirEntry  byte = 0x01
	prefixBlob      byte = 0x02
	prefixManifest  byte = 0x04

	subkeyInlineData byte = 0x00
	subkeyDirMeta    byte = 0x01

	subkeyBlobData    byte = 0x00
	subkeyBlobDigests byte = 0x01
	subkeyBlobHash    byte = 0x02

	// Break-even threshold: if (N-1)*size > blobOverheadB, externalize.
	blobOverheadB = 50
)

// dirDataKey returns the key for inline data stored under a directory entry.
// Format: 0x01 + path + 0x00 + 0x00
func dirDataKey(path string) []byte {
	k := make([]byte, 0, 1+len(path)+2)
	k = append(k, prefixDirEntry)
	k = append(k, []byte(path)...)
	k = append(k, 0x00, subkeyInlineData)
	return k
}

// dirMetaKey returns the key for directory entry metadata (DirectoryEntry proto).
// Format: 0x01 + path + 0x00 + 0x01
func dirMetaKey(path string) []byte {
	k := make([]byte, 0, 1+len(path)+2)
	k = append(k, prefixDirEntry)
	k = append(k, []byte(path)...)
	k = append(k, 0x00, subkeyDirMeta)
	return k
}

// extractPathFromDirMetaKey extracts the path from a dirMetaKey.
// Returns an error if the key doesn't have the correct format/suffix.
func extractPathFromDirMetaKey(k []byte) (string, error) {
	if len(k) < 3 {
		return "", fmt.Errorf("key too short")
	}
	if k[0] != prefixDirEntry {
		return "", fmt.Errorf("wrong prefix")
	}
	if k[len(k)-1] != subkeyDirMeta || k[len(k)-2] != 0x00 {
		return "", fmt.Errorf("not a dir meta key")
	}
	path := string(k[1 : len(k)-2])
	return path, nil
}

// blobDataKey returns the key for blob data (external).
// Format: 0x02 + hash(32) + 0x00
func blobDataKey(hash []byte) []byte {
	k := make([]byte, 0, 1+len(hash)+1)
	k = append(k, prefixBlob)
	k = append(k, hash...)
	k = append(k, subkeyBlobData)
	return k
}

// blobDigestsKey returns the key for blob digests.
// Format: 0x02 + hash(32) + 0x01
func blobDigestsKey(hash []byte) []byte {
	k := make([]byte, 0, 1+len(hash)+1)
	k = append(k, prefixBlob)
	k = append(k, hash...)
	k = append(k, subkeyBlobDigests)
	return k
}

// blobHashEntryKey returns the key for the HashEntry proto.
// Format: 0x02 + hash(32) + 0x02
func blobHashEntryKey(hash []byte) []byte {
	k := make([]byte, 0, 1+len(hash)+1)
	k = append(k, prefixBlob)
	k = append(k, hash...)
	k = append(k, subkeyBlobHash)
	return k
}

// manifestKey returns the key for a manifest entry.
// Format: 0x04 + key
func manifestKey(key string) []byte {
	k := make([]byte, 0, 1+len(key))
	k = append(k, prefixManifest)
	k = append(k, []byte(key)...)
	return k
}

// FileInfo holds the metadata for a file upload.
type FileInfo struct {
	Name, ModuleType    string
	ContentLength       int64
	TimestampUnix       int64
	RecvDigests         map[string]string
	Blake3Hash          []byte
}

// UploadStatus is returned after a successful upload.
type UploadStatus struct {
	Digests    map[string]string
	Size       int64
	Hardlinked bool
}

// DownloadResult holds data returned from a Download operation.
type DownloadResult struct {
	Size                  int64
	ModuleType            string
	LastModifiedTimestamp int64
	Digests               Digests
	Body                  io.ReadSeeker
}

// Store is the content-addressable file store backed by a Badger database.
type Store struct {
	db       *badger.DB
	stopChan chan struct{}
	doneChan chan struct{}
}

// NewStore creates a new Store using the provided Badger DB and starts a GC goroutine.
func NewStore(db *badger.DB) *Store {
	s := &Store{
		db:       db,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
	go s.gcLoop()
	return s
}

func (s *Store) gcLoop() {
	defer close(s.doneChan)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			for {
				if err := s.db.RunValueLogGC(0.5); err != nil {
					break
				}
			}
		}
	}
}

// Close stops the GC goroutine.
func (s *Store) Close() {
	close(s.stopChan)
	<-s.doneChan
}

// getProto is a generic helper to read and unmarshal a proto message from a Badger transaction.
func getProto[T any, PT interface {
	*T
	proto.Message
}](tx *badger.Txn, key []byte) (PT, error) {
	item, err := tx.Get(key)
	if err != nil {
		return nil, err
	}
	var msg T
	pt := PT(&msg)
	err = item.Value(func(val []byte) error {
		return proto.Unmarshal(val, pt)
	})
	if err != nil {
		return nil, err
	}
	return pt, nil
}

// setProto marshals and stores a proto message in a Badger transaction.
func setProto(tx *badger.Txn, key []byte, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return tx.Set(key, b)
}

// shouldExternalize returns true if (numPaths-1)*dataSize > blobOverheadB.
func shouldExternalize(numPaths int, dataSize int64) bool {
	return int64(numPaths-1)*dataSize > blobOverheadB
}

// Upload stores data and metadata for a file using content-addressable storage.
func (s *Store) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error) {
	// Buffer the body while hashing it.
	hashes := NewHashes()
	data, err := io.ReadAll(io.TeeReader(body, hashes))
	if err != nil {
		return UploadStatus{}, fmt.Errorf("reading body: %w", err)
	}
	digests := hashes.Digests()

	// Verify Blake3Hash if provided (transit corruption check).
	if len(info.Blake3Hash) > 0 {
		if !bytes.Equal(digests.Blake3, info.Blake3Hash) {
			return UploadStatus{}, fmt.Errorf("blake3 hash mismatch: transit corruption detected")
		}
	}

	blake3Hash := digests.Blake3
	dataSize := int64(len(data))

	// Set timestamp if zero.
	if info.TimestampUnix == 0 {
		info.TimestampUnix = time.Now().Unix()
	}

	var hardlinked bool

	err = s.db.Update(func(tx *badger.Txn) error {
		// Check if this path already exists (overwrite scenario).
		existing, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(info.Name))
		if err != nil && err != badger.ErrKeyNotFound {
			return fmt.Errorf("checking existing entry: %w", err)
		}
		if err == nil && existing != nil {
			// Path exists: unlink the old hash, delete old inline data if applicable.
			if !existing.GetExternal() {
				if delErr := tx.Delete(dirDataKey(info.Name)); delErr != nil && delErr != badger.ErrKeyNotFound {
					return fmt.Errorf("deleting old inline data: %w", delErr)
				}
			}
			if len(existing.GetBlake3Hash()) > 0 {
				if ulErr := unlinkHash(tx, existing.GetBlake3Hash(), info.Name); ulErr != nil {
					return fmt.Errorf("unlinking old hash: %w", ulErr)
				}
			}
		}

		// Look up HashEntry for this blob.
		he, err := getProto[pb.HashEntry](tx, blobHashEntryKey(blake3Hash))
		if err != nil && err != badger.ErrKeyNotFound {
			return fmt.Errorf("looking up hash entry: %w", err)
		}

		if err == badger.ErrKeyNotFound {
			// First upload of this hash: store inline.
			dirEntry := &pb.DirectoryEntry{
				Blake3Hash:            blake3Hash,
				ModuleType:            info.ModuleType,
				LastModifiedTimestamp: info.TimestampUnix,
				Digests:               digests.ToProto(),
				DataSize:              dataSize,
				External:              false,
			}
			if setErr := setProto(tx, dirMetaKey(info.Name), dirEntry); setErr != nil {
				return fmt.Errorf("writing dir meta: %w", setErr)
			}
			if setErr := tx.Set(dirDataKey(info.Name), data); setErr != nil {
				return fmt.Errorf("writing inline data: %w", setErr)
			}
			newHE := &pb.HashEntry{
				State: &pb.HashEntry_InlinePaths{
					InlinePaths: &pb.PathList{Paths: []string{info.Name}},
				},
			}
			if setErr := setProto(tx, blobHashEntryKey(blake3Hash), newHE); setErr != nil {
				return fmt.Errorf("writing hash entry: %w", setErr)
			}
			return nil
		}

		// HashEntry exists. Check state.
		switch s := he.GetState().(type) {
		case *pb.HashEntry_InlinePaths:
			// Add this path to the inline list.
			paths := append(s.InlinePaths.GetPaths(), info.Name)
			numPaths := len(paths)

			if shouldExternalize(numPaths, dataSize) {
				// Externalize: write blob data and digests once.
				if setErr := tx.Set(blobDataKey(blake3Hash), data); setErr != nil {
					return fmt.Errorf("writing blob data: %w", setErr)
				}
				if setErr := setProto(tx, blobDigestsKey(blake3Hash), digests.ToProto()); setErr != nil {
					return fmt.Errorf("writing blob digests: %w", setErr)
				}

				// Rewrite all existing dir entries to external and delete their inline data.
				for _, existingPath := range s.InlinePaths.GetPaths() {
					existingDE, deErr := getProto[pb.DirectoryEntry](tx, dirMetaKey(existingPath))
					if deErr != nil {
						return fmt.Errorf("reading dir entry for %s: %w", existingPath, deErr)
					}
					existingDE.External = true
					if setErr := setProto(tx, dirMetaKey(existingPath), existingDE); setErr != nil {
						return fmt.Errorf("updating dir entry for %s: %w", existingPath, setErr)
					}
					if delErr := tx.Delete(dirDataKey(existingPath)); delErr != nil && delErr != badger.ErrKeyNotFound {
						return fmt.Errorf("deleting inline data for %s: %w", existingPath, delErr)
					}
				}

				// Write the new dir entry as external.
				dirEntry := &pb.DirectoryEntry{
					Blake3Hash:            blake3Hash,
					ModuleType:            info.ModuleType,
					LastModifiedTimestamp: info.TimestampUnix,
					Digests:               digests.ToProto(),
					DataSize:              dataSize,
					External:              true,
				}
				if setErr := setProto(tx, dirMetaKey(info.Name), dirEntry); setErr != nil {
					return fmt.Errorf("writing new external dir entry: %w", setErr)
				}

				// Convert HashEntry to refcount.
				newHE := &pb.HashEntry{
					State: &pb.HashEntry_Refcount{
						Refcount: int64(numPaths),
					},
				}
				if setErr := setProto(tx, blobHashEntryKey(blake3Hash), newHE); setErr != nil {
					return fmt.Errorf("writing updated hash entry: %w", setErr)
				}
				hardlinked = true
			} else {
				// Keep inline, just add path.
				dirEntry := &pb.DirectoryEntry{
					Blake3Hash:            blake3Hash,
					ModuleType:            info.ModuleType,
					LastModifiedTimestamp: info.TimestampUnix,
					Digests:               digests.ToProto(),
					DataSize:              dataSize,
					External:              false,
				}
				if setErr := setProto(tx, dirMetaKey(info.Name), dirEntry); setErr != nil {
					return fmt.Errorf("writing dir meta (inline dup): %w", setErr)
				}
				if setErr := tx.Set(dirDataKey(info.Name), data); setErr != nil {
					return fmt.Errorf("writing inline data (dup): %w", setErr)
				}
				updatedHE := &pb.HashEntry{
					State: &pb.HashEntry_InlinePaths{
						InlinePaths: &pb.PathList{Paths: paths},
					},
				}
				if setErr := setProto(tx, blobHashEntryKey(blake3Hash), updatedHE); setErr != nil {
					return fmt.Errorf("writing updated hash entry: %w", setErr)
				}
			}

		case *pb.HashEntry_Refcount:
			// Already externalized: increment refcount, write external dir entry.
			dirEntry := &pb.DirectoryEntry{
				Blake3Hash:            blake3Hash,
				ModuleType:            info.ModuleType,
				LastModifiedTimestamp: info.TimestampUnix,
				Digests:               digests.ToProto(),
				DataSize:              dataSize,
				External:              true,
			}
			if setErr := setProto(tx, dirMetaKey(info.Name), dirEntry); setErr != nil {
				return fmt.Errorf("writing external dir entry: %w", setErr)
			}
			newHE := &pb.HashEntry{
				State: &pb.HashEntry_Refcount{
					Refcount: s.Refcount + 1,
				},
			}
			if setErr := setProto(tx, blobHashEntryKey(blake3Hash), newHE); setErr != nil {
				return fmt.Errorf("writing updated hash entry (refcount): %w", setErr)
			}
			hardlinked = true
		}

		return nil
	})

	if err != nil {
		return UploadStatus{}, err
	}

	return UploadStatus{
		Digests:    DigestsToMap(digests),
		Size:       dataSize,
		Hardlinked: hardlinked,
	}, nil
}

// unlinkHash removes a path from the HashEntry for the given blake3 hash.
// For inline (path list): removes the path from the list.
// For external (refcount): decrements the refcount; deletes the blob if it reaches zero.
func unlinkHash(tx *badger.Txn, blake3Hash []byte, path string) error {
	he, err := getProto[pb.HashEntry](tx, blobHashEntryKey(blake3Hash))
	if err == badger.ErrKeyNotFound {
		// Nothing to unlink.
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading hash entry: %w", err)
	}

	switch s := he.GetState().(type) {
	case *pb.HashEntry_InlinePaths:
		oldPaths := s.InlinePaths.GetPaths()
		newPaths := make([]string, 0, len(oldPaths))
		for _, p := range oldPaths {
			if p != path {
				newPaths = append(newPaths, p)
			}
		}
		if len(newPaths) == 0 {
			// Delete the hash entry entirely.
			if delErr := tx.Delete(blobHashEntryKey(blake3Hash)); delErr != nil && delErr != badger.ErrKeyNotFound {
				return fmt.Errorf("deleting hash entry: %w", delErr)
			}
		} else {
			updatedHE := &pb.HashEntry{
				State: &pb.HashEntry_InlinePaths{
					InlinePaths: &pb.PathList{Paths: newPaths},
				},
			}
			if setErr := setProto(tx, blobHashEntryKey(blake3Hash), updatedHE); setErr != nil {
				return fmt.Errorf("writing updated hash entry: %w", setErr)
			}
		}

	case *pb.HashEntry_Refcount:
		newRC := s.Refcount - 1
		if newRC <= 0 {
			// Delete blob data and digests.
			if delErr := tx.Delete(blobDataKey(blake3Hash)); delErr != nil && delErr != badger.ErrKeyNotFound {
				return fmt.Errorf("deleting blob data: %w", delErr)
			}
			if delErr := tx.Delete(blobDigestsKey(blake3Hash)); delErr != nil && delErr != badger.ErrKeyNotFound {
				return fmt.Errorf("deleting blob digests: %w", delErr)
			}
			if delErr := tx.Delete(blobHashEntryKey(blake3Hash)); delErr != nil && delErr != badger.ErrKeyNotFound {
				return fmt.Errorf("deleting hash entry: %w", delErr)
			}
		} else {
			newHE := &pb.HashEntry{
				State: &pb.HashEntry_Refcount{
					Refcount: newRC,
				},
			}
			if setErr := setProto(tx, blobHashEntryKey(blake3Hash), newHE); setErr != nil {
				return fmt.Errorf("writing updated hash entry: %w", setErr)
			}
		}
	}

	return nil
}

// Download retrieves a file by path and calls fn with the result.
// Returns fs.ErrNotExist if the path is not found.
func (s *Store) Download(ctx context.Context, path string, fn func(DownloadResult) error) error {
	return s.db.View(func(tx *badger.Txn) error {
		de, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(path))
		if err == badger.ErrKeyNotFound {
			return fs.ErrNotExist
		}
		if err != nil {
			return fmt.Errorf("reading dir entry: %w", err)
		}

		dr := DownloadResult{
			Size:                  de.GetDataSize(),
			ModuleType:            de.GetModuleType(),
			LastModifiedTimestamp: de.GetLastModifiedTimestamp(),
		}

		if de.GetExternal() {
			// Load digests from blob.
			digProto, err := getProto[pb.Digests](tx, blobDigestsKey(de.GetBlake3Hash()))
			if err != nil && err != badger.ErrKeyNotFound {
				return fmt.Errorf("reading blob digests: %w", err)
			}
			if err == nil {
				dr.Digests = DigestsFromProto(digProto)
			}

			// Load blob data.
			dataItem, err := tx.Get(blobDataKey(de.GetBlake3Hash()))
			if err != nil {
				return fmt.Errorf("reading blob data: %w", err)
			}
			return dataItem.Value(func(v []byte) error {
				buf := make([]byte, len(v))
				copy(buf, v)
				dr.Body = bytes.NewReader(buf)
				return fn(dr)
			})
		}

		// Inline data.
		dr.Digests = DigestsFromProto(de.GetDigests())
		dataItem, err := tx.Get(dirDataKey(path))
		if err != nil {
			return fmt.Errorf("reading inline data: %w", err)
		}
		return dataItem.Value(func(v []byte) error {
			buf := make([]byte, len(v))
			copy(buf, v)
			dr.Body = bytes.NewReader(buf)
			return fn(dr)
		})
	})
}

// Delete unlinks the hash and removes all directory entries for a path.
func (s *Store) Delete(ctx context.Context, path string) error {
	return s.db.Update(func(tx *badger.Txn) error {
		de, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(path))
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading dir entry: %w", err)
		}

		if !de.GetExternal() {
			if delErr := tx.Delete(dirDataKey(path)); delErr != nil && delErr != badger.ErrKeyNotFound {
				return fmt.Errorf("deleting inline data: %w", delErr)
			}
		}

		if len(de.GetBlake3Hash()) > 0 {
			if ulErr := unlinkHash(tx, de.GetBlake3Hash(), path); ulErr != nil {
				return fmt.Errorf("unlinking hash: %w", ulErr)
			}
		}

		if delErr := tx.Delete(dirMetaKey(path)); delErr != nil && delErr != badger.ErrKeyNotFound {
			return fmt.Errorf("deleting dir meta: %w", delErr)
		}

		return nil
	})
}

// List returns all paths stored under the given prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var result []string

	// Build the key prefix to scan (0x01 + prefix).
	scanPrefix := append([]byte{prefixDirEntry}, []byte(prefix)...)

	err := s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Seek(scanPrefix); it.ValidForPrefix(scanPrefix); it.Next() {
			k := it.Item().KeyCopy(nil)
			path, err := extractPathFromDirMetaKey(k)
			if err != nil {
				// Not a meta key (could be data subkey), skip.
				continue
			}
			result = append(result, path)
		}
		return nil
	})

	return result, err
}

// Wipe deletes all files in the store.
func (s *Store) Wipe(ctx context.Context) error {
	paths, err := s.List(ctx, "")
	if err != nil {
		return fmt.Errorf("listing files: %w", err)
	}
	for _, path := range paths {
		if err := s.Delete(ctx, path); err != nil {
			return fmt.Errorf("deleting %s: %w", path, err)
		}
	}
	return nil
}

// GetManifest retrieves a manifest by key.
func (s *Store) GetManifest(ctx context.Context, key string) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *badger.Txn) error {
		item, err := tx.Get(manifestKey(key))
		if err == badger.ErrKeyNotFound {
			return fs.ErrNotExist
		}
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			result = make([]byte, len(v))
			copy(result, v)
			return nil
		})
	})
	return result, err
}

// SetManifest stores a manifest by key.
func (s *Store) SetManifest(ctx context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Set(manifestKey(key), value)
	})
}

// ListManifests returns all manifest keys under the given prefix.
func (s *Store) ListManifests(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	scanPrefix := append([]byte{prefixManifest}, []byte(prefix)...)

	err := s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Seek(scanPrefix); it.ValidForPrefix(scanPrefix); it.Next() {
			k := it.Item().KeyCopy(nil)
			// Strip the prefixManifest byte.
			if len(k) < 1 || k[0] != prefixManifest {
				continue
			}
			result = append(result, string(k[1:]))
		}
		return nil
	})

	return result, err
}
