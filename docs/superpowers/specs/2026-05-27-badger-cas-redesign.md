# Advfiler: Badger CAS Redesign

Replace efstore + LevelDB with a single Badger v4 database using content-addressable storage with dynamic inline/external placement.

## Context

Advfiler is an HTTP file storage service for programming contests. Contest workloads have a key property: many files share identical content (correct solutions produce the same output). The previous badger-based backend had CAS with inode-based dedup but used chunking and complex inode indirection. The efstore migration lost dedup entirely and added a fragile LevelDB index layer.

This redesign keeps Badger's crash resilience and simplicity while adding a cleaner CAS scheme with dynamic inline/external storage decisions.

## Storage Model

### Single Badger DB

One `*badger.DB` instance for everything. Compound keys using `\x00` as delimiter (paths never contain null bytes).

### Key Layout

**Directory entries (`0x01` prefix):**

| Key | Value |
|-----|-------|
| `0x01` + path + `\x00\x00` | raw file bytes (zero-copy servable, present only when inline) |
| `0x01` + path + `\x00\x01` | `DirectoryEntry` proto (metadata) |

**Content blobs (`0x02` prefix, keyed by blake3-256 hash):**

| Key | Value |
|-----|-------|
| `0x02` + hash(32) + `\x00` | raw file bytes (zero-copy servable) |
| `0x02` + hash(32) + `\x01` | `Digests` proto (SHA1, MD5, SHA256; blake3 is implicit from key) |
| `0x02` + hash(32) + `\x02` | `HashEntry` proto |

All three sub-keys share the 33-byte prefix, enabling Badger key prefix compression.

**Problem manifests (`0x04` prefix):**

| Key | Value |
|-----|-------|
| `0x04` + manifest key | JSON blob |

### DirectoryEntry proto

```proto
message DirectoryEntry {
    bytes blake3_hash = 1;
    string module_type = 2;
    int64 last_modified_timestamp = 3;
    Digests digests = 4;        // present when inline, wiped on externalization
    int64 data_size = 5;
    bool external = 6;
}
```

The `blake3_hash` is always stored for CAS lookup and integrity. When `external` is false, raw data lives at `0x01` + path + `\x00\x00` and digests are in this entry. When `external` is true, raw data lives at `0x02` + hash + `\x00` and digests are at `0x02` + hash + `\x01`.

On externalization, `digests` is cleared from the DirectoryEntry (moved to the shared `0x02` + hash + `\x01` entry) to avoid duplication across all referencing paths.

### HashEntry proto

```proto
message HashEntry {
    oneof state {
        PathList inline_paths = 1;
        int64 refcount = 2;
    }
}

message PathList {
    repeated string paths = 1;
}
```

Two states:
- **Pre-externalization** (`inline_paths`): lists all directory entry paths with this content stored inline.
- **Post-externalization** (`refcount`): counts directory entries referencing the external blob.

No `data_size` — callers know the size from the buffered upload body or the `DirectoryEntry.data_size` field.

### Inline vs External Decision

Directory entries always store the blake3 hash. The per-entry cost difference between inline and external is the `data_size` bytes of inline content. Externalizing N copies saves `(N-1) * data_size` bytes but costs a fixed blob overhead `B` (blob Badger entry for `0x02` key + digests entry + HashEntry conversion, approximately 50 bytes).

**Externalize when**: `(N-1) * data_size > B`

Examples (B=50):
- 100-byte value: externalize at N=2 (savings: 100 > 50)
- 10-byte value: externalize at N=7 (savings: 60 > 50)
- 2-byte value: externalize at N=26 (savings: 50 > 50)
- 1-byte value: externalize at N=51

The path list in `HashEntry` is bounded by `B / data_size + 1` entries. Worst case is ~50 entries for 1-byte values. After externalization the list is replaced by a refcount integer.

## Operations

### Upload

1. **Buffer body in memory**, computing blake3-256 (plus SHA1, MD5, SHA256 for HTTP digest headers) as data streams in.
2. **If hash was provided in HTTP header** (e.g., `X-Blake3-Hash`): compare against computed hash. Reject with error if mismatch (transit corruption).
3. **If overwriting an existing path**: handle the old content's hash within the same transaction (decrement refcount or remove from paths list — see Delete logic below).
4. **Look up `0x02` + hash + `\x02`** (HashEntry):
   - **Not found** (first upload of this content): write `HashEntry{inline_paths: [this_path]}`, write raw data to `0x01` + path + `\x00\x00`, write `DirectoryEntry` (inline, with digests) to `0x01` + path + `\x00\x01`. Single `db.Update` transaction.
   - **Found, pre-externalization** (`inline_paths`): add this path to the list. Check break-even. If threshold met: write blob to `0x02` + hash + `\x00`, write digests to `0x02` + hash + `\x01`, rewrite ALL listed directory entries (set `external=true`, clear digests, delete their `\x00\x00` raw data keys), convert `HashEntry` to `refcount` mode. If threshold not met: write raw data to `0x01` + path + `\x00\x00`, write `DirectoryEntry` with inline data, update paths list. Single `db.Update` transaction.
   - **Found, post-externalization** (`refcount`): increment refcount, write `DirectoryEntry` (external, no digests, no raw data key). Content blob already exists. Single `db.Update` transaction.

### Download

Single `db.View` transaction, zero-copy serving:

