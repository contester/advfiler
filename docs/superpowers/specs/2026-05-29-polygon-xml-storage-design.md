# Advfiler: Polygon Contest/Problem XML Storage

A specialized storage API for Codeforces Polygon contest and problem XML documents. Contests are unversioned (single record with a timestamp); problems are versioned by integer revision.

## Context

Advfiler stores contest artifacts (test data, submissions) and JSON judge manifests. This feature adds storage for the *Polygon* descriptor XMLs:

- **Contest XML** — e.g. `https://polygon.codeforces.com/c/52378/contest.xml` (example: `director.0/contest-example.xml`). Lists a contest's problems and statements.
- **Problem XML** — e.g. `https://polygon.codeforces.com/p3fdVrf/PikMike/remove-colors/problem.xml` (example: `director.0/example-problem-2.xml`). Carries `revision="13"`, test/checker/solution metadata.

Advfiler treats both as **opaque blobs**: it never parses the XML. Keys, revisions, and timestamps are supplied by the client. This keeps storage decoupled from Polygon's XML schema.

This is distinct from the existing `metadataServer` (`/problem/set/`, `/problem/get/`), which stores JSON judge manifests under Badger prefix `0x04`. The new endpoints live under `/xml/` and use new prefixes.

## Requirements

**Contest:**
- Set: store content + timestamp under a client-supplied key. Overwrites any existing record.
- Get: return content + timestamp by key.
- Timestamp is client-supplied; if omitted, the server uses `time.Now()`.
- Re-setting byte-identical content still updates the timestamp (satisfied trivially by unconditional overwrite).

**Problem:**
- Set: store content + timestamp under a client-supplied key at a client-supplied integer revision. Re-setting an existing revision overwrites it.
- Get specific revision: return content + revision + timestamp.
- Get latest: return the numerically highest stored revision's content + revision + timestamp.
- Timestamp is client-supplied; if omitted, `time.Now()`.

**Out of scope (YAGNI):** delete, listing, content-size caps, XML parsing/validation.

## Storage Model (Approach A: dedicated proto records)

Reuses the existing `*Store` (same Badger DB). Two new key prefixes:

| Key | Value |
|-----|-------|
| `0x05` + contestKey | `ContestRecord` proto |
| `0x06` + problemKey + `\x00` + revision (big-endian uint64, 8 bytes) | `ProblemRecord` proto |

- Each contest is a single Badger entry; each problem revision is a single entry. Writes are atomic single Puts.
- Revision is encoded big-endian fixed-width so lexicographic key order equals numeric order. `GetLatestProblem` reverse-iterates the prefix `0x06` + problemKey + `\x00` and takes the first (highest) key, then reads its value.
- Problem keys contain `/` but never `\x00`, consistent with the store's existing delimiter convention (paths never contain null bytes).

### Proto additions (`protos/protos.proto`)

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

The `revision` field in `ProblemRecord` duplicates the key's revision; it is stored so reads can populate the response header without decoding the key. Opaque API (editions 2024): construct with `X_builder{...}.Build()`, read with `GetX()`. User regenerates `protos.pb.go`.

## Store API (methods on `*Store`)

```go
func (s *Store) SetContest(ctx context.Context, key string, content []byte, ts int64) error
func (s *Store) GetContest(ctx context.Context, key string) (content []byte, ts int64, err error)

func (s *Store) SetProblem(ctx context.Context, key string, revision int64, content []byte, ts int64) error
func (s *Store) GetProblem(ctx context.Context, key string, revision int64) (content []byte, ts int64, err error)
func (s *Store) GetLatestProblem(ctx context.Context, key string) (content []byte, revision int64, ts int64, err error)
```

- `ts == 0` ⇒ the method substitutes `time.Now().Unix()`.
- Missing records return an error wrapping `fs.ErrNotExist` (consistent with existing Store methods).
- `SetContest` / `SetProblem` are unconditional overwrites within a single `db.Update`.
- `GetLatestProblem` uses a `db.View` with a reverse iterator (`badger.IteratorOptions{Reverse: true}`) seeking to the end of the prefix range; if the prefix is empty it returns `fs.ErrNotExist`.

