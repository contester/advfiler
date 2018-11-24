package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/golang/snappy"
	log "github.com/sirupsen/logrus"
)

type filerServer struct {
	backend   common.Backend
	urlPrefix string
}

func NewFiler(backend common.Backend) *filerServer {
	return &filerServer{
		backend:   backend,
		urlPrefix: "/fs/",
	}
}

func (f *filerServer) handleList(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	names, err := f.backend.List(ctx, path)
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

func (f *filerServer) handleDownload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" || path[len(path)-1] == '/' {
		return f.handleList(ctx, w, r, path)
	}

	limitValue := int64(-1)
	if limitStr := r.Header.Get("X-Fs-Limit"); limitStr != "" {
		lv, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil {
			return err
		}
		limitValue = lv
	}

	result, err := f.backend.Download(ctx, path)
	if err != nil {
		if err == common.NotFound {
			http.NotFound(w, r)
			return nil
		}
		return err
	}

	rsize := result.Size()
	w.Header().Add("X-Fs-Content-Length", strconv.FormatInt(rsize, 10))
	if m := result.ModuleType(); m != "" {
		w.Header().Add("X-Fs-Module-Type", m)
	}
	if r.Method == http.MethodHead {
		addDigests(w.Header(), common.DigestsToMap(result.Digests()))
		return nil
	}
	if limitValue != -1 && limitValue < rsize {
		w.Header().Add("X-Fs-Truncated", "true")
		w.Header().Add("Content-Length", strconv.FormatInt(limitValue, 10))
	} else {
		addDigests(w.Header(), common.DigestsToMap(result.Digests()))
		w.Header().Add("Content-Length", strconv.FormatInt(rsize, 10))
	}

	if limitValue == 0 {
		return nil
	}

	cr := snappy.NewReader(result)
	var xr io.Reader
	if limitValue == -1 {
		xr = cr
	} else {
		xr = io.LimitReader(cr, limitValue)
	}

	n, err := io.Copy(w, xr)
	log.Infof("f: %q rsize: %d n: %d", path, rsize, n)

	return err
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

func (f *filerServer) handleDelete(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	return f.backend.Delete(ctx, path)
}

func (f *filerServer) handleUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return f.handleMultiDownload(ctx, w, r)
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	fi := common.FileInfo{
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
		Name:       path,
	}
	if ch := r.Header.Get("X-Fs-Content-Length"); ch != "" {
		var err error
		fi.ContentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
	}

	fi.RecvDigests = parseDigests(r.Header.Get("Digest"))
	if ch := r.Header.Get("Content-MD5"); ch != "" {
		fi.RecvDigests["MD5"] = ch
	}

	result, err := f.backend.Upload(ctx, fi, r.Body)
	if err != nil {
		return err
	}

	return json.NewEncoder(w).Encode(&result)
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
	result, err := f.backend.Download(ctx, name)
	if err != nil {
		return err
	}
	fh := zip.FileHeader{
		Name:               as,
		UncompressedSize64: uint64(result.Size()),
		Method:             zip.Deflate,
	}
	if m := result.ModuleType(); m != "" {
		fh.Name += "." + m
	}
	wr, err := w.CreateHeader(&fh)
	if err != nil {
		return err
	}
	return result.WriteTo(ctx, wr, -1)
}

func (f *filerServer) writeProblemData(ctx context.Context, w *zip.Writer, problemID string) error {
	prefix := "problem/" + problemID + "/"
	names, _ := f.backend.List(ctx, prefix)
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
		names, _ := f.backend.List(r.Context(), "submit/"+contestID+"/"+submitID+"/"+testingID+"/")
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
