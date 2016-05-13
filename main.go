package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	//	log "github.com/Sirupsen/logrus"
	"gopkg.in/redis.v3"
)

type filerServer struct {
	redisClient *redis.Client
	weedMaster  string
}

const chunksize = 256 * 1024

type assignResp struct {
	Count     int    `json:"count"`
	Fid       string `json:"fid"`
	Url       string `json:"url"`
	PublicUrl string `json:"publicUrl"`
}

func (f *filerServer) uploadChunk(buf []byte) (string, error) {
	resp, err := http.Get(f.weedMaster + "/dir/assign")
	if err != nil {
		return "", err
	}
	var ar assignResp
	err = json.NewDecoder(resp.Body).Decode(&ar)
	resp.Body.Close()
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "test.dat")
	if err != nil {
		return "", err
	}
	if _, err = part.Write(buf); err != nil {
		return "", err
	}
	writer.Close()
	resp, err = http.Post("http://"+ar.Url+"/"+ar.Fid, writer.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	return ar.Fid, nil
}

type lookupData struct {
	Locations []struct {
		PublicUrl string `json:"publicUrl"`
		Url       string `json:"url"`
	} `json:"locations"`
}

func lookupVolUrl(weedMaster, fid string) (string, error) {
	resp, err := http.Get(weedMaster + "/dir/lookup?volumeId=" + fid) // fixme
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var ld lookupData
	if err = json.NewDecoder(resp.Body).Decode(&ld); err != nil {
		return "", err
	}
	return ld.Locations[0].Url, nil
}

type redisChunk struct {
	Fid     string
	Sha1sum []byte
}

type redisFileInfo struct {
	Name       string
	Size       int64
	Digests    map[string]string
	ModuleType string `json:",omitempty"`
	Chunks     []redisChunk
}

type fileList struct {
	Entries []listFileEntry
}

type listFileEntry struct {
	Name string
}

func getAllKeys(client *redis.Client, pattern string) ([]string, error) {
	var cursor int64
	seen := make(map[string]struct{})
	var result []string
	for {
		var keys []string
		var err error
		cursor, keys, err = client.Scan(cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		for _, v := range keys {
			if _, ok := seen[v]; !ok {
				result = append(result, v)
				seen[v] = struct{}{}
			}
		}
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

func (f *filerServer) handleList(w http.ResponseWriter, r *http.Request, path string) error {
	keys, err := getAllKeys(f.redisClient, redisKey(path)+"*")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(keys))
	for _, v := range keys {
		if pf, _ := redisKeyToPath(v); pf != "" {
			names = append(names, pf)
		}
	}
	sort.Strings(names)
	w.Write([]byte(strings.Join(names, "\n")))
	/*var wr fileList
	for _, v := range names {
		wr.Entries = append(wr.Entries, listFileEntry{Name: v})
	}
	return json.NewEncoder(w).Encode(&wr)*/
	return nil
}

func addDigests(h http.Header, digests map[string]string) {
	var dkeys []string
	for k := range digests {
		dkeys = append(dkeys, k)
	}
	sort.Strings(dkeys)
	var dvals []string
	for _, k := range dkeys {
		dvals = append(dvals, k+"="+digests[k])
	}
	h.Add("Digest", strings.Join(dvals, ","))
	if md5, ok := digests["MD5"]; ok && md5 != "" {
		h.Add("Content-MD5", md5)
	}
}

func parseDigests(dh string) map[string]string {
	result := make(map[string]string)
	for _, v := range strings.Split(dh, ",") {
		ds := strings.SplitN(strings.TrimSpace(v), "=", 2)
		if len(ds) != 2 {
			continue
		}
		result[strings.ToUpper(ds[0])] = ds[1]
	}
	return result
}

func (f *filerServer) handleDownload(w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" || path[len(path)-1] == '/' {
		return f.handleList(w, r, path)
	}
	fi, err := f.getFileInfo(path)
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return nil
		}
		return err
	}
	addDigests(w.Header(), fi.Digests)
	w.Header().Add("Content-Length", strconv.FormatInt(fi.Size, 10))
	w.Header().Add("X-Fs-Content-Length", strconv.FormatInt(fi.Size, 10))
	if fi.ModuleType != "" {
		w.Header().Add("X-Fs-Module-Type", fi.ModuleType)
	}
	if r.Method == "HEAD" {
		return nil
	}

	for _, ch := range fi.Chunks {
		volurl, err := lookupVolUrl(f.weedMaster, ch.Fid)
		if err != nil {
			return err
		}
		resp, err := http.Get("http://" + volurl + "/" + ch.Fid)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func urlToPath(urlpath string) (string, error) {
	if !strings.HasPrefix(urlpath, "/fs/") {
		return "", fmt.Errorf("url must start with /fs/")
	}
	return strings.TrimPrefix(urlpath, "/fs/"), nil
}

func redisKey(path string) string {
	return "fs/" + path
}

func redisKeyToPath(key string) (string, error) {
	if !strings.HasPrefix(key, "fs/") {
		return "", fmt.Errorf("key must start with fs/")
	}
	return strings.TrimPrefix(key, "fs/"), nil
}

func (f *filerServer) getFileInfo(path string) (*redisFileInfo, error) {
	res, err := f.redisClient.Get(redisKey(path)).Result()
	if err != nil {
		return nil, err
	}
	var fi redisFileInfo
	if err = json.Unmarshal([]byte(res), &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *filerServer) deleteFile(fi *redisFileInfo) error {
	redisErr := f.redisClient.Del(redisKey(fi.Name)).Err()
	var wg sync.WaitGroup
	wg.Add(len(fi.Chunks))
	for _, ch := range fi.Chunks {
		go func(fid string) {
			defer wg.Done()
			f.deleteChunkFid(fid)
		}(ch.Fid)
	}
	wg.Wait()
	return redisErr
}

func (f *filerServer) handleDelete(w http.ResponseWriter, r *http.Request, path string) error {
	fi, err := f.getFileInfo(path)
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return nil
		}
		return err
	}
	return f.deleteFile(fi)
}

