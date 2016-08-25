package main

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/redis.v4"
)

type filerServer struct {
	redisClient *redis.Client
	weed        *WeedClient
}

const chunksize = 256 * 1024

type redisChunk struct {
	Fid     string
	Sha1sum []byte
	Size    int64 `json:",omitempty"`
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
	var cursor uint64
	seen := make(map[string]struct{})
	var result []string
	for {
		var keys []string
		var err error
		keys, cursor, err = client.Scan(cursor, pattern, 100).Result()
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
		resp, err := f.weed.Get(r.Context(), ch.Fid)
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

func (f *filerServer) deleteFile(ctx context.Context, fi *redisFileInfo) error {
	redisErr := f.redisClient.Del(redisKey(fi.Name)).Err()
	f.deleteChunks(ctx, fi.Chunks)
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
	return f.deleteFile(r.Context(), fi)
}

func (f *filerServer) deleteChunks(ctx context.Context, chunks []redisChunk) error {
	var wg sync.WaitGroup
	wg.Add(len(chunks))
	errs := make([]error, len(chunks))
	for i, v := range chunks {
		go func(i int, v redisChunk) {
			defer wg.Done()
			errs[i] = f.weed.Delete(ctx, v.Fid)
		}(i, v)
	}
	wg.Wait()
	for _, v := range errs {
		if v != nil {
			return v
		}
	}
	return nil
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
		if err = f.deleteFile(r.Context(), fi); err != nil {
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
				f.deleteChunks(r.Context(), fi.Chunks)
				return err
			}
			break
		}
		buf = buf[0:n]
		csum := sha1.Sum(buf)

		fid, err := f.weed.Upload(r.Context(), buf)
		if err != nil {
			f.deleteChunks(r.Context(), fi.Chunks)
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
		f.deleteChunks(r.Context(), fi.Chunks)
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
			f.deleteChunks(r.Context(), fi.Chunks)
			return nil
		}
	}
	jb, err := json.Marshal(&fi)
	if err != nil {
		f.deleteChunks(r.Context(), fi.Chunks)
		return nil
	}
	if err = f.redisClient.Set(redisKey(path), jb, 0).Err(); err != nil {
		f.deleteChunks(r.Context(), fi.Chunks)
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
