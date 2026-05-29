package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
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
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}

// Key layout tests

func TestDirDataKey(t *testing.T) {
	k := dirDataKey("foo/bar")
	if k[0] != prefixDirEntry {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixDirEntry, k[0])
	}
	if k[len(k)-2] != 0x00 || k[len(k)-1] != subkeyInlineData {
		t.Fatalf("expected suffix 0x00 0x00, got 0x%02x 0x%02x", k[len(k)-2], k[len(k)-1])
	}
	if string(k[1:len(k)-2]) != "foo/bar" {
		t.Fatalf("expected path 'foo/bar', got %q", string(k[1:len(k)-2]))
	}
}

func TestDirMetaKey(t *testing.T) {
	k := dirMetaKey("foo/bar")
	if k[0] != prefixDirEntry {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixDirEntry, k[0])
	}
	if k[len(k)-2] != 0x00 || k[len(k)-1] != subkeyDirMeta {
		t.Fatalf("expected suffix 0x00 0x01, got 0x%02x 0x%02x", k[len(k)-2], k[len(k)-1])
	}
	if string(k[1:len(k)-2]) != "foo/bar" {
		t.Fatalf("expected path 'foo/bar', got %q", string(k[1:len(k)-2]))
	}
}

func TestBlobKey(t *testing.T) {
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	k := blobDataKey(hash)
	if k[0] != prefixBlob {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixBlob, k[0])
	}
	if !bytes.Equal(k[1:33], hash) {
		t.Fatal("hash bytes mismatch")
	}
	if k[33] != subkeyBlobData {
		t.Fatalf("expected subkey 0x%02x, got 0x%02x", subkeyBlobData, k[33])
	}
}

func TestBlobDigestsKey(t *testing.T) {
	hash := make([]byte, 32)
	k := blobDigestsKey(hash)
	if k[0] != prefixBlob {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixBlob, k[0])
	}
	if k[33] != subkeyBlobDigests {
		t.Fatalf("expected subkey 0x%02x, got 0x%02x", subkeyBlobDigests, k[33])
	}
}

func TestBlobHashEntryKey(t *testing.T) {
	hash := make([]byte, 32)
	k := blobHashEntryKey(hash)
	if k[0] != prefixBlob {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixBlob, k[0])
	}
	if k[33] != subkeyBlobHash {
		t.Fatalf("expected subkey 0x%02x, got 0x%02x", subkeyBlobHash, k[33])
	}
}

func TestManifestKey(t *testing.T) {
	k := manifestKey("some/key")
	if k[0] != prefixManifest {
		t.Fatalf("expected prefix 0x%02x, got 0x%02x", prefixManifest, k[0])
	}
	if string(k[1:]) != "some/key" {
		t.Fatalf("expected key 'some/key', got %q", string(k[1:]))
	}
}

