# Badger CAS Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace efstore + LevelDB storage with a single Badger v4 database using content-addressable storage, blake3 hashing, dynamic inline/external placement, and zero-copy serving.

**Architecture:** Single `Store` struct wrapping `*badger.DB` with compound keys (`\x00` delimited). Directory entries at prefix `0x01`, content blobs at prefix `0x02` (keyed by blake3-256 hash), manifests at prefix `0x04`. No backend interface — HTTP handlers call Store methods directly. Download is callback-based for zero-copy `item.Value` serving.

**Tech Stack:** Go 1.25, Badger v4, blake3 (lukechampine.com/blake3), protobuf, logrus, systemd integration

**Spec:** `docs/superpowers/specs/2026-05-27-badger-cas-redesign.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `protos/protos.proto` | Rewrite | Add DirectoryEntry, HashEntry, PathList; remove FileMetadata, Assets |
| `protos/protos.pb.go` | Regenerate | Generated from proto |
| `protos/protos.go` | Keep | go:generate directive |
| `store.go` | Create | Store struct, key builders, Upload/Download/Delete/List/Wipe/manifest methods, value log GC goroutine |
| `store_test.go` | Create | Tests for all Store operations |
| `hashes.go` | Create | Digests type, Hashes multi-writer (blake3+SHA256+SHA1+MD5), digest↔map conversion, digest↔proto conversion |
| `hashes_test.go` | Create | Tests for hash computation and digest conversion |
| `main.go` | Rewrite | New config struct, Badger open, Store init, HTTP wiring |
| `filer.go` | Rewrite | HTTP handlers adapted to callback-based Download, Store.Upload with FileInfo |
| `metadata.go` | Rewrite | metadataServer uses Store.GetManifest/SetManifest/ListManifests |
| `authchecker.go` | Keep | Unchanged |
| `common/` | Delete | Entire package eliminated |
| `efbackend/` | Delete | Entire package eliminated |
| `ldbackend/` | Delete | Entire package eliminated |
| `go.mod` | Rewrite | Add badger/v4, blake3; remove leveldb, efstore |
| `backup/main.go` | Keep | HTTP client, no backend dependency |

---

### Task 1: Proto Definitions

**Files:**
- Modify: `protos/protos.proto`
- Regenerate: `protos/protos.pb.go`

- [ ] **Step 1: Rewrite protos.proto**

Replace the current proto file with the new schema. Keep `Digests`, `AuthAction`, `Asset`, `TestRecord`, `TestingRecord` (unchanged). Add `DirectoryEntry`, `PathList`, `HashEntry`. Remove `FileMetadata`.

```proto
syntax = "proto3";

option go_package = "github.com/contester/advfiler/protos";

package protos;

message Digests {
    bytes sha1 = 1;
    bytes md5 = 2;
    bytes sha256 = 3;
}

message DirectoryEntry {
    bytes blake3_hash = 1;
    string module_type = 2;
    int64 last_modified_timestamp = 3;
    Digests digests = 4;
    int64 data_size = 5;
    bool external = 6;
}

message PathList {
    repeated string paths = 1;
}

message HashEntry {
    oneof state {
        PathList inline_paths = 1;
        int64 refcount = 2;
    }
}

enum AuthAction {
    A_NONE = 0;
    A_READ = 1;
    A_WRITE = 2;
}

message Asset {
    string name = 1;
    bool truncated = 2;
    bytes data = 3;
    int64 original_size = 4;
}

message TestRecord {
    int64 test_id = 1;
    Asset input = 2;
    Asset output = 3;
    Asset answer = 4;
    Asset tester_output = 5;
}

message TestingRecord {
    Asset solution = 1;
    repeated TestRecord test = 2;
}
```

- [ ] **Step 2: Regenerate protobuf Go code**

Run: `cd protos && go generate`

Expected: `protos.pb.go` regenerated with `DirectoryEntry`, `PathList`, `HashEntry` types.

- [ ] **Step 3: Verify proto compiles**

Run: `go build ./protos/`

Expected: Clean build, no errors.

- [ ] **Step 4: Commit**

```bash
git add protos/protos.proto protos/protos.pb.go
git commit -m "proto: add DirectoryEntry, HashEntry, PathList; remove FileMetadata"
```

---

### Task 2: Dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Update go.mod**

Remove the `replace` directive for efstore. Remove `stingr.net/go/efstore` and `github.com/syndtr/goleveldb` from requires. Add `github.com/dgraph-io/badger/v4` and `lukechampine.com/blake3`.

Note: Don't run `go mod tidy` yet — the code still imports the old packages. Just add the new dependencies so they're available for the next tasks:

```bash
go get github.com/dgraph-io/badger/v4@latest
go get lukechampine.com/blake3@latest
```

- [ ] **Step 2: Verify new deps are available**

Run: `grep -E 'badger/v4|blake3' go.mod`

Expected: Both dependencies appear in go.mod.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add badger v4 and blake3"
```

---

### Task 3: Hashes Module

**Files:**
- Create: `hashes.go`
- Create: `hashes_test.go`

This extracts and extends digest utilities from `common/hashes.go`. Adds blake3 to the multi-hasher.

- [ ] **Step 1: Write hashes_test.go**

```go
package main

import (
	"bytes"
	"testing"

	"lukechampine.com/blake3"
)

func TestHashesWrite(t *testing.T) {
	h := NewHashes()
	data := []byte("hello world")
	n, err := h.Write(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("wrote %d, want %d", n, len(data))
	}

	d := h.Digests()
	if len(d.Blake3) != 32 {
		t.Fatalf("blake3 length %d, want 32", len(d.Blake3))
	}
	if len(d.SHA256) != 32 {
		t.Fatalf("sha256 length %d, want 32", len(d.SHA256))
	}
	if len(d.SHA1) != 20 {
		t.Fatalf("sha1 length %d, want 20", len(d.SHA1))
	}
	if len(d.MD5) != 16 {
		t.Fatalf("md5 length %d, want 16", len(d.MD5))
	}

	expected := blake3.Sum256(data)
	if !bytes.Equal(d.Blake3, expected[:]) {
		t.Fatalf("blake3 mismatch")
	}
}

func TestDigestsToMap(t *testing.T) {
	h := NewHashes()
	h.Write([]byte("test"))
	d := h.Digests()
	m := DigestsToMap(d)
	if m == nil {
		t.Fatal("expected non-nil map")
	}
	if m["SHA-256"] == "" {
		t.Fatal("expected SHA-256 in map")
	}
	if m["SHA"] == "" {
		t.Fatal("expected SHA in map")
	}
	if m["MD5"] == "" {
		t.Fatal("expected MD5 in map")
	}
}

func TestDigestsRoundTrip(t *testing.T) {
	h := NewHashes()
	h.Write([]byte("roundtrip"))
	d := h.Digests()
	m := DigestsToMap(d)
	d2 := MapToDigests(m)
	if !bytes.Equal(d.SHA256, d2.SHA256) {
		t.Fatal("SHA256 roundtrip failed")
	}
	if !bytes.Equal(d.SHA1, d2.SHA1) {
		t.Fatal("SHA1 roundtrip failed")
	}
	if !bytes.Equal(d.MD5, d2.MD5) {
		t.Fatal("MD5 roundtrip failed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestHashes -v .`

Expected: FAIL — `NewHashes` not defined.

- [ ] **Step 3: Write hashes.go**