func deleteChunkURL(url string) error {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (f *filerServer) deleteChunkFid(fid string) error {
	volurl, err := lookupVolUrl(f.weedMaster, fid)
	if err != nil {
		return err
	}
	return deleteChunkURL("http://" + volurl + "/" + fid)
}

func (f *filerServer) deleteChunks(chunks []redisChunk) {
	for _, v := range chunks {
		f.deleteChunkFid(v.Fid)
	}
}

type shortUploadStatus struct {
	Digests map[string]string
	Size    int64
}

func (f *filerServer) handleUpload(w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return fmt.Errorf("can't upload to empty path")
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	if fi, err := f.getFileInfo(path); err == nil && fi != nil {
		if err = f.deleteFile(fi); err != nil {
			return err
		}
	}

	fi := redisFileInfo{
		Name:       path,
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
	}
	hashes := map[string]hash.Hash{
		"MD5": md5.New(),
		"SHA": sha1.New(),
	}

	contentLength := int64(-1)

	if ch := r.Header.Get("X-Fs-Content-Length"); ch != "" {
		var err error
		contentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
	}

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(r.Body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				f.deleteChunks(fi.Chunks)
				return err
			}
			break
		}
		buf = buf[0:n]
		csum := sha1.Sum(buf)

		fid, err := f.uploadChunk(buf)
		if err != nil {
			f.deleteChunks(fi.Chunks)
			return err
		}
		for _, v := range hashes {
			v.Write(buf)
		}
		fi.Size += int64(n)
		fi.Chunks = append(fi.Chunks, redisChunk{
			Fid:     fid,
			Sha1sum: csum[:],
		})
	}
	if contentLength >= 0 && fi.Size != contentLength {
		f.deleteChunks(fi.Chunks)
		return nil
	}
	fi.Digests = make(map[string]string)
	recvDigests := parseDigests(r.Header.Get("Digest"))
	if ch := r.Header.Get("Content-MD5"); ch != "" {
		recvDigests["MD5"] = ch
	}
	for k, v := range hashes {
		fi.Digests[k] = base64.StdEncoding.EncodeToString(v.Sum(nil))
		if prev, ok := recvDigests[k]; ok && fi.Digests[k] != prev {
			f.deleteChunks(fi.Chunks)
			return nil
		}
	}
	jb, err := json.Marshal(&fi)
	if err != nil {
		f.deleteChunks(fi.Chunks)
		return nil
	}
	if err = f.redisClient.Set(redisKey(path), jb, 0).Err(); err != nil {
		f.deleteChunks(fi.Chunks)
		return nil
	}
	ss := shortUploadStatus{
		Digests: fi.Digests,
		Size:    fi.Size,
	}
	return json.NewEncoder(w).Encode(&ss)
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := urlToPath(r.URL.Path)
	if err == nil {
		switch r.Method {
		case "PUT", "POST":
			err = f.handleUpload(w, r, path)
		case "GET", "HEAD":
			err = f.handleDownload(w, r, path)
		case "DELETE":
			err = f.handleDelete(w, r, path)
		default:
			http.Error(w, "", http.StatusMethodNotAllowed)
		}
	}
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

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
	rev, err := f.getManifestByKey(id + "/" + strconv.FormatInt(revision, 10))
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

func (f *metadataServer) serveAllRevisions(w http.ResponseWriter, r *http.Request, id string) {
	revs, err := f.getAllRevs(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(revs) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sort.Sort(byIdRev(revs))
	if err = json.NewEncoder(w).Encode(revs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *metadataServer) serveProblemList(w http.ResponseWriter, r *http.Request) {
	revs, err := f.getAllManifestsPattern("problem/*")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Sort(byIdRev(revs))
	json.NewEncoder(w).Encode(revs)
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
		if revValue, err := strconv.ParseInt(rev, 10, 64); err == nil {
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
)

func main() {
	flag.Parse()
	f := filerServer{
		redisClient: redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "", // no password set
			DB:       0,  // use default DB
		}),
		weedMaster: "http://localhost:9333",
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
