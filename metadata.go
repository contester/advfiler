package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/contester/advfiler/common"
	"stingr.net/go/efstore/efcommon"
)

type metadataServer struct {
	kv common.DB
}

func NewMetadataServer(kv common.DB) *metadataServer {
	return &metadataServer{
		kv: kv,
	}
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
	if err = f.kv.Set(r.Context(), revKey(mf.Id, mf.Revision), mb); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return
}

func (f *metadataServer) handleDelManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
}

func revKey(id string, rev int) string {
	return id + "/" + strconv.Itoa(rev)
}

func (f *metadataServer) getManifest(ctx context.Context, key string) (problemManifest, error) {
	var result problemManifest
	err := common.KVGetJson(ctx, f.kv, key, &result)
	return result, err
}

func (f *metadataServer) getK(ctx context.Context, pk problemKey) ([]problemManifest, error) {
	keys, err := f.buildKeys(ctx, pk)
	if err != nil {
		return nil, err
	}
	result := make([]problemManifest, 0, len(keys))
	for _, v := range keys {
		if m, err := f.getManifest(ctx, v); err == nil {
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

func (f *metadataServer) buildKeys(ctx context.Context, pk problemKey) ([]string, error) {
	if pk.Revision != 0 {
		return []string{revKey(pk.Id, pk.Revision)}, nil
	}
	return f.kv.List(ctx, pk.Id)
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
	// log.Infof("gm: %v", r)
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

	revs, err := f.getK(r.Context(), pk)
	if err != nil {
		if errors.Is(err, efcommon.ErrNotFound) {
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