```go
package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"sort"
	"strings"

	pb "github.com/contester/advfiler/protos"
	"lukechampine.com/blake3"
)

type Digests struct {
	Blake3, SHA256, SHA1, MD5 []byte
}

type Hashes struct {
	blake3         hash.Hash
	sha256, sha1   hash.Hash
	md5            hash.Hash
}

func NewHashes() *Hashes {
	return &Hashes{
		blake3: blake3.New(32, nil),
		sha256: sha256.New(),
		sha1:   sha1.New(),
		md5:    md5.New(),
	}
}

func (s *Hashes) Write(p []byte) (int, error) {
	s.blake3.Write(p)
	s.sha256.Write(p)
	s.sha1.Write(p)
	return s.md5.Write(p)
}

func (s *Hashes) Digests() Digests {
	return Digests{
		Blake3: s.blake3.Sum(nil),
		SHA256: s.sha256.Sum(nil),
		SHA1:   s.sha1.Sum(nil),
		MD5:    s.md5.Sum(nil),
	}
}

func DigestsFromProto(s *pb.Digests) Digests {
	if s == nil {
		return Digests{}
	}
	return Digests{
		SHA256: s.GetSha256(),
		SHA1:   s.GetSha1(),
		MD5:    s.GetMd5(),
	}
}

func (d Digests) ToProto() *pb.Digests {
	return &pb.Digests{
		Sha256: d.SHA256,
		Sha1:   d.SHA1,
		Md5:    d.MD5,
	}
}

func maybeSetDigest(m map[string]string, name string, value []byte) {
	if len(value) > 0 {
		m[name] = base64.StdEncoding.EncodeToString(value)
	}
}

func DigestsToMap(d Digests) map[string]string {
	r := make(map[string]string)
	maybeSetDigest(r, "MD5", d.MD5)
	maybeSetDigest(r, "SHA", d.SHA1)
	maybeSetDigest(r, "SHA-256", d.SHA256)
	if len(r) == 0 {
		return nil
	}
	return r
}

func MapToDigests(m map[string]string) Digests {
	return Digests{
		MD5:    maybeGetDigest(m["MD5"]),
		SHA1:   maybeGetDigest(m["SHA"]),
		SHA256: maybeGetDigest(m["SHA-256"]),
	}
}

func maybeGetDigest(x string) []byte {
	r, _ := base64.StdEncoding.DecodeString(x)
	return r
}

func addDigests(h map[string][]string, digests map[string]string) {
	if len(digests) == 0 {
		return
	}
	dkeys := make([]string, 0, len(digests))
	for k := range digests {
		dkeys = append(dkeys, k)
	}
	sort.Strings(dkeys)
	dvals := make([]string, 0, len(dkeys))
	for _, k := range dkeys {
		dvals = append(dvals, k+"="+digests[k])
	}
	h.Add("Digest", strings.Join(dvals, ","))
	if md5, ok := digests["MD5"]; ok && md5 != "" {
		h.Add("Content-MD5", md5)
	}
}
```

Wait — `addDigests` takes `http.Header`, not `map[string][]string`. Let me fix this. Actually, `addDigests` is a helper used in `filer.go`. It should stay in filer.go or be adapted later. Let me keep `hashes.go` focused on the Digests type and conversion functions. The `addDigests` HTTP helper will remain in `filer.go`.

Remove the `addDigests` function from hashes.go — it belongs in filer.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestHashes -v . && go test -run TestDigests -v .`

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add hashes.go hashes_test.go
git commit -m "feat: add hashes module with blake3 multi-hasher"
```

---

### Task 4: Store — Key Builders and Types

**Files:**
- Create: `store.go`
- Create: `store_test.go`

Build the Store struct, key construction functions, and public types. No operations yet — just the skeleton and key layout.

- [ ] **Step 1: Write key builder tests in store_test.go**

```go
package main

import (
	"bytes"
	"testing"
)

func TestDirMetaKey(t *testing.T) {
	k := dirMetaKey("foo/bar")
	// 0x01 + "foo/bar" + \x00 + \x01
	expected := append([]byte{0x01}, []byte("foo/bar")...)
	expected = append(expected, 0x00, 0x01)
	if !bytes.Equal(k, expected) {
		t.Fatalf("got %x, want %x", k, expected)
	}
}

func TestDirDataKey(t *testing.T) {
	k := dirDataKey("a/b")
	expected := append([]byte{0x01}, []byte("a/b")...)
	expected = append(expected, 0x00, 0x00)
	if !bytes.Equal(k, expected) {
		t.Fatalf("got %x, want %x", k, expected)
	}
}

func TestBlobKey(t *testing.T) {
	hash := make([]byte, 32)
	hash[0] = 0xAB
	k := blobDataKey(hash)
	if k[0] != 0x02 {
		t.Fatalf("prefix %x, want 0x02", k[0])
	}
	if k[33] != 0x00 {
		t.Fatalf("suffix %x, want 0x00", k[33])
	}
	if len(k) != 34 {
		t.Fatalf("len %d, want 34", len(k))
	}
}

func TestBlobDigestsKey(t *testing.T) {
	hash := make([]byte, 32)
	k := blobDigestsKey(hash)
	if k[33] != 0x01 {
		t.Fatalf("suffix %x, want 0x01", k[33])
	}
}

func TestBlobHashEntryKey(t *testing.T) {
	hash := make([]byte, 32)
	k := blobHashEntryKey(hash)
	if k[33] != 0x02 {
		t.Fatalf("suffix %x, want 0x02", k[33])
	}
}

func TestManifestKey(t *testing.T) {
	k := manifestKey("prob/1")
	expected := append([]byte{0x04}, []byte("prob/1")...)
	if !bytes.Equal(k, expected) {
		t.Fatalf("got %x, want %x", k, expected)
	}
}

func TestExtractPathFromDirMetaKey(t *testing.T) {
	k := dirMetaKey("submit/1/2/source")
	path, ok := extractPathFromDirMetaKey(k)
	if !ok {
		t.Fatal("expected ok")
	}
	if path != "submit/1/2/source" {
		t.Fatalf("got %q, want %q", path, "submit/1/2/source")
	}
}

func TestExtractPathRejectsDataKey(t *testing.T) {
	k := dirDataKey("foo")
	_, ok := extractPathFromDirMetaKey(k)
	if ok {
		t.Fatal("should reject data key")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestDir -v . && go test -run TestBlob -v . && go test -run TestManifest -v . && go test -run TestExtract -v .`

Expected: FAIL — functions not defined.

- [ ] **Step 3: Write store.go skeleton**

