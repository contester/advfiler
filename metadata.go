package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"gopkg.in/redis.v4"
)

type metadataServer struct {
	kv filerKV
}

func NewMetadataServer(client *redis.Client) *metadataServer {
	return &metadataServer{
		kv: NewRedisKV(client, "problem/"),
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
	data, err := f.kv.Get(ctx, key)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func (f *metadataServer) getAllManifests(ctx context.Context, prefix string) ([]problemManifest, error) {
	keys, err := f.kv.List(ctx, prefix)
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

func (f *metadataServer) getSingleRev(ctx context.Context, id string, revision int64) ([]problemManifest, error) {
	rev, err := f.getManifest(ctx, revKey(id, int(revision)))
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []problemManifest{rev}, nil
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

func (f *metadataServer) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var revs []problemManifest
	var err error

	id := r.FormValue("id")
	if id == "" {
		if revs, err = f.getAllManifests(r.Context(), ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(revs)
		return
	}

	rev := r.FormValue("revision")
	if rev == "" {
		revs, err = f.getAllManifests(r.Context(), id)
	} else {
		var revValue int64
		if revValue, err = strconv.ParseInt(rev, 10, 64); err == nil {
			revs, err = f.getSingleRev(r.Context(), id, revValue)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(revs) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(revs)
}
