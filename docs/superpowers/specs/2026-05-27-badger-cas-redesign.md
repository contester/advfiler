# Advfiler: Badger CAS Redesign

Replace efstore + LevelDB with a single Badger v4 database using content-addressable storage with dynamic inline/external placement.

## Context

Advfiler is an HTTP file storage service for programming contests. Contest workloads have a key property: many files share identical content (correct solutions produce the same output). The previous badger-based backend had CAS with inode-based dedup but used chunking and complex inode indirection. The efstore migration lost dedup entirely and added a fragile LevelDB index layer.

This redesign keeps Badger's crash resilience and simplicity while adding a cleaner CAS scheme with dynamic inline/external storage decisions.

## Storage Model

### Single Badger DB

One `*badger.DB` instance for everything. Key prefix namespaces:

| Prefix | Key | Value |
|--------|-----|-------|
| `0x01` | path | `DirectoryEntry` proto |
| `0x02` | blake3-256 hash (32 bytes) | raw file bytes |
| `0x03` | blake3-256 hash (32 bytes) | `HashEntry` proto |
| `0x04` | manifest key (string) | JSON blob (problem manifests) |

### DirectoryEntry proto

```proto
message DirectoryEntry {
    bytes blake3_hash = 1;
    string module_type = 2;
    int64 last_modified_timestamp = 3;
    Digests digests = 4;       // SHA1, MD5, SHA256 for HTTP Digest headers
    bytes inline_data = 5;     // non-empty when content is stored inline
}
```

When `inline_data` is set, the content lives in this entry. When empty, the content is in the blob at `0x02` + `blake3_hash`.

The `blake3_hash` is always stored (regardless of inline/external) for integrity verification and CAS lookup.

### HashEntry proto

```proto
message HashEntry {
    int64 data_size = 1;
    repeated string paths = 2;   // tracked while content is inline
    int64 refcount = 3;          // used after externalization (paths is empty)
}
```

Two states:
- **Pre-externalization**: `paths` lists all directory entry paths with this content stored inline. `refcount` is 0 (implicit from `len(paths)`).
- **Post-externalization**: `paths` is empty. `refcount` tracks the number of directory entries referencing the external blob.

### Inline vs External Decision

Directory entries always store the blake3 hash. The per-entry cost difference between inline and external is the `data_size` bytes of inline content. Externalizing N copies saves `(N-1) * data_size` bytes but costs a fixed blob overhead `B` (blob Badger entry: key + metadata, approximately 50 bytes).

**Externalize when**: `(N-1) * data_size > B`

Examples (B=50):
- 100-byte value: externalize at N=2 (savings: 100 > 50)
- 10-byte value: externalize at N=7 (savings: 60 > 50)
- 2-byte value: externalize at N=26 (savings: 50 > 50)
- 1-byte value: externalize at N=51

The path list in `HashEntry` is bounded by `B / data_size + 1` entries. Worst case is ~50 entries for 1-byte values.

## Operations

### Upload

1. **Buffer body in memory**, computing blake3-256 (plus SHA1, MD5, SHA256 for digest headers) as data streams in.
2. **If hash was provided in HTTP header**: compare against computed hash. Reject with error if mismatch (transit corruption).
3. **Look up `0x03` + hash**:
   - **Not found** (first upload of this content): create `HashEntry{data_size, paths: [this_path]}`, write `DirectoryEntry` with inline data. Single `db.Update` transaction.
   - **Found, pre-externalization** (paths list): add this path to the list. Check break-even. If threshold met: write blob to `0x02`, rewrite ALL listed directory entries from inline→reference, convert `HashEntry` to refcount mode. If threshold not met: write `DirectoryEntry` with inline data, update paths list. Single `db.Update` transaction.
   - **Found, post-externalization** (refcount): increment refcount, write `DirectoryEntry` with hash reference (no inline data). Content blob already exists. Single `db.Update` transaction.
4. **If overwriting an existing path**: handle the old content's hash — decrement its refcount or remove from its paths list (see Delete logic, applied inline within the upload transaction).

### Download

Single `db.View` transaction, zero-copy serving:

```go
db.View(func(tx *badger.Txn) error {
    // 1. Get DirectoryEntry at 0x01 + path
    // 2. Extract metadata (size, module_type, digests, timestamp)
    // 3. If inline_data is set:
    //      http.ServeContent(w, r, "", modTime, bytes.NewReader(entry.InlineData))
    // 4. If external:
    //      contentItem, _ := tx.Get(0x02 + entry.Blake3Hash)
    //      contentItem.Value(func(v []byte) error {
    //          http.ServeContent(w, r, "", modTime, bytes.NewReader(v))
    //          return nil
    //      })
})
```

The read transaction stays open during HTTP serving. Acceptable for contest workloads (fast local clients, files < 128MB).

### Delete

Single `db.Update` transaction:

1. Get `DirectoryEntry` at `0x01` + path. Extract hash.
2. Look up `0x03` + hash:
   - **Pre-externalization**: remove this path from list. If list empty, delete `0x03` entry.
   - **Post-externalization**: decrement refcount. If zero, delete `0x02` blob and `0x03` entry.
3. Delete `0x01` + path.

### List

Badger prefix iterator over `0x01` + prefix with `PrefetchValues: false` (keys only). Strip the prefix byte from returned keys.

### Wipe

Iterate all `0x01` entries, delete each (with proper refcount/hash cleanup). Or, since this is a full wipe, just drop all prefixes.

## Architecture

### No Backend Interface

No `common.Backend` interface. A single concrete `Store` struct holds the `*badger.DB` and exposes methods called directly by HTTP handlers:

