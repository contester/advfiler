package main

import (
	"archive/zip"
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

type filerServer struct {
	backend filer.Backend
	urlPrefix string
}

func NewFiler(backend Backend) *filerServer {
	return &filerServer{
		backend: backend,
		urlPrefix: "/fs/",
	}
}

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

func (f *filerServer) writeChunks(ctx context.Context, w io.Writer, chunks []*pb.FileChunk, limitValue int64) error {
	for _, ch := range chunks {
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
	return f.writeChunks(ctx, w, fi.Chunks, limitValue)
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
	return backend.Delete(ctx, path)
}

type shortUploadStatus struct {
	Digests map[string]string
	Size    int64
}


func (f *filerServer) handleUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return f.handleMultiDownload(ctx, w, r)
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	if fi, err := f.getFileInfo(ctx, path); err == nil && fi != nil {
		if err = f.deleteFile(ctx, path, fi); err != nil {
			return err
		}
	}

	fi := filer.FileInfo{
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
	}
	if ch := r.Header.Get("X-Fs-Content-Length"); ch != "" {
		var err error
		fi.ContentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
	}

	err := s.backend.Upload(ctx, fi, r.Body)
	if err != nil { return err }

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

type singleDownloadEntry struct {
	Source      string
	Destination string
}

type multiDownloadRequest struct {
	Entry []singleDownloadEntry
}

func (f *filerServer) handleMultiDownload(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	decoder := json.NewDecoder(r.Body)
	var mdreq multiDownloadRequest
	if err := decoder.Decode(&mdreq); err != nil {
		return err
	}
	cout := zip.NewWriter(w)
	defer cout.Close()
	for _, entry := range mdreq.Entry {
		f.writeRemoteFileAs(ctx, cout, entry.Source, entry.Destination)
	}
	return nil
}

func (f *filerServer) writeRemoteFileAs(ctx context.Context, w *zip.Writer, name, as string) error {
	fi, err := f.getFileInfo(ctx, name)
	if err != nil {
		return err
	}
	fh := zip.FileHeader{
		Name:               as,
		UncompressedSize64: uint64(fi.Size_),
		Method:             zip.Deflate,
	}
	if fi.ModuleType != "" {
		fh.Name += "." + fi.ModuleType
	}
	wr, err := w.CreateHeader(&fh)
	if err != nil {
		return err
	}
	return f.writeChunks(ctx, wr, fi.Chunks, fi.Size_)
}

func (f *filerServer) writeProblemData(ctx context.Context, w *zip.Writer, problemID string) error {
	prefix := "problem/" + problemID + "/"
	names, _ := f.kv.List(ctx, prefix)
	for _, name := range names {
		pname := strings.TrimPrefix(name, prefix)
		if pname == "checker" {
			f.writeRemoteFileAs(ctx, w, name, "checker")
			continue
		}
		splits := strings.Split(pname, "/")
		if len(splits) != 3 || splits[0] != "tests" {
			continue
		}
		var dname string
		switch splits[2] {
		case "input.txt":
			dname = splits[1]
		case "answer.txt":
			dname = splits[1] + ".a"
		}
		if dname == "" {
			continue
		}
		f.writeRemoteFileAs(ctx, w, name, dname)
	}

	return nil
}

func (f *filerServer) HandlePackage(w http.ResponseWriter, r *http.Request) {
	cout := zip.NewWriter(w)
	defer cout.Close()

	contestID := r.FormValue("contest")
	submitID := r.FormValue("submit")
	testingID := r.FormValue("testing")

	if contestID != "" && submitID != "" && testingID != "" {
		names, _ := f.kv.List(r.Context(), "submit/"+contestID+"/"+submitID+"/"+testingID+"/")
		for _, name := range names {
			splits := strings.Split(name, "/")
			if len(splits) < 5 {
				continue
			}
			if splits[len(splits)-1] != "output" {
				continue
			}
			f.writeRemoteFileAs(r.Context(), cout, name, splits[len(splits)-2]+".o")
		}
		f.writeRemoteFileAs(r.Context(), cout, "submit/"+contestID+"/"+submitID+"/compiledModule", "solution")
		f.writeRemoteFileAs(r.Context(), cout, "submit/"+contestID+"/"+submitID+"/sourceModule", "solution")
	}

	if problemID := r.FormValue("problem"); problemID != "" {
		f.writeProblemData(r.Context(), cout, problemID)
	}
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
