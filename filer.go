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

	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
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

func (f *filerServer) handleList(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	names, err := f.kv.List(ctx, path)
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

func maybeSetDigest(m map[string]string, name string, value []byte) {
	if len(value) > 0 {
		m[name] = base64.StdEncoding.EncodeToString(value)
	}
}

func digestsToMap(d *pb.FileInfo_Digests) map[string]string {
	if d == nil {
		return nil
	}
	r := make(map[string]string)
	maybeSetDigest(r, "MD5", d.Md5)
	maybeSetDigest(r, "SHA", d.Sha1)
	if len(r) == 0 {
		return nil
	}
	return r
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

func (f *filerServer) handleDownload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" || path[len(path)-1] == '/' {
		return f.handleList(ctx, w, r, path)
	}
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		if err == NotFound {
			http.NotFound(w, r)
			return nil
		}
		return err
	}
	w.Header().Add("X-Fs-Content-Length", strconv.FormatInt(fi.Size_, 10))
	if fi.ModuleType != "" {
		w.Header().Add("X-Fs-Module-Type", fi.ModuleType)
	}
	if r.Method == http.MethodHead {
		addDigests(w.Header(), digestsToMap(fi.Digests))
		return nil
	}
	limitValue := fi.Size_
	if limitStr := r.Header.Get("X-Fs-Limit"); limitStr != "" {
		lv, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil {
			return err
		}
		limitValue = lv
	}
	if limitValue > fi.Size_ {
		limitValue = fi.Size_
	}
	if limitValue < fi.Size_ {
		w.Header().Add("X-Fs-Truncated", "true")
	} else {
		addDigests(w.Header(), digestsToMap(fi.Digests))
	}

	w.Header().Add("Content-Length", strconv.FormatInt(limitValue, 10))

	if limitValue == 0 {
		return nil
	}

	for _, ch := range fi.Chunks {
		resp, err := f.weed.Get(ctx, ch.Fid)
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

func (f *filerServer) getFileInfo(ctx context.Context, path string) (*pb.FileInfo, error) {
	var fi pb.FileInfo
	if err := KVGetProto(ctx, f.kv, path, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *filerServer) deleteFile(ctx context.Context, name string, fi *pb.FileInfo) error {
	err := f.kv.Del(ctx, name)
	f.deleteChunks(ctx, fi.Chunks)
	return err
}

func (f *filerServer) handleDelete(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		if err == NotFound {
			http.NotFound(w, r)
			return nil
		}
		return err
	}
	return f.deleteFile(ctx, path, fi)
}

func (f *filerServer) deleteChunks(ctx context.Context, chunks []*pb.FileChunk) error {
	var wg sync.WaitGroup
	wg.Add(len(chunks))
	errs := make([]error, len(chunks))
	for i, v := range chunks {
		go func(i int, v *pb.FileChunk) {
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

type allHashes struct {
	sha1, md5 hash.Hash
}

func (s *allHashes) Write(p []byte) (n int, err error) {
	if n, err = s.md5.Write(p); err != nil {
		return n, err
	}
	return s.sha1.Write(p)
}

func (s *allHashes) toDigests() *pb.FileInfo_Digests {
	return &pb.FileInfo_Digests{
		Md5: s.md5.Sum(nil),
		Sha1: s.sha1.Sum(nil),
	}
}

func newHashes() *allHashes {
	return &allHashes{
		sha1: sha1.New(),
		md5: md5.New(),
	}
}

func (f *filerServer) handleUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return fmt.Errorf("can't upload to empty path")
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	if fi, err := f.getFileInfo(ctx, path); err == nil && fi != nil {
		if err = f.deleteFile(ctx, path, fi); err != nil {
			return err
		}
	}

	fi := pb.FileInfo{
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
	}
	hashes := newHashes()

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
				f.deleteChunks(ctx, fi.Chunks)
				return err
			}
			break
		}
		buf = buf[0:n]
		csum := sha1.Sum(buf)

		fid, err := f.weed.Upload(ctx, buf)
		if err != nil {
			f.deleteChunks(ctx, fi.Chunks)
			return err
		}
		hashes.Write(buf)
		fi.Size_ += int64(n)
		fi.Chunks = append(fi.Chunks, &pb.FileChunk{
			Fid:     fid,
			Sha1Sum: csum[:],
			Size_:    int64(n),
		})
	}
	if contentLength >= 0 && fi.Size_ != contentLength {
		f.deleteChunks(ctx, fi.Chunks)
		return nil
	}
	fi.Digests = hashes.toDigests()
	recvDigests := parseDigests(r.Header.Get("Digest"))
	if ch := r.Header.Get("Content-MD5"); ch != "" {
		recvDigests["MD5"] = ch
	}
	stDigests := digestsToMap(fi.Digests)
	for k, v := range stDigests {
		if prev, ok := recvDigests[k]; ok && v != prev {
			f.deleteChunks(ctx, fi.Chunks)
			return fmt.Errorf("checksum mismatch")
		}
	}
	prb, err := proto.Marshal(&fi)
	if err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return err
	}
	if err = f.kv.Set(ctx, path, prb); err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return err
	}
	ss := shortUploadStatus{
		Digests: stDigests,
		Size:    fi.Size_,
	}
	return json.NewEncoder(w).Encode(&ss)
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := f.urlToPath(r.URL.Path)
	if err == nil {
		ctx := r.Context()
		switch r.Method {
		case http.MethodPut, http.MethodPost:
			err = f.handleUpload(ctx, w, r, path)
		case http.MethodGet, http.MethodHead:
			err = f.handleDownload(ctx, w, r, path)
		case http.MethodDelete:
			err = f.handleDelete(ctx, w, r, path)
		default:
			http.Error(w, "", http.StatusMethodNotAllowed)
		}
	}
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
