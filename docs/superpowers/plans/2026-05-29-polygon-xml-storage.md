# Polygon Contest/Problem XML Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opaque-blob storage for Polygon contest XML (unversioned, timestamped) and problem XML (versioned by integer revision) to advfiler, exposed via `/xml/contest/` and `/xml/problem/` query-param HTTP endpoints.

**Architecture:** Two new Badger key prefixes (`0x05` contest, `0x06` problem) on the existing `*Store`. Each contest is one proto record; each problem revision is one proto record keyed with a big-endian revision suffix so the latest is a reverse-scan. HTTP handlers reuse the existing `AuthChecker`.

**Tech Stack:** Go 1.25, Badger v4, protobuf (editions 2024, opaque API), existing advfiler `Store`.

**Spec:** `docs/superpowers/specs/2026-05-29-polygon-xml-storage-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `protos/protos.proto` | Modify | Add `ContestRecord`, `ProblemRecord` messages |
| `protos/protos.pb.go` | Regenerate (USER) | Generated opaque API for the new messages |
| `xmlstore.go` | Create | Prefixes, key builders, 5 `*Store` methods (contest/problem set/get/latest) |
| `xmlstore_test.go` | Create | Store-level tests |
| `xmlhandlers.go` | Create | `xmlServer` struct + `handleContest`/`handleProblem` + timestamp header parse |
| `main.go` | Modify | Construct `xmlServer`, register `/xml/contest/` and `/xml/problem/` |

**Convention notes (from reading the existing `store.go`):**
- Key builders return `[]byte` built by appending a single prefix byte then content.
- Read helper: `getProto[T, PT](tx, key)` returns `(PT, error)`; on missing key it returns `badger.ErrKeyNotFound`, which public methods translate to a wrapped `fs.ErrNotExist`.
- Write helper: `setProto(tx, key, msg)`.
- Opaque proto API: build with `pb.X_builder{...}.Build()`, read with `GetX()`.
- `proto.Unmarshal` copies `bytes` fields, so values read inside a txn callback remain valid after the txn closes (same assumption the existing code relies on).
- `AuthCheck` interface and `tokenFromHeader(r)` live in `filer.go`; `AuthChecker` concrete value is built in `main.go` as `authCheck` and passed by address.

---

### Task 1: Proto messages

**Files:**
- Modify: `protos/protos.proto`
- Regenerate (USER): `protos/protos.pb.go`

- [ ] **Step 1: Add the two messages to protos.proto**

Insert after the existing `Digests` / `DigestsAndSize` messages (anywhere among the top-level messages is fine; place near the other storage records):

```proto
message ContestRecord {
    bytes content = 1;
    int64 timestamp_unix = 2;
}

