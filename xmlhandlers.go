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
	key := r.URL.Query().Get("key")
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
	key := r.URL.Query().Get("key")
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
		rev, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
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
		if revStr := r.URL.Query().Get("revision"); revStr != "" {
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
