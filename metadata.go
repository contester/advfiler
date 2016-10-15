package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"gopkg.in/redis.v4"
)

type metadataServer struct {
	kv filerKV
}

func NewMetadataServer(kv filerKV) *metadataServer {
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
	if r.Method != "PUT" && r.Method != "POST" {
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
	if r.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
}

func revKey(id string, rev int) string {
	return id + "/" + strconv.Itoa(rev)
}

func (f *metadataServer) getManifest(ctx context.Context, key string) (problemManifest, error) {
	var result problemManifest
	err := KVGetJson(ctx, f.kv, key, &result)
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
	sort.Sort(byIdRev(result))
	return result, nil
}

func (f *metadataServer) buildKeys(ctx context.Context, pk problemKey) ([]string, error) {
	if pk.Revision == 0 {
		return []string{revKey(pk.Id, pk.Revision)}, nil
	}
	return f.kv.List(ctx, pk.Id)
}

type byIdRev []problemManifest

func (s byIdRev) Len() int      { return len(s) }
func (s byIdRev) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byIdRev) Less(i, j int) bool {
	if s[i].Id < s[j].Id {
		return true
	}
	if s[j].Id > s[j].Id {
		return false
	}
	return s[i].Revision > s[j].Revision
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

	fmt.Printf("Parsed key: %+v", pk)

	revs, err := f.getK(r.Context(), pk)
	if err != nil {
		if err == redis.Nil {
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