message ProblemRecord {
    bytes content = 1;
    int64 timestamp_unix = 2;
    int64 revision = 3;
}
```

- [ ] **Step 2: Commit the proto source change**

```bash
git add protos/protos.proto
git commit -m "proto: add ContestRecord and ProblemRecord"
```

- [ ] **Step 3: HANDOFF — user regenerates protos.pb.go**

The user regenerates the generated Go code themselves (their workflow; do not run `protoc`/`go generate`). Their command (from `protos/protos.go`) is:
`protoc --go_out=. --go_opt=paths=source_relative protos.proto`

Verify the new types exist before proceeding to Task 2:

Run: `grep -c 'ContestRecord_builder\|ProblemRecord_builder' protos/protos.pb.go`
Expected: `2` (both builders present).

If the count is 0, stop and ask the user to regenerate. Then commit the regenerated file:

```bash
git add protos/protos.pb.go
git commit -m "proto: regenerate for ContestRecord/ProblemRecord"
```

---

### Task 2: Store methods (xmlstore.go)

**Files:**
- Create: `xmlstore.go`
- Create: `xmlstore_test.go`

Depends on Task 1's regenerated `pb.ContestRecord` / `pb.ProblemRecord`.

- [ ] **Step 1: Write xmlstore_test.go (failing tests)**

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
)

func TestContestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetContest(ctx, "52378", []byte("<contest/>"), 1000); err != nil {
		t.Fatal(err)
	}
	content, ts, err := s.GetContest(ctx, "52378")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<contest/>" {
		t.Fatalf("content = %q, want %q", content, "<contest/>")
	}
	if ts != 1000 {
		t.Fatalf("ts = %d, want 1000", ts)
	}
}

func TestContestTimestampDefault(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetContest(ctx, "c", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	_, ts, err := s.GetContest(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	if ts == 0 {
		t.Fatal("expected a non-zero default timestamp")
	}
}

func TestContestTimestampBumpOnIdenticalContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetContest(ctx, "c", []byte("same"), 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetContest(ctx, "c", []byte("same"), 200); err != nil {
		t.Fatal(err)
	}
	_, ts, err := s.GetContest(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	if ts != 200 {
		t.Fatalf("ts = %d, want 200 (timestamp must bump even when content is identical)", ts)
	}
}

func TestContestNotFound(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.GetContest(context.Background(), "missing")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestProblemRevisions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Set out of order.
	for _, rev := range []int64{5, 13, 1} {
		content := []byte("rev" + string(rune('0'+rev%10)))
		if err := s.SetProblem(ctx, "p/k", rev, content, rev*10); err != nil {
			t.Fatal(err)
		}
	}

	for _, rev := range []int64{1, 5, 13} {
		content, ts, err := s.GetProblem(ctx, "p/k", rev)
		if err != nil {
			t.Fatalf("rev %d: %v", rev, err)
		}
		want := "rev" + string(rune('0'+rev%10))
		if string(content) != want {
			t.Fatalf("rev %d content = %q, want %q", rev, content, want)
		}
		if ts != rev*10 {
			t.Fatalf("rev %d ts = %d, want %d", rev, ts, rev*10)
		}
	}
}

func TestProblemLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, rev := range []int64{5, 13, 1} {
		if err := s.SetProblem(ctx, "p/k", rev, []byte("body"), 0); err != nil {
			t.Fatal(err)
		}
	}
	content, rev, _, err := s.GetLatestProblem(ctx, "p/k")
	if err != nil {
		t.Fatal(err)
	}
	if rev != 13 {
		t.Fatalf("latest rev = %d, want 13", rev)
	}
	if string(content) != "body" {
		t.Fatalf("latest content = %q, want %q", content, "body")
	}
}

func TestProblemRevisionOverwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetProblem(ctx, "p/k", 5, []byte("first"), 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProblem(ctx, "p/k", 5, []byte("second"), 200); err != nil {
		t.Fatal(err)
	}
	content, ts, err := s.GetProblem(ctx, "p/k", 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "second" || ts != 200 {
		t.Fatalf("got (%q, %d), want (second, 200)", content, ts)
	}
}

func TestProblemNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, _, err := s.GetProblem(ctx, "nope", 1); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("GetProblem err = %v, want fs.ErrNotExist", err)
	}
	if _, _, _, err := s.GetLatestProblem(ctx, "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("GetLatestProblem err = %v, want fs.ErrNotExist", err)
	}
}

func TestProblemKeyOrdering(t *testing.T) {
	// big-endian revision suffix must sort numerically
	a := problemRecordKey("k", 2)
	b := problemRecordKey("k", 10)
	if bytes.Compare(a, b) >= 0 {
		t.Fatalf("expected key(rev=2) < key(rev=10)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestContest|TestProblem' -v .`
Expected: FAIL — `SetContest`, `problemRecordKey`, etc. undefined (compile error).

- [ ] **Step 3: Write xmlstore.go**