### Key builders (new, in the same file)

```go
func contestRecordKey(key string) []byte                  // 0x05 + key
func problemRecordKey(key string, revision int64) []byte  // 0x06 + key + 0x00 + be64(revision)
func problemRecordPrefix(key string) []byte               // 0x06 + key + 0x00  (for latest scan)
```

Names are suffixed `Record` to avoid colliding with the existing `problemKey` struct type in `metadata.go`. Prefix constants: `prefixContest = 0x05`, `prefixProblem = 0x06`.

## HTTP API

Query-parameter style, raw XML request/response body, guarded by the existing `AuthChecker` (`A_WRITE` for PUT, `A_READ` for GET). A new handler struct holds `*Store` and the `AuthCheck`.

| Method | Path + query | Body | Request headers | Response |
|--------|--------------|------|-----------------|----------|
| PUT | `/xml/contest/?key=52378` | XML | `X-Timestamp` (unix sec, optional) | 200 |
| GET | `/xml/contest/?key=52378` | — | — | XML body; `X-Timestamp`, `Last-Modified`; 404 if absent |
| PUT | `/xml/problem/?key=p3fdVrf/PikMike/remove-colors&revision=13` | XML | `X-Timestamp` (optional) | 200 |
| GET | `/xml/problem/?key=...&revision=13` | — | — | XML body; `X-Revision`, `X-Timestamp`, `Last-Modified`; 404 if absent |
| GET | `/xml/problem/?key=...` (no `revision`) | — | — | latest revision; same response headers as above |

Behavior:
- `key` is required; missing ⇒ 400.
- For problem PUT, `revision` is required and must parse as a non-negative integer; missing/invalid ⇒ 400.
- For problem GET, omitting `revision` selects the latest; an invalid `revision` value ⇒ 400.
- `X-Timestamp` request header: unix seconds; absent or unparseable ⇒ server uses `time.Now()`.
- GET responses serve the body via `http.ServeContent` (with the stored timestamp as the modtime) for range/conditional support, consistent with the filer.
- Not found ⇒ 404. Read of a problem with no revisions ⇒ 404.

## Files

- `protos/protos.proto` — add `ContestRecord`, `ProblemRecord` (user regenerates `protos.pb.go`).
- `xmlstore.go` (new) — prefixes, key builders, the five `*Store` methods.
- `xmlhandlers.go` (new) — handler struct + `handleContest` / `handleProblem` (dispatch on method) + a registration helper.
- `main.go` — construct the handler, register `/xml/contest/` and `/xml/problem/`.
- `xmlstore_test.go` (new) — Store-level tests.

## Error Handling

- Store methods wrap `fs.ErrNotExist` for missing records; handlers translate that to HTTP 404.
- Malformed request parameters ⇒ 400 with a short message.
- Storage/transaction errors ⇒ 500.
- Auth failure ⇒ 401.

## Testing

`xmlstore_test.go`, using the existing `newTestStore(t)` helper:

- **Contest round-trip:** set with explicit timestamp, get returns identical content + timestamp.
- **Contest timestamp default:** set with `ts == 0`, get returns a non-zero timestamp near now.
- **Contest timestamp bump:** set content C with ts=100, set identical content C with ts=200, get returns ts=200.
- **Contest not found:** get a missing key ⇒ `fs.ErrNotExist`.
- **Problem revisions:** set revisions 1, 5, 13 (out of order); get each returns its own content + revision.
- **Problem latest:** after the above, `GetLatestProblem` returns revision 13's content and revision=13.
- **Problem revision overwrite:** set revision 5 twice with different content/timestamp; get returns the second.
- **Problem not found:** get a missing key/revision, and `GetLatestProblem` on a missing key ⇒ `fs.ErrNotExist`.

HTTP handler tests are optional; the Store-level tests cover the core logic.