func TestExtractPathFromDirMetaKey(t *testing.T) {
	// Roundtrip test.
	path := "foo/bar/baz"
	k := dirMetaKey(path)
	got, err := extractPathFromDirMetaKey(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}

	// Rejection of data key.
	dataK := dirDataKey(path)
	_, err = extractPathFromDirMetaKey(dataK)
	if err == nil {
		t.Fatal("expected error for data key, got nil")
	}

	// Short key.
	_, err = extractPathFromDirMetaKey([]byte{prefixDirEntry, 0x00})
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	content := "hello"
	fi := FileInfo{Name: "test/hello.txt", ModuleType: "txt"}
	status, err := s.Upload(ctx, fi, strings.NewReader(content))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if status.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), status.Size)
	}

	err = s.Download(ctx, "test/hello.txt", func(dr DownloadResult) error {
		if dr.Size != int64(len(content)) {
			t.Errorf("expected size %d, got %d", len(content), dr.Size)
		}
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(got) != content {
			t.Errorf("expected %q, got %q", content, string(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
}

func TestUploadDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 100-byte content. shouldExternalize at N=2: (2-1)*100 = 100 > 50 → externalize.
	content := strings.Repeat("x", 100)

	paths := []string{"dedup/a", "dedup/b", "dedup/c"}
	for _, p := range paths {
		fi := FileInfo{Name: p}
		_, err := s.Upload(ctx, fi, strings.NewReader(content))
		if err != nil {
			t.Fatalf("upload %s failed: %v", p, err)
		}
	}

	// All paths must be downloadable and return correct content.
	for _, p := range paths {
		err := s.Download(ctx, p, func(dr DownloadResult) error {
			got, err := io.ReadAll(dr.Body)
			if err != nil {
				return err
			}
			if string(got) != content {
				t.Errorf("path %s: expected %q, got %q", p, content, string(got))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("download %s failed: %v", p, err)
		}
	}
}

func TestDownloadNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.Download(ctx, "nonexistent/path", func(dr DownloadResult) error {
		return nil
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestUploadOverwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fi := FileInfo{Name: "overwrite/file"}
	_, err := s.Upload(ctx, fi, strings.NewReader("old"))
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}

	_, err = s.Upload(ctx, fi, strings.NewReader("new"))
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}

	err = s.Download(ctx, "overwrite/file", func(dr DownloadResult) error {
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(got) != "new" {
			t.Errorf("expected 'new', got %q", string(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download after overwrite failed: %v", err)
	}
}

func TestUploadZeroSize(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	status, err := s.Upload(ctx, FileInfo{Name: "empty/file", ModuleType: "txt"}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if status.Size != 0 {
		t.Fatalf("expected size 0, got %d", status.Size)
	}

	err = s.Download(ctx, "empty/file", func(dr DownloadResult) error {
		if dr.Size != 0 {
			t.Errorf("expected size 0, got %d", dr.Size)
		}
		if dr.ModuleType != "txt" {
			t.Errorf("expected module type txt, got %q", dr.ModuleType)
		}
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if len(got) != 0 {
			t.Errorf("expected empty body, got %q", got)
		}
		// Digests are synthesized from empty input.
		if !bytes.Equal(dr.Digests.SHA256, emptyDigests.SHA256) {
			t.Errorf("sha256 not the empty-input digest")
		}
		if !bytes.Equal(dr.Digests.Blake3, emptyDigests.Blake3) {
			t.Errorf("blake3 not the empty-input digest")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
}

func TestUploadZeroSizeOverwriteTransitions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	fi := FileInfo{Name: "transition/file"}

	// non-zero -> zero -> non-zero, verifying content each step.
	steps := []string{"hello", "", "world"}
	for _, content := range steps {
		if _, err := s.Upload(ctx, fi, strings.NewReader(content)); err != nil {
			t.Fatalf("upload %q failed: %v", content, err)
		}
		err := s.Download(ctx, fi.Name, func(dr DownloadResult) error {
			got, err := io.ReadAll(dr.Body)
			if err != nil {
				return err
			}
			if string(got) != content {
				t.Errorf("expected %q, got %q", content, got)
			}
			if dr.Size != int64(len(content)) {
				t.Errorf("expected size %d, got %d", len(content), dr.Size)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("download after %q failed: %v", content, err)
		}
	}

	// After the transitions, deleting the final entry should succeed.
	if err := s.Delete(ctx, fi.Name); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fi := FileInfo{Name: "delete/file"}
	_, err := s.Upload(ctx, fi, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if err := s.Delete(ctx, "delete/file"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	err = s.Download(ctx, "delete/file", func(dr DownloadResult) error {
		return nil
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist after delete, got %v", err)
	}
}

func TestDeleteDedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 100-byte content → externalized at N=2.
	content := strings.Repeat("y", 100)

	_, err := s.Upload(ctx, FileInfo{Name: "dd/a"}, strings.NewReader(content))
	if err != nil {
		t.Fatalf("upload a failed: %v", err)
	}
	_, err = s.Upload(ctx, FileInfo{Name: "dd/b"}, strings.NewReader(content))
	if err != nil {
		t.Fatalf("upload b failed: %v", err)
	}

	// Delete a, b should still work.
	if err := s.Delete(ctx, "dd/a"); err != nil {
		t.Fatalf("delete a failed: %v", err)
	}

	err = s.Download(ctx, "dd/b", func(dr DownloadResult) error {
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(got) != content {
			t.Errorf("expected content for b, got %q", string(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download b after deleting a failed: %v", err)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, p := range []string{"dir/a", "dir/b", "other/c"} {
		_, err := s.Upload(ctx, FileInfo{Name: p}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("upload %s failed: %v", p, err)
		}
	}

	names, err := s.List(ctx, "dir/")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names under dir/, got %d: %v", len(names), names)
	}
}

func TestWipe(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, p := range []string{"wipe/a", "wipe/b"} {
		_, err := s.Upload(ctx, FileInfo{Name: p}, strings.NewReader("data"))
		if err != nil {
			t.Fatalf("upload %s failed: %v", p, err)
		}
	}

	if err := s.Wipe(ctx); err != nil {
		t.Fatalf("wipe failed: %v", err)
	}

	names, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("list after wipe failed: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected 0 files after wipe, got %d: %v", len(names), names)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	key := "problem/test/1"
	value := []byte(`{"id":"test","revision":1}`)

	if err := s.SetManifest(ctx, key, value); err != nil {
		t.Fatalf("set manifest failed: %v", err)
	}

	got, err := s.GetManifest(ctx, key)
	if err != nil {
		t.Fatalf("get manifest failed: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("expected %q, got %q", value, got)
	}
}

func TestListManifests(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, k := range []string{"problem/a/1", "problem/a/2", "problem/b/1"} {
		if err := s.SetManifest(ctx, k, []byte(`{}`)); err != nil {
			t.Fatalf("set manifest %s failed: %v", k, err)
		}
	}

	keys, err := s.ListManifests(ctx, "problem/a/")
	if err != nil {
		t.Fatalf("list manifests failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 manifests under problem/a/, got %d: %v", len(keys), keys)
	}
}

func TestIntegrationLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Upload files.
	content1 := strings.Repeat("a", 100)
	content2 := "unique-content"

	_, err := s.Upload(ctx, FileInfo{Name: "life/file1"}, strings.NewReader(content1))
	if err != nil {
		t.Fatalf("upload file1: %v", err)
	}
	_, err = s.Upload(ctx, FileInfo{Name: "life/file2"}, strings.NewReader(content2))
	if err != nil {
		t.Fatalf("upload file2: %v", err)
	}

	// List.
	names, err := s.List(ctx, "life/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 files, got %d", len(names))
	}

	// Download.
	err = s.Download(ctx, "life/file2", func(dr DownloadResult) error {
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(got) != content2 {
			t.Errorf("content mismatch: expected %q, got %q", content2, string(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download file2: %v", err)
	}

	// Overwrite.
	_, err = s.Upload(ctx, FileInfo{Name: "life/file2"}, strings.NewReader("updated"))
	if err != nil {
		t.Fatalf("overwrite file2: %v", err)
	}
	err = s.Download(ctx, "life/file2", func(dr DownloadResult) error {
		got, err := io.ReadAll(dr.Body)
		if err != nil {
			return err
		}
		if string(got) != "updated" {
			t.Errorf("expected 'updated', got %q", string(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("download after overwrite: %v", err)
	}

	// Dedup: upload content1 to a second path.
	_, err = s.Upload(ctx, FileInfo{Name: "life/file1b"}, strings.NewReader(content1))
	if err != nil {
		t.Fatalf("upload file1b: %v", err)
	}
	// Both should still be accessible.
	for _, p := range []string{"life/file1", "life/file1b"} {
		err = s.Download(ctx, p, func(dr DownloadResult) error {
			got, err := io.ReadAll(dr.Body)
			if err != nil {
				return err
			}
			if string(got) != content1 {
				t.Errorf("%s: content mismatch", p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("download %s: %v", p, err)
		}
	}

	// Delete.
	if err := s.Delete(ctx, "life/file1"); err != nil {
		t.Fatalf("delete file1: %v", err)
	}
	err = s.Download(ctx, "life/file1", func(dr DownloadResult) error { return nil })
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected ErrNotExist after delete, got %v", err)
	}

	// Manifest.
	if err := s.SetManifest(ctx, "m/key1", []byte("val1")); err != nil {
		t.Fatalf("set manifest: %v", err)
	}
	if err := s.SetManifest(ctx, "m/key2", []byte("val2")); err != nil {
		t.Fatalf("set manifest 2: %v", err)
	}
	mkeys, err := s.ListManifests(ctx, "m/")
	if err != nil {
		t.Fatalf("list manifests: %v", err)
	}
	if len(mkeys) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(mkeys))
	}

	// Wipe.
	if err := s.Wipe(ctx); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	names, err = s.List(ctx, "")
	if err != nil {
		t.Fatalf("list after wipe: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty after wipe, got %d files", len(names))
	}
}