```go
type Store struct {
    db *badger.DB
}

func (s *Store) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error)
func (s *Store) Download(ctx context.Context, path string, fn func(DownloadResult) error) error
func (s *Store) List(ctx context.Context, prefix string) ([]string, error)
func (s *Store) Delete(ctx context.Context, path string) error
func (s *Store) Close()

// Manifest methods (problem metadata)
func (s *Store) GetManifest(ctx context.Context, key string) ([]byte, error)
func (s *Store) SetManifest(ctx context.Context, key string, value []byte) error
func (s *Store) ListManifests(ctx context.Context, prefix string) ([]string, error)
```

`Download` is callback-based: the `DownloadResult` is valid only inside the callback (tied to the Badger read transaction lifetime).

```go
type DownloadResult struct {
    Size                 int64
    ModuleType           string
    Digests              Digests
    LastModifiedTimestamp int64
    Body                 io.ReadSeeker
}
```

### Upload Pre-check with Provided Hash

When the client sends a blake3 hash in the HTTP request header (e.g., `X-Blake3-Hash`), the upload can short-circuit:

1. Look up the hash before reading the body.
2. If content exists and overwrite semantics allow hardlink: still read and verify the body (reject on mismatch), but skip writing the blob.
3. If content is new: proceed normally.

This enables clients that already know the hash to signal intent early, while always verifying integrity.

### Package Layout

```
advfiler/
  main.go              — config, HTTP wiring, Badger open/close
  store.go             — Store struct, Upload/Download/Delete/List/manifest methods
  filer.go             — HTTP handlers for /fs/, /tar/, /fs2/, /protopackage/, /wipe/
  metadata.go          — HTTP handlers for /problem/get/, /problem/set/
  authchecker.go       — bearer token auth (unchanged)
  hashes.go            — Digests, Hashes, blake3 helpers (extracted from common/)
  protos/              — proto definitions and generated code
  backup/              — CLI backup tool (unchanged)
```

The `common/`, `efbackend/`, `ldbackend/` packages are eliminated. Hash utilities move to the main package or a small `hashes.go` file.

### Filer.go Changes

HTTP handlers call `Store` methods directly. Key changes from current code:

- `handleDownload`: uses callback-based `store.Download` — HTTP serving happens inside the callback.
- `handleUpload`: passes request body to `store.Upload`, which handles buffering and CAS internally.
- `handleTarUpload`/`handleTarDownload`: same pattern, iterating entries.
- `downloadAsset`, `handleProtoPackage`, `HandlePackage`, `writeRemoteFileAs`: adapted to callback-based download.
- `handleWipe`: calls `store.Wipe()` or iterates `store.List` + `store.Delete`.

### MetadataServer Changes

`metadataServer` holds a `*Store` instead of a `common.DB`. Calls `store.GetManifest`/`store.SetManifest`/`store.ListManifests` which use the `0x04` prefix namespace directly.

## Proto Changes

Updated `protos.proto`:

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
    bytes inline_data = 5;
}

message HashEntry {
    int64 data_size = 1;
    repeated string paths = 2;
    int64 refcount = 3;
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

Removed: `FileMetadata`, `FileInfo`, `FileChunk`, `Inode`, `ChunkList`, `ThisChecksum`, `InodeVolatileAttributes`, `CompressionType`.

## Dependencies

### Add
- `github.com/dgraph-io/badger/v4`
- `lukechampine.com/blake3` (or `github.com/zeebo/blake3`)

### Remove
- `github.com/syndtr/goleveldb`
- `stingr.net/go/efstore` (and the `replace` directive)

### Keep
- `google.golang.org/protobuf`
- `github.com/prometheus/client_golang`
- `github.com/sirupsen/logrus`
- `github.com/coreos/go-systemd`
- `github.com/kelseyhightower/envconfig`
- `golang.org/x/net`
- `stingr.net/go/systemdutil`

## Configuration

Environment variables (via envconfig):

```
ADVFILER_LISTEN_HTTP        — HTTP listen addresses
ADVFILER_BADGER_DIR         — Badger DB directory
ADVFILER_BADGER_VALUE_DIR   — Badger value log directory (optional, defaults to BADGER_DIR)
ADVFILER_VALID_AUTH_TOKENS  — bearer token whitelist
```

Replaces the old `MANIFEST_DB`, `FILER_DB`, `FILER_STORE` (three directories) with one or two.

## Migration

Fresh deployment. No migration from existing LevelDB/efstore data needed.

## Bug Fixes Ported from Efstore Branch

The following fixes from the efstore branch apply to `filer.go` and `metadata.go` regardless of backend:

1. **downloadAsset leak** (4f851f7): add `defer fr.Body().Close()`, error check after Read
2. **handleDownload/writeRemoteFileAs leaks** (1ea16e5): add `defer result.Body().Close()`, use `errors.Is()`
3. **ServeContent for proto packages** (c888a31): use `http.ServeContent` instead of raw `w.Write`
4. **limitReadSeeker** (5222504): replace `io.LimitReader`/`io.Copy` with `http.ServeContent` + seekable reader
5. **Double error write** (384bf54): remove `http.Error` after `log.Errorf` when response already written
6. **sort.Slice** (f09c4cf): replace sort.Interface boilerplate

These will be applied during implementation since the handler code is being rewritten to use callback-based download.

## GC

Badger's value log GC runs periodically (the old code ran it every 60 minutes at 50% threshold). The new code should do the same via a background goroutine in `Store`.

No application-level GC is needed — refcount management is transactional and inline with upload/delete operations.
