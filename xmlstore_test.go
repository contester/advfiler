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
	a := problemRecordKey("k", 2)
	b := problemRecordKey("k", 10)
	if bytes.Compare(a, b) >= 0 {
		t.Fatalf("expected key(rev=2) < key(rev=10)")
	}
}