```go
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"time"

	"github.com/dgraph-io/badger/v4"
	pb "github.com/contester/advfiler/protos"
	"google.golang.org/protobuf/proto"
)

// Key prefixes for Polygon XML records.
const (
	prefixContest byte = 0x05
	prefixProblem byte = 0x06
)

// contestRecordKey: 0x05 + key
func contestRecordKey(key string) []byte {
	k := make([]byte, 0, 1+len(key))
	k = append(k, prefixContest)
	k = append(k, key...)
	return k
}

// problemRecordPrefix: 0x06 + key + 0x00  (all revisions of a problem)
func problemRecordPrefix(key string) []byte {
	k := make([]byte, 0, 1+len(key)+1)
	k = append(k, prefixProblem)
	k = append(k, key...)
	k = append(k, 0x00)
	return k
}

// problemRecordKey: 0x06 + key + 0x00 + be64(revision)
func problemRecordKey(key string, revision int64) []byte {
	k := problemRecordPrefix(key)
	var rev [8]byte
	binary.BigEndian.PutUint64(rev[:], uint64(revision))
	return append(k, rev[:]...)
}

func nowIfZero(ts int64) int64 {
	if ts == 0 {
		return time.Now().Unix()
	}
	return ts
}

// SetContest stores (or overwrites) a contest's XML and timestamp.
// ts == 0 means "use the current time".
func (s *Store) SetContest(ctx context.Context, key string, content []byte, ts int64) error {
	rec := pb.ContestRecord_builder{
		Content:       content,
		TimestampUnix: proto.Int64(nowIfZero(ts)),
	}.Build()
	return s.db.Update(func(tx *badger.Txn) error {
		return setProto(tx, contestRecordKey(key), rec)
	})
}

// GetContest returns a contest's XML and timestamp, or a wrapped fs.ErrNotExist.
func (s *Store) GetContest(ctx context.Context, key string) (content []byte, ts int64, err error) {
	err = s.db.View(func(tx *badger.Txn) error {
		rec, gerr := getProto[pb.ContestRecord](tx, contestRecordKey(key))
		if gerr == badger.ErrKeyNotFound {
			return fmt.Errorf("%w: contest %s", fs.ErrNotExist, key)
		}
		if gerr != nil {
			return gerr
		}
		content = rec.GetContent()
		ts = rec.GetTimestampUnix()
		return nil
	})
	return content, ts, err
}

// SetProblem stores (or overwrites) one revision of a problem.
// ts == 0 means "use the current time".
func (s *Store) SetProblem(ctx context.Context, key string, revision int64, content []byte, ts int64) error {
	rec := pb.ProblemRecord_builder{
		Content:       content,
		TimestampUnix: proto.Int64(nowIfZero(ts)),
		Revision:      proto.Int64(revision),
	}.Build()
	return s.db.Update(func(tx *badger.Txn) error {
		return setProto(tx, problemRecordKey(key, revision), rec)
	})
}

// GetProblem returns a specific revision's XML and timestamp.
func (s *Store) GetProblem(ctx context.Context, key string, revision int64) (content []byte, ts int64, err error) {
	err = s.db.View(func(tx *badger.Txn) error {
		rec, gerr := getProto[pb.ProblemRecord](tx, problemRecordKey(key, revision))
		if gerr == badger.ErrKeyNotFound {
			return fmt.Errorf("%w: problem %s revision %d", fs.ErrNotExist, key, revision)
		}
		if gerr != nil {
			return gerr
		}
		content = rec.GetContent()
		ts = rec.GetTimestampUnix()
		return nil
	})
	return content, ts, err
}

// GetLatestProblem returns the highest-numbered revision of a problem.
func (s *Store) GetLatestProblem(ctx context.Context, key string) (content []byte, revision int64, ts int64, err error) {
	prefix := problemRecordPrefix(key)
	err = s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = prefix
		it := tx.NewIterator(opts)
		defer it.Close()

		// Seek past the largest possible key under the prefix so reverse
		// iteration lands on the highest revision.
		seek := append(append([]byte{}, prefix...), 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
		it.Seek(seek)
		if !it.ValidForPrefix(prefix) {
			return fmt.Errorf("%w: problem %s", fs.ErrNotExist, key)
		}
		return it.Item().Value(func(v []byte) error {
			var rec pb.ProblemRecord
			if uerr := proto.Unmarshal(v, &rec); uerr != nil {
				return uerr
			}
			content = rec.GetContent()
			revision = rec.GetRevision()
			ts = rec.GetTimestampUnix()
			return nil
		})
	})
	return content, revision, ts, err
}
```

