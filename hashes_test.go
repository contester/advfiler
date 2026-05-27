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
