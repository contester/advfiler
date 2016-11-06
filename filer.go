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
)

type filerKV interface {
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Del(ctx context.Context, key string) error
	Set(ctx context.Context, key string, value []byte) error
}

type filerServer struct {
	kv                     filerKV
	weed                   *WeedClient
	urlPrefix, redisPrefix string
}

func NewFiler(kv filerKV, weed *WeedClient) *filerServer {
	return &filerServer{
		kv:        kv,
		weed:      weed,
		urlPrefix: "/fs/",
	}
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

func (f *filerServer) handleList(w http.ResponseWriter, r *http.Request, path string) error {
	names, err := f.kv.List(r.Context(), path)
	if err != nil {
		return err
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
	dkeys := make([]string, 0, len(digests))
	for k := range digests {
		dkeys = append(dkeys, k)
	}
	sort.Strings(dkeys)
	dvals := make([]string, 0, len(dkeys))
	for _, k := range dkeys {
		dvals = append(dvals, k+"="+digests[k])
	}
	h.Add("Digest", strings.Join(dvals, ","))
	if md5, ok := digests["MD5"]; ok && md5 != "" {
		h.Add("Content-MD5", md5)
	}
}

func parseDigests(dh string) map[string]string {
	splits := strings.Split(dh, ",")
	result := make(map[string]string, len(splits))
	for _, v := range splits {
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
	fi, err := f.getFileInfo(r.Context(), path)
	if err != nil {
		if err == NotFound {
			http.NotFound(w, r)
			return nil
		}
		return err
	}
	w.Header().Add("X-Fs-Content-Length", strconv.FormatInt(fi.Size, 10))
	if fi.ModuleType != "" {
		w.Header().Add("X-Fs-Module-Type", fi.ModuleType)
	}
	if r.Method == http.MethodHead {
		return nil
	}
	limitValue := fi.Size
	if limitStr := r.Header.Get("X-Fs-Limit"); limitStr != "" {
		lv, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil {
			return err
		}
		limitValue = lv
	}
	if limitValue > fi.Size {
		limitValue = fi.Size
	}
	if limitValue < fi.Size {
		w.Header().Add("X-Fs-Truncated", "true")
	} else {
		addDigests(w.Header(), fi.Digests)
	}

	w.Header().Add("Content-Length", strconv.FormatInt(limitValue, 10))

	if limitValue == 0 {
		return nil
	}

	for _, ch := range fi.Chunks {
		resp, err := f.weed.Get(r.Context(), ch.Fid)
		if err != nil {
			return err
		}
		lr := io.LimitReader(resp.Body, limitValue)
		n, err := io.Copy(w, lr)
		limitValue -= n
		resp.Body.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func trimOr(s, prefix, what string) (string, error) {
	if r := strings.TrimPrefix(s, prefix); r != s {
		return r, nil
	}
	return "", fmt.Errorf("%s must start with %s, got %s", what, s, prefix)
}

func (f *filerServer) urlToPath(urlpath string) (string, error) {
	return trimOr(urlpath, f.urlPrefix, "filer url")
}

func (f *filerServer) getFileInfo(ctx context.Context, path string) (*redisFileInfo, error) {
	var fi redisFileInfo
	if err := KVGetJson(ctx, f.kv, path, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *filerServer) deleteFile(ctx context.Context, fi *redisFileInfo) error {
	err := f.kv.Del(ctx, fi.Name)
	f.deleteChunks(ctx, fi.Chunks)
	return err
}

func (f *filerServer) handleDelete(w http.ResponseWriter, r *http.Request, path string) error {
	fi, err := f.getFileInfo(r.Context(), path)
	if err != nil {
		if err == NotFound {
			http.NotFound(w, r)
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
	if fi, err := f.getFileInfo(r.Context(), path); err == nil && fi != nil {
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
			Size:    int64(n),
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
	if err = f.kv.Set(r.Context(), path, jb); err != nil {
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
	path, err := f.urlToPath(r.URL.Path)
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
