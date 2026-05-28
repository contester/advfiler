package main

import (
	"bytes"
	"net/http"
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

func TestDigestsHeaderRoundTrip(t *testing.T) {
	h := NewHashes()
	h.Write([]byte("roundtrip"))
	d := h.Digests()

	hdr := http.Header{}
	AddDigests(hdr, d)
	d2 := ParseDigests(hdr)

	if !bytes.Equal(d.Blake3, d2.Blake3) {
		t.Fatal("Blake3 roundtrip failed")
	}
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

func TestVerifyDigests(t *testing.T) {
	h := NewHashes()
	h.Write([]byte("verify me"))
	d := h.Digests()

	// All matching received digests pass.
	if err := VerifyDigests(d, d); err != nil {
		t.Fatalf("matching digests should verify: %v", err)
	}
	// Empty received digests pass (nothing to check).
	if err := VerifyDigests(d, Digests{}); err != nil {
		t.Fatalf("empty received should verify: %v", err)
	}
	// A mismatched received digest fails.
	bad := Digests{SHA256: []byte("not the right hash")}
	if err := VerifyDigests(d, bad); err == nil {
		t.Fatal("mismatched digest should fail verification")
	}
}
