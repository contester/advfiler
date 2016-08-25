package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	//	log "github.com/Sirupsen/logrus"
	"gopkg.in/redis.v4"
)

type metadataServer struct {
	redisClient *redis.Client
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
	if err = f.redisClient.Set(problemRedisKeyRev(mf.Id, mf.Revision), mb, 0).Err(); err != nil {
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

func problemRedisKey(suffix string) string {
	return "problem/" + suffix
}

func problemRedisKeyRev(id string, rev int) string {
	return problemRedisKey(id + "/" + strconv.FormatInt(int64(rev), 10))
}

func (f *metadataServer) getManifestByKey(key string) (problemManifest, error) {
	var result problemManifest
	data, err := f.redisClient.Get(key).Result()
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(data), &result)
	return result, err
}

func (f *metadataServer) getAllManifests(keys []string) ([]problemManifest, error) {
	result := make([]problemManifest, 0, len(keys))
	for _, v := range keys {
		if m, err := f.getManifestByKey(v); err == nil {
			result = append(result, m)
		}
	}
	sort.Sort(byIdRev(result))
	return result, nil
}

func (f *metadataServer) getAllManifestsPattern(pattern string) ([]problemManifest, error) {
	keys, err := getAllKeys(f.redisClient, pattern)
	if err != nil {
		return nil, err
	}
	return f.getAllManifests(keys)
}

func (f *metadataServer) getAllRevs(id string) ([]problemManifest, error) {
	return f.getAllManifestsPattern(problemRedisKey(id) + "/*")
}

func (f *metadataServer) getSingleRev(id string, revision int64) ([]problemManifest, error) {
	rev, err := f.getManifestByKey(problemRedisKey(id) + "/" + strconv.FormatInt(revision, 10))
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
	if r.Method != "GET" {
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
		if revs, err = f.getAllManifestsPattern("problem/*"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(revs)
		return
	}

	rev := r.FormValue("revision")
	if rev == "" {
		revs, err = f.getAllRevs(id)
	} else {
		var revValue int64
		if revValue, err = strconv.ParseInt(rev, 10, 64); err == nil {
			revs, err = f.getSingleRev(id, revValue)
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

var (
	listen = flag.String("listen", ":9094", "")

	redisBackend  = flag.String("redis", "localhost:6379", "")
	redisPassword = flag.String("redis_password", "", "")
	redisDb       = flag.Int("redis_db", 0, "")
)

func main() {
	flag.Parse()
	f := filerServer{
		redisClient: redis.NewClient(&redis.Options{
			Addr:     *redisBackend,
			Password: *redisPassword,
			DB:       *redisDb,
		}),
		weed: &WeedClient{master: "http://localhost:9333"},
	}
	ms := metadataServer{
		redisClient: f.redisClient,
	}
	fmt.Println(f.redisClient.Ping().Result())
	http.Handle("/fs/", &f)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.ListenAndServe(*listen, nil)
}