`proto.Int64` and `proto.Unmarshal` come from `google.golang.org/protobuf/proto` (already used the same way in `store.go`); `nowIfZero` is the small helper defined above. No extra helper functions are needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestContest|TestProblem' -v .`
Expected: PASS for all eight tests.

- [ ] **Step 5: Run the full suite + vet**

Run: `go test -count=1 . && go vet .`
Expected: `ok` and no vet output.

- [ ] **Step 6: Commit**

```bash
git add xmlstore.go xmlstore_test.go
git commit -m "feat: store methods for Polygon contest/problem XML records"
```

---

### Task 3: HTTP handlers + wiring

**Files:**
- Create: `xmlhandlers.go`
- Modify: `main.go`

- [ ] **Step 1: Write xmlhandlers.go**

```go
package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	pb "github.com/contester/advfiler/protos"
)

type xmlServer struct {
	store       *Store
	authChecker AuthCheck
}

func NewXMLServer(store *Store, authChecker AuthCheck) *xmlServer {
	return &xmlServer{store: store, authChecker: authChecker}
}

// parseTimestampHeader reads X-Timestamp (unix seconds). Returns 0 when absent
// or unparseable, which the Store treats as "use the current time".
func parseTimestampHeader(r *http.Request) int64 {
	if v := r.Header.Get("X-Timestamp"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			return ts
		}
	}
	return 0
}

func (x *xmlServer) handleContest(w http.ResponseWriter, r *http.Request) {
	key := r.FormValue("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	switch r.Method {
	case http.MethodPut, http.MethodPost:
		if v, _ := x.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_WRITE, key); !v {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := x.store.SetContest(ctx, key, body, parseTimestampHeader(r)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case http.MethodGet, http.MethodHead:
		if v, _ := x.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, key); !v {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		content, ts, err := x.store.GetContest(ctx, key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Timestamp", strconv.FormatInt(ts, 10))
		http.ServeContent(w, r, "", time.Unix(ts, 0), bytes.NewReader(content))

	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}

func (x *xmlServer) handleProblem(w http.ResponseWriter, r *http.Request) {
	key := r.FormValue("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	switch r.Method {
	case http.MethodPut, http.MethodPost:
		if v, _ := x.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_WRITE, key); !v {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rev, err := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		if err != nil || rev < 0 {
			http.Error(w, "missing or invalid revision", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := x.store.SetProblem(ctx, key, rev, body, parseTimestampHeader(r)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case http.MethodGet, http.MethodHead:
		if v, _ := x.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, key); !v {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var (
			content []byte
			rev, ts int64
			err     error
		)
		if revStr := r.FormValue("revision"); revStr != "" {
			rev, err = strconv.ParseInt(revStr, 10, 64)
			if err != nil || rev < 0 {
				http.Error(w, "invalid revision", http.StatusBadRequest)
				return
			}
			content, ts, err = x.store.GetProblem(ctx, key, rev)
		} else {
			content, rev, ts, err = x.store.GetLatestProblem(ctx, key)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Revision", strconv.FormatInt(rev, 10))
		w.Header().Set("X-Timestamp", strconv.FormatInt(ts, 10))
		http.ServeContent(w, r, "", time.Unix(ts, 0), bytes.NewReader(content))

	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}
```

- [ ] **Step 2: Wire routes in main.go**

In `main.go`, after the existing `ms := NewMetadataServer(store)` / handler registrations, add construction and two routes. Locate this block:

```go
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
```

Replace it with (adds `xs` plus two routes):