```go
package main

import (
	"context"
	"io"
	"time"

	"github.com/dgraph-io/badger/v4"
	log "github.com/sirupsen/logrus"
)

const (
	prefixDir      = 0x01
	prefixBlob     = 0x02
	prefixManifest = 0x04

	subkeyData    = 0x00
	subkeyMeta    = 0x01
	subkeyDigests = 0x01
	subkeyHash    = 0x02
)

const blobOverheadB = 50

func dirMetaKey(path string) []byte {
	b := make([]byte, 1+len(path)+2)
	b[0] = prefixDir
	copy(b[1:], path)
	b[len(b)-2] = 0x00
	b[len(b)-1] = subkeyMeta
	return b
}

func dirDataKey(path string) []byte {
	b := make([]byte, 1+len(path)+2)
	b[0] = prefixDir
	copy(b[1:], path)
	b[len(b)-2] = 0x00
	b[len(b)-1] = subkeyData
	return b
}

func blobKey(hash []byte, suffix byte) []byte {
	b := make([]byte, 1+32+1)
	b[0] = prefixBlob
	copy(b[1:], hash[:32])
	b[33] = suffix
	return b
}

func blobDataKey(hash []byte) []byte    { return blobKey(hash, subkeyData) }
func blobDigestsKey(hash []byte) []byte { return blobKey(hash, subkeyDigests) }
func blobHashEntryKey(hash []byte) []byte { return blobKey(hash, subkeyHash) }

func manifestKey(key string) []byte {
	b := make([]byte, 1+len(key))
	b[0] = prefixManifest
	copy(b[1:], key)
	return b
}

func extractPathFromDirMetaKey(k []byte) (string, bool) {
	// Must be: 0x01 + path + \x00 + \x01
	if len(k) < 4 || k[0] != prefixDir || k[len(k)-1] != subkeyMeta || k[len(k)-2] != 0x00 {
		return "", false
	}
	return string(k[1 : len(k)-2]), true
}

type FileInfo struct {
	Name          string
	ContentLength int64
	ModuleType    string
	RecvDigests   map[string]string
	TimestampUnix int64
	Blake3Hash    []byte
}

type UploadStatus struct {
	Digests    map[string]string
	Size       int64
	Hardlinked bool
}

type DownloadResult struct {
	Size                 int64
	ModuleType           string
	LastModifiedTimestamp int64
	Digests              Digests
	Body                 io.ReadSeeker
}

type Store struct {
	db       *badger.DB
	stopChan chan struct{}
	doneChan chan struct{}
}

func NewStore(db *badger.DB) *Store {
	s := &Store{
		db:       db,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
	go s.gcLoop()
	return s
}

func (s *Store) Close() {
	close(s.stopChan)
	<-s.doneChan
}

func (s *Store) gcLoop() {
	defer close(s.doneChan)
	ticker := time.NewTicker(60 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			if err := s.db.RunValueLogGC(0.5); err == nil {
				log.Info("value log GC successful")
			}
		}
	}
}

func shouldExternalize(numPaths int, dataSize int64) bool {
	n := int64(numPaths)
	return (n-1)*dataSize > blobOverheadB
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestDir|TestBlob|TestManifest|TestExtract' -v .`

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add store.go store_test.go
git commit -m "feat: store skeleton with key builders and types"
```

---

### Task 5: Store — Upload and Download

**Files:**
- Modify: `store.go`
- Modify: `store_test.go`

Implement `Upload`, `Download`, `Delete`, `List` on `Store`. This is the core CAS logic.

- [ ] **Step 1: Write upload/download test**

Append to `store_test.go`:

```go
import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	opts := badger.DefaultOptions(dir).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	t.Cleanup(func() {
		s.Close()
		db.Close()
	})
	return s
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	info := FileInfo{Name: "test/file.txt", ContentLength: 5}
	body := strings.NewReader("hello")
	status, err := s.Upload(ctx, info, body)
	if err != nil {
		t.Fatal(err)
	}
	if status.Size != 5 {
		t.Fatalf("size %d, want 5", status.Size)
	}

	err = s.Download(ctx, "test/file.txt", func(dr DownloadResult) error {
		if dr.Size != 5 {
			t.Fatalf("download size %d, want 5", dr.Size)
		}
		data, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(data) != "hello" {
			t.Fatalf("data %q, want %q", data, "hello")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUploadDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := strings.Repeat("X", 100) // > blobOverheadB, externalizes at N=2
	for i := 0; i < 3; i++ {
		info := FileInfo{
			Name:          "file" + string(rune('A'+i)),
			ContentLength: int64(len(data)),
		}
		_, err := s.Upload(ctx, info, strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
	}

	// All three should be downloadable with same content
	for _, name := range []string{"fileA", "fileB", "fileC"} {
		err := s.Download(ctx, name, func(dr DownloadResult) error {
			got, _ := io.ReadAll(dr.Body)
			if string(got) != data {
				t.Fatalf("%s: data mismatch", name)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
	}
}

func TestDownloadNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Download(context.Background(), "nonexistent", func(dr DownloadResult) error {
		t.Fatal("should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUploadOverwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Upload(ctx, FileInfo{Name: "f", ContentLength: 3}, strings.NewReader("old"))
	s.Upload(ctx, FileInfo{Name: "f", ContentLength: 3}, strings.NewReader("new"))

	s.Download(ctx, "f", func(dr DownloadResult) error {
		got, _ := io.ReadAll(dr.Body)
		if string(got) != "new" {
			t.Fatalf("got %q, want %q", got, "new")
		}
		return nil
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestUpload|TestDownload' -v .`

Expected: FAIL — `Upload`, `Download` not defined.

- [ ] **Step 3: Implement Upload on Store**

Add to `store.go`:

```go
func (s *Store) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error) {
	hashes := NewHashes()
	data, err := io.ReadAll(io.TeeReader(body, hashes))
	if err != nil {
		return UploadStatus{}, err
	}

	computed := hashes.Digests()

	if len(info.Blake3Hash) > 0 && !bytes.Equal(computed.Blake3, info.Blake3Hash) {
		return UploadStatus{}, fmt.Errorf("blake3 hash mismatch: transit corruption")
	}

	if info.TimestampUnix == 0 {
		info.TimestampUnix = time.Now().Unix()
	}

	dataSize := int64(len(data))
	blake3Hash := computed.Blake3

	err = s.db.Update(func(tx *badger.Txn) error {
		// Handle overwrite: if path already exists, unlink old hash
		if oldEntry, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(info.Name)); err == nil {
			if err := s.unlinkHash(tx, oldEntry.Blake3Hash, info.Name, !oldEntry.External); err != nil {
				return err
			}
			if !oldEntry.External {
				tx.Delete(dirDataKey(info.Name))
			}
		}

		// Look up HashEntry for this content hash
		heKey := blobHashEntryKey(blake3Hash)
		he, heErr := getProto[pb.HashEntry](tx, heKey)

		switch {
		case heErr != nil:
			// First upload of this content — store inline
			newHE := &pb.HashEntry{}
			newHE.SetInlinePaths(&pb.PathList{Paths: []string{info.Name}})
			if err := setProto(tx, heKey, newHE); err != nil {
				return err
			}
			if err := tx.Set(dirDataKey(info.Name), data); err != nil {
				return err
			}
			return setProto(tx, dirMetaKey(info.Name), &pb.DirectoryEntry{
				Blake3Hash:           blake3Hash,
				ModuleType:           info.ModuleType,
				LastModifiedTimestamp: info.TimestampUnix,
				Digests:              computed.ToProto(),
				DataSize:             dataSize,
				External:             false,
			})

		case he.GetInlinePaths() != nil:
			// Pre-externalization: add path, maybe externalize
			paths := he.GetInlinePaths().Paths
			paths = append(paths, info.Name)

			if shouldExternalize(len(paths), dataSize) {
				// Write external blob and digests
				if err := tx.Set(blobDataKey(blake3Hash), data); err != nil {
					return err
				}
				if err := setProto(tx, blobDigestsKey(blake3Hash), computed.ToProto()); err != nil {
					return err
				}
				// Rewrite all existing inline entries to external
				for _, p := range paths[:len(paths)-1] {
					tx.Delete(dirDataKey(p))
					if existing, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(p)); err == nil {
						existing.External = true
						existing.Digests = nil
						setProto(tx, dirMetaKey(p), existing)
					}
				}
				// Write new entry as external
				if err := setProto(tx, dirMetaKey(info.Name), &pb.DirectoryEntry{
					Blake3Hash:           blake3Hash,
					ModuleType:           info.ModuleType,
					LastModifiedTimestamp: info.TimestampUnix,
					DataSize:             dataSize,
					External:             true,
				}); err != nil {
					return err
				}
				// Convert HashEntry to refcount
				newHE := &pb.HashEntry{}
				newHE.SetRefcount(int64(len(paths)))
				return setProto(tx, heKey, newHE)
			}

			// Stay inline
			if err := tx.Set(dirDataKey(info.Name), data); err != nil {
				return err
			}
			if err := setProto(tx, dirMetaKey(info.Name), &pb.DirectoryEntry{
				Blake3Hash:           blake3Hash,
				ModuleType:           info.ModuleType,
				LastModifiedTimestamp: info.TimestampUnix,
				Digests:              computed.ToProto(),
				DataSize:             dataSize,
				External:             false,
			}); err != nil {
				return err
			}
			newHE := &pb.HashEntry{}
			newHE.SetInlinePaths(&pb.PathList{Paths: paths})
			return setProto(tx, heKey, newHE)

		default:
			// Post-externalization: increment refcount
			rc := he.GetRefcount() + 1
			newHE := &pb.HashEntry{}
			newHE.SetRefcount(rc)
			if err := setProto(tx, heKey, newHE); err != nil {
				return err
			}
			return setProto(tx, dirMetaKey(info.Name), &pb.DirectoryEntry{
				Blake3Hash:           blake3Hash,
				ModuleType:           info.ModuleType,
				LastModifiedTimestamp: info.TimestampUnix,
				DataSize:             dataSize,
				External:             true,
			})
		}
	})

	return UploadStatus{
		Digests: DigestsToMap(computed),
		Size:    dataSize,
	}, err
}

func (s *Store) unlinkHash(tx *badger.Txn, hash []byte, path string, wasInline bool) error {
	heKey := blobHashEntryKey(hash)
	he, err := getProto[pb.HashEntry](tx, heKey)
	if err != nil {
		return nil // no hash entry, nothing to unlink
	}

	if pl := he.GetInlinePaths(); pl != nil {
		paths := pl.Paths
		for i, p := range paths {
			if p == path {
				paths = append(paths[:i], paths[i+1:]...)
				break
			}
		}
		if len(paths) == 0 {
			return tx.Delete(heKey)
		}
		newHE := &pb.HashEntry{}
		newHE.SetInlinePaths(&pb.PathList{Paths: paths})
		return setProto(tx, heKey, newHE)
	}

	rc := he.GetRefcount() - 1
	if rc <= 0 {
		tx.Delete(blobDataKey(hash))
		tx.Delete(blobDigestsKey(hash))
		return tx.Delete(heKey)
	}
	newHE := &pb.HashEntry{}
	newHE.SetRefcount(rc)
	return setProto(tx, heKey, newHE)
}
```

- [ ] **Step 4: Implement Download on Store**

Add to `store.go`:

```go
func (s *Store) Download(ctx context.Context, path string, fn func(DownloadResult) error) error {
	return s.db.View(func(tx *badger.Txn) error {
		entry, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(path))
		if err != nil {
			return fmt.Errorf("%w: %s", fs.ErrNotExist, path)
		}

		dr := DownloadResult{
			Size:                 entry.DataSize,
			ModuleType:           entry.ModuleType,
			LastModifiedTimestamp: entry.LastModifiedTimestamp,
		}

		var dataKey []byte
		if entry.External {
			dataKey = blobDataKey(entry.Blake3Hash)
			if digestsPb, err := getProto[pb.Digests](tx, blobDigestsKey(entry.Blake3Hash)); err == nil {
				dr.Digests = DigestsFromProto(digestsPb)
			}
		} else {
			dataKey = dirDataKey(path)
			dr.Digests = DigestsFromProto(entry.Digests)
		}

		item, err := tx.Get(dataKey)
		if err != nil {
			return fmt.Errorf("reading data for %s: %w", path, err)
		}

		return item.Value(func(v []byte) error {
			dr.Body = bytes.NewReader(v)
			return fn(dr)
		})
	})
}
```

- [ ] **Step 5: Add proto helper functions**

Add to `store.go`:

```go
func getProto[T any, PT interface {
	*T
	proto.Message
}](tx *badger.Txn, key []byte) (PT, error) {
	item, err := tx.Get(key)
	if err != nil {
		return nil, err
	}
	result := PT(new(T))
	err = item.Value(func(v []byte) error {
		return proto.Unmarshal(v, result)
	})
	return result, err
}

func setProto(tx *badger.Txn, key []byte, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return tx.Set(key, b)
}
```

Add required imports to `store.go`:

```go
import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/dgraph-io/badger/v4"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	pb "github.com/contester/advfiler/protos"
)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -run 'TestUpload|TestDownload' -v .`

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add store.go store_test.go
git commit -m "feat: store Upload and Download with CAS and inline/external logic"
```

---

### Task 6: Store — Delete, List, Wipe, Manifests

**Files:**
- Modify: `store.go`
- Modify: `store_test.go`

- [ ] **Step 1: Write tests**

Append to `store_test.go`:

```go
func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Upload(ctx, FileInfo{Name: "x", ContentLength: 3}, strings.NewReader("abc"))
	if err := s.Delete(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	err := s.Download(ctx, "x", func(dr DownloadResult) error {
		t.Fatal("should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := strings.Repeat("Z", 100)
	s.Upload(ctx, FileInfo{Name: "a", ContentLength: 100}, strings.NewReader(data))
	s.Upload(ctx, FileInfo{Name: "b", ContentLength: 100}, strings.NewReader(data))

	// Delete one — the other should still work
	s.Delete(ctx, "a")
	err := s.Download(ctx, "b", func(dr DownloadResult) error {
		got, _ := io.ReadAll(dr.Body)
		if string(got) != data {
			t.Fatal("data mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Upload(ctx, FileInfo{Name: "dir/a", ContentLength: 1}, strings.NewReader("1"))
	s.Upload(ctx, FileInfo{Name: "dir/b", ContentLength: 1}, strings.NewReader("2"))
	s.Upload(ctx, FileInfo{Name: "other/c", ContentLength: 1}, strings.NewReader("3"))

	names, err := s.List(ctx, "dir/")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %v", len(names), names)
	}
}

func TestWipe(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Upload(ctx, FileInfo{Name: "a", ContentLength: 1}, strings.NewReader("x"))
	s.Upload(ctx, FileInfo{Name: "b", ContentLength: 1}, strings.NewReader("y"))

	if err := s.Wipe(ctx); err != nil {
		t.Fatal(err)
	}

	names, _ := s.List(ctx, "")
	if len(names) != 0 {
		t.Fatalf("expected empty after wipe, got %v", names)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte(`{"id":"prob1","revision":1}`)
	if err := s.SetManifest(ctx, "prob1/1", data); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetManifest(ctx, "prob1/1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestListManifests(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetManifest(ctx, "prob1/1", []byte(`{}`))
	s.SetManifest(ctx, "prob1/2", []byte(`{}`))
	s.SetManifest(ctx, "prob2/1", []byte(`{}`))

	keys, err := s.ListManifests(ctx, "prob1")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d, want 2: %v", len(keys), keys)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestDelete|TestList|TestWipe|TestManifest' -v .`

Expected: FAIL — methods not defined.

- [ ] **Step 3: Implement Delete, List, Wipe**

Add to `store.go`:

```go
func (s *Store) Delete(ctx context.Context, path string) error {
	return s.db.Update(func(tx *badger.Txn) error {
		entry, err := getProto[pb.DirectoryEntry](tx, dirMetaKey(path))
		if err != nil {
			return fmt.Errorf("%w: %s", fs.ErrNotExist, path)
		}

		if err := s.unlinkHash(tx, entry.Blake3Hash, path, !entry.External); err != nil {
			return err
		}

		if !entry.External {
			tx.Delete(dirDataKey(path))
		}

		return tx.Delete(dirMetaKey(path))
	})
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	scanPrefix := make([]byte, 1+len(prefix))
	scanPrefix[0] = prefixDir
	copy(scanPrefix[1:], prefix)

	err := s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = scanPrefix
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Seek(scanPrefix); it.Valid(); it.Next() {
			if path, ok := extractPathFromDirMetaKey(it.Item().Key()); ok {
				result = append(result, path)
			}
		}
		return nil
	})
	return result, err
}

func (s *Store) Wipe(ctx context.Context) error {
	paths, err := s.List(ctx, "")
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := s.Delete(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement manifest methods**

Add to `store.go`:

```go
func (s *Store) GetManifest(ctx context.Context, key string) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *badger.Txn) error {
		item, err := tx.Get(manifestKey(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return fmt.Errorf("%w: manifest %s", fs.ErrNotExist, key)
			}
			return err
		}
		result, err = item.ValueCopy(nil)
		return err
	})
	return result, err
}

func (s *Store) SetManifest(ctx context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Set(manifestKey(key), value)
	})
}

func (s *Store) ListManifests(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	pfx := manifestKey(prefix)

	err := s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = pfx
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Seek(pfx); it.Valid(); it.Next() {
			key := it.Item().Key()
			result = append(result, string(key[1:])) // strip prefix byte
		}
		return nil
	})
	return result, err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -run 'TestDelete|TestList|TestWipe|TestManifest' -v .`

Expected: All PASS.

- [ ] **Step 6: Run full test suite**

Run: `go test -v .`

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add store.go store_test.go
git commit -m "feat: store Delete, List, Wipe, and manifest methods"
```

---

### Task 7: Main — Config and Wiring

**Files:**
- Rewrite: `main.go`

- [ ] **Step 1: Rewrite main.go**

```go
package main

import (
	"net/http"
	"os"

	"github.com/coreos/go-systemd/daemon"
	"github.com/dgraph-io/badger/v4"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/trace"
	"stingr.net/go/systemdutil"

	log "github.com/sirupsen/logrus"

	_ "net/http/pprof"
)

type config struct {
	ListenHTTP      []string `envconfig:"LISTEN_HTTP"`
	BadgerDir       string   `envconfig:"BADGER_DIR"`
	BadgerValueDir  string   `envconfig:"BADGER_VALUE_DIR"`
	ValidAuthTokens []string `envconfig:"VALID_AUTH_TOKENS"`
}

func main() {
	systemdutil.Init()

	var cfg config
	if err := envconfig.Process("advfiler", &cfg); err != nil {
		log.Fatal(err)
	}

	if cfg.BadgerDir == "" {
		log.Fatal("ADVFILER_BADGER_DIR must be specified")
	}

	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) { return true, true }
	http.Handle("/metrics", promhttp.Handler())

	_, httpSockets, _ := systemdutil.ListenSystemd(systemdutil.ActivationFiles())

	authCheck := AuthChecker{
		validTokens: make(map[string]struct{}, len(cfg.ValidAuthTokens)),
	}
	for _, v := range cfg.ValidAuthTokens {
		authCheck.validTokens[v] = struct{}{}
	}

	httpSockets = append(httpSockets, systemdutil.MustListenTCPSlice(cfg.ListenHTTP)...)

	opts := badger.DefaultOptions(cfg.BadgerDir).WithLogger(log.StandardLogger())
	if cfg.BadgerValueDir != "" {
		opts.ValueDir = cfg.BadgerValueDir
	}
	if err := os.MkdirAll(opts.Dir, os.ModePerm); err != nil {
		log.Fatalf("can't create badger dir: %v", err)
	}
	if err := os.MkdirAll(opts.ValueDir, os.ModePerm); err != nil {
		log.Fatalf("can't create badger value dir: %v", err)
	}

	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("can't open badger: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	defer store.Close()

	f := NewFiler(store, &authCheck)
	ms := NewMetadataServer(store)
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.HandleFunc("/tar/", f.handleTarUpload)
	http.HandleFunc("/wipe/", f.handleWipe)
	http.HandleFunc("/protopackage/", f.handleProtoPackage)
	http.HandleFunc("/protopackage", f.handleProtoPackage)
	systemdutil.ServeAll(nil, httpSockets, nil)
	daemon.SdNotify(false, daemon.SdNotifyReady)
	defer daemon.SdNotify(false, daemon.SdNotifyStopping)
	systemdutil.WaitSigint()
}
```

- [ ] **Step 2: Verify it compiles (won't fully build yet — filer.go and metadata.go still reference old types)**

Run: `go vet ./main.go ./store.go ./hashes.go ./authchecker.go`

Expected: May have errors from filer.go/metadata.go — that's OK, they're rewritten next.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: rewrite main.go for Badger + Store"
```

---

### Task 8: Filer — HTTP Handlers

**Files:**
- Rewrite: `filer.go`

Adapt all HTTP handlers to use `Store` directly with callback-based `Download`. Incorporates all bug fixes from the efstore branch. Moves `addDigests` and `parseDigests` here (HTTP-layer helpers). Keeps `limitReadSeeker` for `X-Fs-Limit` support.

- [ ] **Step 1: Rewrite filer.go**

```go
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/contester/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

type AuthCheck interface {
	Check(ctx context.Context, token string, action pb.AuthAction, path string) (bool, error)
}

type filerServer struct {
	store       *Store
	urlPrefix   string
	authChecker AuthCheck
}

func NewFiler(store *Store, authCheck AuthCheck) *filerServer {
	return &filerServer{
		store:       store,
		urlPrefix:   "/fs/",
		authChecker: authCheck,
	}
}

func tokenFromHeader(req *http.Request) string {
	if ah := req.Header.Get("Authorization"); len(ah) > 7 && strings.EqualFold(ah[0:7], "BEARER ") {
		return ah[7:]
	}
	return ""
}

var errUnauthorized = errors.New("unauthorized")

func addDigestsToHeader(h http.Header, digests map[string]string) {
	if len(digests) == 0 {
		return
	}
	dkeys := make([]string, 0, len(digests))
	for k := range digests {
		dkeys = append(dkeys, k)
	}
	sort.Strings(dkeys)
	dvals := make([]string, 0, len(dkeys))
	for _, k := range dkeys {
		dvals = append(dvals, k+"="+digests[k])
	}
	h.Add("Digest", strings.Join(dvals, ","))
	if md5, ok := digests["MD5"]; ok && md5 != "" {
		h.Add("Content-MD5", md5)
	}
}

func parseDigests(dh string) map[string]string {
	splits := strings.Split(dh, ",")
	result := make(map[string]string, len(splits))
	for _, v := range splits {
		ds := strings.SplitN(strings.TrimSpace(v), "=", 2)
		if len(ds) != 2 {
			continue
		}
		result[strings.ToUpper(ds[0])] = ds[1]
	}
	return result
}

func (f *filerServer) handleList(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		return errUnauthorized
	}
	names, err := f.store.List(ctx, path)
	if err != nil {
		return err
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, v := range names {
		buf.WriteString(v)
		buf.WriteByte('\n')
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(buf.Bytes()))
	return nil
}

func (f *filerServer) handleDownload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" || path[len(path)-1] == '/' {
		return f.handleList(ctx, w, r, path)
	}

	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		return errUnauthorized
	}

	limitValue := int64(-1)
	if limitStr := r.Header.Get("X-Fs-Limit"); limitStr != "" {
		lv, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil {
			return err
		}
		limitValue = lv
	}

	return f.store.Download(ctx, path, func(result DownloadResult) error {
		rsize := result.Size
		w.Header().Add("X-Fs-Content-Length", strconv.FormatInt(rsize, 10))
		if result.ModuleType != "" {
			w.Header().Add("X-Fs-Module-Type", result.ModuleType)
		}
		if result.LastModifiedTimestamp != 0 {
			t := time.Unix(result.LastModifiedTimestamp, 0)
			w.Header().Set("Last-Modified", t.UTC().Format(http.TimeFormat))
		}

		digestMap := DigestsToMap(result.Digests)

		if r.Method == http.MethodHead {
			addDigestsToHeader(w.Header(), digestMap)
			return nil
		}
		if limitValue != -1 && limitValue < rsize {
			w.Header().Add("X-Fs-Truncated", "true")
		} else {
			addDigestsToHeader(w.Header(), digestMap)
		}
		if limitValue == 0 {
			return nil
		}

		var xr io.ReadSeeker = result.Body
		if limitValue != -1 && limitValue < rsize {
			xr = &limitReadSeeker{r: xr, bytesTotal: limitValue, bytesRemaining: limitValue}
		}
		http.ServeContent(w, r, "", time.Time{}, xr)
		return nil
	})
}

type limitReadSeeker struct {
	r                          io.ReadSeeker
	bytesTotal, bytesRemaining int64
}

func (s *limitReadSeeker) Read(p []byte) (n int, err error) {
	if s.bytesRemaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > s.bytesRemaining {
		p = p[0:s.bytesRemaining]
	}
	n, err = s.r.Read(p)
	s.bytesRemaining -= int64(n)
	return
}

func (s *limitReadSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	case io.SeekStart:
		if offset > s.bytesTotal {
			return 0, errors.New("invalid offset")
		}
		n, err := s.r.Seek(offset, whence)
		if err != nil {
			return 0, err
		}
		s.bytesRemaining = s.bytesTotal - n
		return n, err
	case io.SeekEnd:
		offset += s.bytesTotal
		if offset > s.bytesTotal {
			return 0, errors.New("invalid offset")
		}
		n, err := s.r.Seek(offset, io.SeekStart)
		if err != nil {
			return 0, err
		}
		s.bytesRemaining = s.bytesTotal - n
		return n, err
	case io.SeekCurrent:
		offset = s.bytesRemaining - offset
		if offset < 0 {
			return 0, errors.New("invalid offset")
		}
		n, err := s.r.Seek(offset, whence)
		if err != nil {
			return 0, err
		}
		s.bytesRemaining = s.bytesTotal - n
		return n, err
	}
}

func trimOr(s, prefix, what string) (string, error) {
	if r := strings.TrimPrefix(s, prefix); r != s {
		return r, nil
	}
	return "", fmt.Errorf("%s must start with %s, got %s", what, s, prefix)
}

func (f *filerServer) urlToPath(urlpath string) (string, error) {
	return trimOr(urlpath, f.urlPrefix, "filer url")
}

func (f *filerServer) handleDelete(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	return f.store.Delete(ctx, path)
}

func (f *filerServer) handleUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return f.handleMultiDownload(ctx, w, r)
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_WRITE, path); !v {
		return errUnauthorized
	}

	fi := FileInfo{
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
		Name:       path,
	}
	if ch := r.Header.Get("Content-Length"); ch != "" {
		var err error
		fi.ContentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
	}
	if ch := r.Header.Get("X-Fs-Content-Length"); ch != "" {
		var err error
		fi.ContentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
	}

	fi.RecvDigests = parseDigests(r.Header.Get("Digest"))
	if ch := r.Header.Get("Content-MD5"); ch != "" {
		fi.RecvDigests["MD5"] = ch
	}

	result, err := f.store.Upload(ctx, fi, r.Body)
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(&result)
}

type singleDownloadEntry struct {
	Source      string
	Destination string
}

type multiDownloadRequest struct {
	Entry []singleDownloadEntry
}

func (f *filerServer) handleMultiDownload(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		return errUnauthorized
	}
	decoder := json.NewDecoder(r.Body)
	var mdreq multiDownloadRequest
	if err := decoder.Decode(&mdreq); err != nil {
		return err
	}
	cout := zip.NewWriter(w)
	defer cout.Close()
	for _, entry := range mdreq.Entry {
		f.writeRemoteFileAs(ctx, cout, nil, entry.Source, entry.Destination)
	}
	return nil
}

func (f *filerServer) writeRemoteFileAs(ctx context.Context, zw *zip.Writer, tw *tar.Writer, name, as string) error {
	return f.store.Download(ctx, name, func(result DownloadResult) error {
		if zw != nil {
			fh := zip.FileHeader{
				Name:               as,
				UncompressedSize64: uint64(result.Size),
				Method:             zip.Deflate,
			}
			if result.ModuleType != "" {
				fh.Name += "." + result.ModuleType
			}
			wr, err := zw.CreateHeader(&fh)
			if err != nil {
				return err
			}
			_, err = io.Copy(wr, result.Body)
			return err
		}
		if tw != nil {
			fh := tar.Header{
				Name:     as,
				Mode:     0666,
				Size:     result.Size,
				Typeflag: tar.TypeReg,
			}
			if result.LastModifiedTimestamp != 0 {
				fh.ModTime = time.Unix(result.LastModifiedTimestamp, 0)
			}
			if result.ModuleType != "" {
				fh.Xattrs = map[string]string{"user.fs_module_type": result.ModuleType}
			}
			if err := tw.WriteHeader(&fh); err != nil {
				return err
			}
			_, err := io.Copy(tw, result.Body)
			return err
		}
		return nil
	})
}

func (f *filerServer) writeProblemData(ctx context.Context, w *zip.Writer, problemID string) error {
	prefix := "problem/" + problemID + "/"
	names, _ := f.store.List(ctx, prefix)
	for _, name := range names {
		pname := strings.TrimPrefix(name, prefix)
		if pname == "checker" {
			f.writeRemoteFileAs(ctx, w, nil, name, "checker")
			continue
		}
		splits := strings.Split(pname, "/")
		if len(splits) != 3 || splits[0] != "tests" {
			continue
		}
		var dname string
		switch splits[2] {
		case "input.txt":
			dname = splits[1]
		case "answer.txt":
			dname = splits[1] + ".a"
		}
		if dname == "" {
			continue
		}
		f.writeRemoteFileAs(ctx, w, nil, name, dname)
	}
	return nil
}

func (f *filerServer) HandlePackage(w http.ResponseWriter, r *http.Request) {
	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cout := zip.NewWriter(w)
	defer cout.Close()

	contestID := r.FormValue("contest")
	submitID := r.FormValue("submit")
	testingID := r.FormValue("testing")

	if contestID != "" && submitID != "" && testingID != "" {
		names, _ := f.store.List(r.Context(), "submit/"+contestID+"/"+submitID+"/"+testingID+"/")
		for _, name := range names {
			splits := strings.Split(name, "/")
			if len(splits) < 5 || splits[len(splits)-1] != "output" {
				continue
			}
			f.writeRemoteFileAs(r.Context(), cout, nil, name, splits[len(splits)-2]+".o")
		}
		f.writeRemoteFileAs(r.Context(), cout, nil, "submit/"+contestID+"/"+submitID+"/compiledModule", "solution")
		f.writeRemoteFileAs(r.Context(), cout, nil, "submit/"+contestID+"/"+submitID+"/sourceModule", "solution")
	}

	if problemID := r.FormValue("problem"); problemID != "" {
		f.writeProblemData(r.Context(), cout, problemID)
	}
}

func (f *filerServer) downloadAsset(ctx context.Context, name, as string, limit int64) (*pb.Asset, error) {
	var asset *pb.Asset
	err := f.store.Download(ctx, name, func(result DownloadResult) error {
		xr := pb.Asset{
			Name:         as,
			OriginalSize: result.Size,
			Truncated:    result.Size > limit,
		}
		bb := make([]byte, limit)
		n, err := result.Body.Read(bb)
		if err != nil && err != io.EOF {
			return err
		}
		xr.Data = append([]byte(nil), bb[:n]...)
		asset = &xr
		return nil
	})
	return asset, err
}

func (f *filerServer) handleProtoPackage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	contestID := r.FormValue("contest")
	submitID := r.FormValue("submit")
	testingID := r.FormValue("testing")
	problemID := r.FormValue("problem")
	sizeLimit := int64(1024)
	solutionSizeLimit := int64(128000)

	if !strings.HasPrefix(problemID, "problem/") {
		http.Error(w, "invalid problem ID: "+problemID, http.StatusNotFound)
		return
	}

	if sz := r.FormValue("sizeLimit"); sz != "" {
		if isz, err := strconv.ParseInt(sz, 10, 64); err == nil {
			sizeLimit = isz
		}
	}

	var result pb.TestingRecord
	var err error
	result.Solution, err = f.downloadAsset(ctx, "submit/"+contestID+"/"+submitID+"/sourceModule", "source", solutionSizeLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	prefix := problemID + "/"
	names, err := f.store.List(ctx, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	testSet := make(map[int64]struct{})
	for _, name := range names {
		pname := strings.TrimPrefix(name, prefix)
		if pname == "checker" {
			continue
		}
		splits := strings.Split(pname, "/")
		if len(splits) != 3 || splits[0] != "tests" {
			continue
		}
		testID, err := strconv.ParseInt(splits[1], 10, 64)
		if err != nil {
			continue
		}
		testSet[testID] = struct{}{}
	}
	testList := make([]int64, 0, len(testSet))
	for i := range testSet {
		testList = append(testList, i)
	}
	sort.Slice(testList, func(i, j int) bool { return testList[i] < testList[j] })

	for _, testID := range testList {
		outName := "submit/" + contestID + "/" + submitID + "/" + testingID + "/" + strconv.FormatInt(testID, 10) + "/output"
		out, err := f.downloadAsset(ctx, outName, "output", sizeLimit)
		if err != nil {
			continue
		}
		testRecord := pb.TestRecord{
			TestId: testID,
			Output: out,
		}
		testPrefix := prefix + "tests/" + strconv.FormatInt(testID, 10) + "/"
		if testRecord.Input, err = f.downloadAsset(ctx, testPrefix+"input.txt", "input", sizeLimit); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		testRecord.Answer, _ = f.downloadAsset(ctx, testPrefix+"answer.txt", "answer", sizeLimit)
		result.Test = append(result.Test, &testRecord)
	}

	b, err := proto.Marshal(&result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := f.urlToPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodPut, http.MethodPost:
		err = f.handleUpload(ctx, w, r, path)
	case http.MethodGet, http.MethodHead:
		err = f.handleDownload(ctx, w, r, path)
	case http.MethodDelete:
		err = f.handleDelete(ctx, w, r, path)
	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		log.Errorf("%q: %v", path, err)
	}
}

func (f *filerServer) handleTarDownload(w http.ResponseWriter, r *http.Request) {
	path := r.FormValue("path")
	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	names, err := f.store.List(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sort.Strings(names)

	fw := tar.NewWriter(w)
	defer fw.Close()
	for _, v := range names {
		f.writeRemoteFileAs(r.Context(), nil, fw, v, v)
	}
}

func (f *filerServer) handleTarUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.handleTarDownload(w, r)
		return
	}
	if r.Method != http.MethodPut {
		return
	}

	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_WRITE, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var realSize, savedSize int64
	var icnt int

	fr := tar.NewReader(r.Body)
	for {
		h, err := fr.Next()
		if err == io.EOF {
			break
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Name == "" || strings.HasSuffix(h.Name, "/") {
			continue
		}
		fi := FileInfo{
			ModuleType:    h.Xattrs["user.fs_module_type"],
			Name:          h.Name,
			ContentLength: h.Size,
		}
		if !h.ModTime.IsZero() {
			fi.TimestampUnix = h.ModTime.Unix()
		}
		res, err := f.store.Upload(r.Context(), fi, fr)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		icnt++
		if res.Hardlinked {
			savedSize += res.Size
		} else {
			realSize += res.Size
		}
	}
	fmt.Fprintf(w, "Files: %d, real size: %d, saved size: %d\n", icnt, realSize, savedSize)
}

func (f *filerServer) handleWipe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		return
	}

	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_WRITE, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	files, err := f.store.List(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, v := range files {
		if v == "" {
			continue
		}
		if err = f.store.Delete(r.Context(), v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	for _, v := range files {
		fmt.Fprintln(w, v)
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add filer.go
git commit -m "feat: rewrite filer.go for Store with callback-based download"
```

---

### Task 9: Metadata — HTTP Handlers

**Files:**
- Rewrite: `metadata.go`

- [ ] **Step 1: Rewrite metadata.go**

```go
package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
)

type metadataServer struct {
	store *Store
}

func NewMetadataServer(store *Store) *metadataServer {
	return &metadataServer{store: store}
}

type problemManifest struct {
	Id       string `json:"id"`
	Revision int    `json:"revision"`

	TestCount       int    `json:"testCount"`
	TimeLimitMicros int64  `json:"timeLimitMicros"`
	MemoryLimit     int64  `json:"memoryLimit"`
	Stdio           bool   `json:"stdio,omitempty"`
	TesterName      string `json:"testerName"`
	Answers         []int  `json:"answers,omitempty"`
	InteractorName  string `json:"interactorName,omitempty"`
	CombinedHash    string `json:"combinedHash,omitempty"`
}

type problemKey struct {
	Id       string `json:"id"`
	Revision int    `json:"revision"`
}

func (f *metadataServer) handleSetManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	var mf problemManifest
	if err := json.NewDecoder(r.Body).Decode(&mf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mb, err := json.Marshal(&mf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err = f.store.SetManifest(r.Context(), revKey(mf.Id, mf.Revision), mb); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func revKey(id string, rev int) string {
	return id + "/" + strconv.Itoa(rev)
}

func (f *metadataServer) getManifest(key string) (problemManifest, error) {
	var result problemManifest
	data, err := f.store.GetManifest(nil, key)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func (f *metadataServer) getK(pk problemKey) ([]problemManifest, error) {
	keys, err := f.buildKeys(pk)
	if err != nil {
		return nil, err
	}
	result := make([]problemManifest, 0, len(keys))
	for _, v := range keys {
		if m, err := f.getManifest(v); err == nil {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a := result[i].Id
		b := result[j].Id
		if a < b {
			return true
		}
		if a > b {
			return false
		}
		return result[i].Revision > result[j].Revision
	})
	return result, nil
}

func (f *metadataServer) buildKeys(pk problemKey) ([]string, error) {
	if pk.Revision != 0 {
		return []string{revKey(pk.Id, pk.Revision)}, nil
	}
	return f.store.ListManifests(nil, pk.Id)
}

func getRequestProblemKey(r *http.Request) (problemKey, error) {
	result := problemKey{
		Id: r.FormValue("id"),
	}
	rev := r.FormValue("revision")
	if rev == "" {
		return result, nil
	}
	var err error
	result.Revision, err = strconv.Atoi(rev)
	return result, err
}

func (f *metadataServer) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pk, err := getRequestProblemKey(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	revs, err := f.getK(pk)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(revs) == 0 && pk.Id != "" {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(revs)
}
```

Note: `getManifest` and `buildKeys` pass `nil` for context since our Store methods accept `context.Context` — we should use `context.Background()` there. Actually, looking at the callers, these are called from HTTP handlers that have `r.Context()`. Let me fix: pass `r.Context()` through the chain. The `getK` and `buildKeys` and `getManifest` methods should take context. Let me update:

The methods `getManifest`, `getK`, `buildKeys` should accept `ctx context.Context` as first parameter. Use `r.Context()` in the handler call sites. Replace the `nil` context calls above with proper `ctx` threading:

- `getManifest(ctx, key)` calls `f.store.GetManifest(ctx, key)`
- `buildKeys(ctx, pk)` calls `f.store.ListManifests(ctx, pk.Id)`
- `getK(ctx, pk)` calls `f.buildKeys(ctx, pk)` and `f.getManifest(ctx, v)`
- `handleGetManifest` calls `f.getK(r.Context(), pk)`

- [ ] **Step 2: Commit**

```bash
git add metadata.go
git commit -m "feat: rewrite metadata.go to use Store manifest methods"
```

---

### Task 10: Delete Old Packages and Clean Up

**Files:**
- Delete: `common/kv.go`, `common/hashes.go`
- Delete: `efbackend/filer.go`
- Delete: `ldbackend/ldpabckend.go`
- Delete: `main_linux.go`, `main_darwin.go`, `main_windows.go` (empty stubs)
- Modify: `go.mod`

- [ ] **Step 1: Remove old packages**

```bash
rm -rf common/ efbackend/ ldbackend/
rm -f main_linux.go main_darwin.go main_windows.go
```

- [ ] **Step 2: Clean up go.mod**

```bash
go mod tidy
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`

Expected: Clean build.

- [ ] **Step 4: Run full test suite**

Run: `go test -v .`

Expected: All tests pass.

- [ ] **Step 5: Run go vet**

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: remove old efstore/leveldb packages, clean up deps"
```

---

### Task 11: Integration Smoke Test

**Files:**
- Modify: `store_test.go`

Add an integration test that exercises the full lifecycle: upload, list, download, overwrite, dedup hardlink detection, delete, wipe, manifests.

- [ ] **Step 1: Write integration test**

Append to `store_test.go`:

```go
func TestIntegrationLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Upload 3 files, 2 with identical content
	s.Upload(ctx, FileInfo{Name: "problem/p1/tests/1/input.txt", ContentLength: 4, ModuleType: ""}, strings.NewReader("1234"))
	s.Upload(ctx, FileInfo{Name: "problem/p1/tests/1/answer.txt", ContentLength: 2}, strings.NewReader("42"))
	s.Upload(ctx, FileInfo{Name: "submit/1/2/3/1/output", ContentLength: 2}, strings.NewReader("42"))

	// List
	names, _ := s.List(ctx, "problem/p1/")
	if len(names) != 2 {
		t.Fatalf("list got %d, want 2", len(names))
	}

	// Download
	s.Download(ctx, "problem/p1/tests/1/input.txt", func(dr DownloadResult) error {
		data, _ := io.ReadAll(dr.Body)
		if string(data) != "1234" {
			t.Fatalf("got %q", data)
		}
		return nil
	})

	// Overwrite
	s.Upload(ctx, FileInfo{Name: "problem/p1/tests/1/input.txt", ContentLength: 5}, strings.NewReader("56789"))
	s.Download(ctx, "problem/p1/tests/1/input.txt", func(dr DownloadResult) error {
		data, _ := io.ReadAll(dr.Body)
		if string(data) != "56789" {
			t.Fatalf("overwrite: got %q", data)
		}
		return nil
	})

	// Delete one
	s.Delete(ctx, "submit/1/2/3/1/output")
	if err := s.Download(ctx, "submit/1/2/3/1/output", func(dr DownloadResult) error { return nil }); err == nil {
		t.Fatal("expected error after delete")
	}

	// Answer still works (was deduped with the deleted one)
	s.Download(ctx, "problem/p1/tests/1/answer.txt", func(dr DownloadResult) error {
		data, _ := io.ReadAll(dr.Body)
		if string(data) != "42" {
			t.Fatalf("dedup survivor: got %q", data)
		}
		return nil
	})

	// Manifests
	s.SetManifest(ctx, "p1/1", []byte(`{"id":"p1","revision":1,"testCount":1}`))
	got, _ := s.GetManifest(ctx, "p1/1")
	if !bytes.Contains(got, []byte("p1")) {
		t.Fatal("manifest missing")
	}

	// Wipe
	s.Wipe(ctx)
	names, _ = s.List(ctx, "")
	if len(names) != 0 {
		t.Fatalf("wipe: got %d entries", len(names))
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test -run TestIntegrationLifecycle -v .`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add store_test.go
git commit -m "test: integration lifecycle test for store"
```

---

## Self-Review

**Spec coverage:**
- ✅ Single Badger DB with compound keys
- ✅ DirectoryEntry, HashEntry, PathList protos
- ✅ Inline vs external decision with break-even
- ✅ Upload with CAS (first upload, pre-externalization, post-externalization)
- ✅ Upload overwrite handling
- ✅ Download zero-copy via item.Value callback
- ✅ Delete with refcount/path list management
- ✅ List with metadata key filtering
- ✅ Wipe
- ✅ Manifest CRUD
- ✅ No Backend interface — direct Store calls
- ✅ Config with BADGER_DIR/BADGER_VALUE_DIR
- ✅ Bug fixes ported (leaks eliminated by callback pattern, ServeContent, limitReadSeeker)
- ✅ Value log GC goroutine
- ✅ Blake3 hashing
- ✅ Package layout matches spec
- ✅ Old packages removed

**Placeholder scan:** No TBDs or TODOs found.

**Type consistency:**
- `FileInfo` — consistent across store.go, filer.go
- `UploadStatus` — consistent across store.go, filer.go
- `DownloadResult` — struct with public fields, consistent usage in callback pattern
- `Store` methods — `Upload`, `Download`, `Delete`, `List`, `Wipe`, `GetManifest`, `SetManifest`, `ListManifests` — all consistent between definition and call sites
- `NewFiler(store, authCheck)` — matches main.go call
- `NewMetadataServer(store)` — matches main.go call
- Key builder functions — `dirMetaKey`, `dirDataKey`, `blobDataKey`, `blobDigestsKey`, `blobHashEntryKey`, `manifestKey`, `extractPathFromDirMetaKey` — consistent between store.go and tests

**Spec gap found:** Upload pre-check with provided hash (X-Blake3-Hash header) — the `FileInfo.Blake3Hash` field exists and `Upload` checks it, but `filer.go`'s `handleUpload` doesn't read the header. Adding to filer.go's handleUpload: parse `X-Blake3-Hash` header into `fi.Blake3Hash` (base64 or hex decode). This should be added in Task 8's handleUpload. Adding this line after the digest parsing:

```go
if bh := r.Header.Get("X-Blake3-Hash"); bh != "" {
    fi.Blake3Hash, _ = base64.StdEncoding.DecodeString(bh)
}
```

This is included in the Task 8 filer.go rewrite above (add it to handleUpload after the RecvDigests parsing). Also need to add `"encoding/base64"` to filer.go imports.