```go
db.View(func(tx *badger.Txn) error {
    // 1. Get DirectoryEntry at 0x01 + path + \x00\x01
    // 2. Extract metadata (data_size, module_type, timestamp)
    // 3a. If inline (external == false):
    //       digests from DirectoryEntry
    //       dataItem := tx.Get(0x01 + path + \x00\x00)
    // 3b. If external:
    //       digests from tx.Get(0x02 + hash + \x01)
    //       dataItem := tx.Get(0x02 + hash + \x00)
    // 4. dataItem.Value(func(v []byte) error {
    //        http.ServeContent(w, r, "", modTime, bytes.NewReader(v))
    //        return nil
    //    })
})
```

Both inline and external paths use the same zero-copy `item.Value` pattern. The read transaction stays open during HTTP serving. Acceptable for contest workloads (fast local clients, files < 128MB).

### Delete

Single `db.Update` transaction:

1. Get `DirectoryEntry` at `0x01` + path + `\x00\x01`. Extract hash.
2. Look up `0x02` + hash + `\x02` (HashEntry):
   - **Pre-externalization** (`inline_paths`): remove this path from list. If list empty, delete `0x02` + hash + `\x02`.
   - **Post-externalization** (`refcount`): decrement. If zero, delete `0x02` + hash + `\x00` (blob), `0x02` + hash + `\x01` (digests), and `0x02` + hash + `\x02` (HashEntry).
3. Delete `0x01` + path + `\x00\x00` (raw data, if inline) and `0x01` + path + `\x00\x01` (metadata).

### List

Badger prefix iterator over `0x01` + prefix with `PrefetchValues: false`. For each key, check that it ends with `\x00\x01` (metadata sub-key). Extract path as the bytes between the leading `0x01` and the trailing `\x00\x01`.

### Wipe

Iterate all `0x01` entries ending in `\x00\x01`, delete each via the Delete flow. Or for a full wipe, iterate all key prefixes and delete everything.

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
func (s *Store) Wipe(ctx context.Context) error
func (s *Store) Close()

// Manifest methods (problem metadata)
func (s *Store) GetManifest(ctx context.Context, key string) ([]byte, error)
func (s *Store) SetManifest(ctx context.Context, key string, value []byte) error
func (s *Store) ListManifests(ctx context.Context, prefix string) ([]string, error)
```

`Download` is callback-based: `DownloadResult` is valid only inside the callback (tied to the Badger read transaction lifetime).

```go
type DownloadResult struct {
    Size                 int64
    ModuleType           string
    LastModifiedTimestamp int64
    Digests              Digests
    Body                 io.ReadSeeker
}
```

### Upload Pre-check with Provided Hash

When the client sends a blake3 hash in the HTTP request header (e.g., `X-Blake3-Hash`):

1. Look up the hash before reading the body — know storage disposition in advance.
2. Read and hash the body regardless. Reject if computed hash doesn't match declared hash (transit corruption).
3. Proceed with CAS logic as normal.

### Package Layout

```
advfiler/
  main.go              — config, HTTP wiring, Badger open/close
  store.go             — Store struct, Upload/Download/Delete/List/Wipe/manifest methods
  filer.go             — HTTP handlers for /fs/, /tar/, /fs2/, /protopackage/, /wipe/
  metadata.go          — HTTP handlers for /problem/get/, /problem/set/
  authchecker.go       — bearer token auth (unchanged)
  hashes.go            — Digests, Hashes, blake3 helpers (extracted from common/)
  protos/              — proto definitions and generated code
  backup/              — CLI backup tool (unchanged)
```

The `common/`, `efbackend/`, `ldbackend/` packages are eliminated.

### Filer.go Changes

HTTP handlers call `Store` methods directly. Key changes from current code:

- `handleDownload`: uses callback-based `store.Download` — HTTP serving happens inside the callback.
- `handleUpload`: passes request body to `store.Upload`, which handles buffering and CAS internally.
- `handleTarUpload`/`handleTarDownload`: same pattern, iterating entries.
- `downloadAsset`, `handleProtoPackage`, `HandlePackage`, `writeRemoteFileAs`: adapted to callback-based download.
- `handleWipe`: calls `store.Wipe()`.

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

Removed from old versions: `FileMetadata`, `FileInfo`, `FileChunk`, `Inode`, `ChunkList`, `ThisChecksum`, `InodeVolatileAttributes`, `CompressionType`.

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

The following fixes from the efstore branch apply to `filer.go` and `metadata.go` regardless of backend. They will be incorporated during the handler rewrite:

1. **downloadAsset leak** (4f851f7): missing body close and error check after Read
2. **handleDownload/writeRemoteFileAs leaks** (1ea16e5): missing body close, `errors.Is()` for error checks
3. **ServeContent for proto packages** (c888a31): use `http.ServeContent` instead of raw `w.Write`
4. **limitReadSeeker** (5222504): replace `io.LimitReader`/`io.Copy` with `http.ServeContent` + seekable reader
5. **Double error write** (384bf54): remove `http.Error` after `log.Errorf` when response already written
6. **sort.Slice** (f09c4cf): replace sort.Interface boilerplate

With callback-based download (no Close needed — lifetime tied to transaction), leak fixes 1-2 are structurally eliminated. The limitReadSeeker is still needed for `X-Fs-Limit` support but operates on the `io.ReadSeeker` body within the callback.

## GC

Badger's value log GC runs periodically (the old code ran it hourly at 50% threshold). The new code should do the same via a background goroutine in `Store`.

No application-level GC is needed — refcount management is transactional and inline with upload/delete operations.