```go
	f := NewFiler(store, &authCheck)
	ms := NewMetadataServer(store)
	xs := NewXMLServer(store, &authCheck)
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.HandleFunc("/tar/", f.handleTarUpload)
	http.HandleFunc("/wipe/", f.handleWipe)
	http.HandleFunc("/protopackage/", f.handleProtoPackage)
	http.HandleFunc("/protopackage", f.handleProtoPackage)
	http.HandleFunc("/xml/contest/", xs.handleContest)
	http.HandleFunc("/xml/problem/", xs.handleProblem)
```

- [ ] **Step 3: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: clean build, no vet output.

- [ ] **Step 4: Run full test suite**

Run: `go test -count=1 .`
Expected: `ok github.com/contester/advfiler`.

- [ ] **Step 5: Commit**

```bash
git add xmlhandlers.go main.go
git commit -m "feat: /xml/contest/ and /xml/problem/ HTTP endpoints"
```

---

## Self-Review

**Spec coverage:**
- ✅ Contest set/get with client timestamp + `time.Now()` fallback — `SetContest`/`GetContest` + `nowIfZero`; Task 2.
- ✅ Contest timestamp bump on identical content — unconditional overwrite; `TestContestTimestampBumpOnIdenticalContent`.
- ✅ Problem set with revision, multiple revisions, get specific, get latest — `SetProblem`/`GetProblem`/`GetLatestProblem`; Task 2.
- ✅ Integer revision, latest = highest — big-endian key suffix + reverse scan; `TestProblemLatest`, `TestProblemKeyOrdering`.
- ✅ Revision overwrite — `TestProblemRevisionOverwrite`.
- ✅ Opaque blobs, no XML parsing — Store stores raw `content` bytes; no parsing anywhere.
- ✅ Storage prefixes `0x05`/`0x06`, proto records — Task 1 + Task 2.
- ✅ Query-param HTTP API, raw body, `X-Timestamp`/`X-Revision` headers, no-revision-GET = latest, auth — Task 3.
- ✅ 404 on missing, 400 on bad params — handlers in Task 3.
- ✅ Out of scope (delete/list/caps/parsing) — not implemented, as intended.

**Placeholder scan:** No TBD/TODO. All steps contain concrete code or exact commands. The one prose note (Task 2 Step 4) explicitly corrects the import placement and shows the final import block.

**Type/name consistency:**
- `pb.ContestRecord` / `pb.ProblemRecord` with builders `ContestRecord_builder{Content, TimestampUnix}` and `ProblemRecord_builder{Content, TimestampUnix, Revision}` — fields match the proto in Task 1 (opaque builder fields are exported names of proto fields; scalar fields take pointers, hence `proto64`).
- Key builders `contestRecordKey`, `problemRecordKey`, `problemRecordPrefix` — used consistently in Task 2 and the ordering test. No collision with the `problemKey` struct in `metadata.go` (verified).
- Store methods `SetContest`/`GetContest`/`SetProblem`/`GetProblem`/`GetLatestProblem` — identical signatures across Task 2 definitions, Task 2 tests, and Task 3 handler call sites.
- `xmlServer`, `NewXMLServer`, `handleContest`, `handleProblem`, `parseTimestampHeader` — defined in Task 3, `xs` wired in `main.go`. No collision with `filerServer`/`metadataServer`.
- Reused existing symbols: `AuthCheck` (interface, filer.go), `tokenFromHeader` (filer.go), `getProto`/`setProto` (store.go), `newTestStore` (store_test.go), `pb.AuthAction_A_READ`/`_A_WRITE`.

**Note on opaque builder pointer fields:** `TimestampUnix` and `Revision` are `int64` proto fields; in the opaque builder they are `*int64`, so the plan uses `proto.Int64(...)` to take addresses (exactly as `store.go` already does for `DirectoryEntry`/`DigestsAndSize`). `Content` is `bytes` → `[]byte` (no pointer).
