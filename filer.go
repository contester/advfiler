package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/contester/advfiler/common"
	"google.golang.org/protobuf/proto"

	pb "github.com/contester/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

var (
	_ = log.Info
)

type AuthCheck interface {
	Check(ctx context.Context, token string, action pb.AuthAction, path string) (bool, error)
}

type filerServer struct {
	backend     common.Backend
	urlPrefix   string
	authChecker AuthCheck
}

func NewFiler(backend common.Backend, authCheck AuthCheck) *filerServer {
	return &filerServer{
		backend:     backend,
		urlPrefix:   "/fs/",
		authChecker: authCheck,
	}
}

func tokenFromHeader(req *http.Request) string {
	if ah := req.Header.Get("Authorization"); len(ah) > 7 && strings.EqualFold(ah[0:7], "BEARER ") {
		return ah[7:]
	}
	return ""
}

var errUnauthorized = errors.New("unauthorized")

func (f *filerServer) handleList(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) error {
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		return errUnauthorized
	}
	names, err := f.backend.List(ctx, path)
	if err != nil {
		return err
	}
	sort.Strings(names)
	var buf bytes.Buffer

	for _, v := range names {
		buf.WriteString(v)
		buf.WriteByte('\n')
	}

	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(buf.Bytes()))

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

	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		return errUnauthorized
	}

	limitValue := int64(-1)
	if limitStr := r.Header.Get("X-Fs-Limit"); limitStr != "" {
		lv, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil {
			return err
		}
		limitValue = lv
	}

	result, err := f.backend.Download(ctx, path, common.DownloadOptions{})
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
	if lm := result.LastModifiedTimestamp(); lm != 0 {
		t := time.Unix(lm, 0)
		w.Header().Set("Last-Modified", t.UTC().Format(http.TimeFormat))
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

	var xr io.Reader = result.Body()
	if limitValue != -1 {
		xr = io.LimitReader(xr, limitValue)
	}

	_, err = io.Copy(w, xr)
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
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_WRITE, path); !v {
		return errUnauthorized
	}

	fi := common.FileInfo{
		ModuleType: r.Header.Get("X-Fs-Module-Type"),
		Name:       path,
	}
	// First, look into content-length.
	if ch := r.Header.Get("Content-Length"); ch != "" {
		var err error
		fi.ContentLength, err = strconv.ParseInt(ch, 10, 64)
		if err != nil {
			return err
		}
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
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		return errUnauthorized
	}
	decoder := json.NewDecoder(r.Body)
	var mdreq multiDownloadRequest
	if err := decoder.Decode(&mdreq); err != nil {
		return err
	}
	cout := zip.NewWriter(w)
	defer cout.Close()
	wr := writeToZip(cout)
	for _, entry := range mdreq.Entry {
		f.writeRemoteFileAs(ctx, wr, entry.Source, entry.Destination)
	}
	return nil
}

func writeToZip(w *zip.Writer) func(result common.DownloadResult, as string) error {
	return func(result common.DownloadResult, as string) error {
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
		_, err = io.Copy(wr, result.Body())
		return err
	}
}

func writeToTar(w *tar.Writer) func(result common.DownloadResult, as string) error {
	return func(result common.DownloadResult, as string) error {
		fh := tar.Header{
			Name:     as,
			Mode:     0666,
			Size:     result.Size(),
			Typeflag: tar.TypeReg,
		}
		if tm := result.LastModifiedTimestamp(); tm != 0 {
			fh.ModTime = time.Unix(tm, 0)
		}
		if mn := result.ModuleType(); mn != "" {
			fh.Xattrs = map[string]string{"user.fs_module_type": mn}
		}
		if err := w.WriteHeader(&fh); err != nil {
			return err
		}
		_, err := io.Copy(w, result.Body())
		return err
	}
}

func (f *filerServer) writeRemoteFileAs(ctx context.Context, wr func(result common.DownloadResult, as string) error, name, as string) error {
	result, err := f.backend.Download(ctx, name, common.DownloadOptions{})
	if err != nil {
		return err
	}
	return wr(result, as)
}

func (f *filerServer) writeProblemData(ctx context.Context, w *zip.Writer, problemID string) error {
	prefix := "problem/" + problemID + "/"
	names, _ := f.backend.List(ctx, prefix)
	wr := writeToZip(w)
	for _, name := range names {
		pname := strings.TrimPrefix(name, prefix)
		if pname == "checker" {
			f.writeRemoteFileAs(ctx, wr, name, "checker")
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
		f.writeRemoteFileAs(ctx, wr, name, dname)
	}

	return nil
}

func (f *filerServer) HandlePackage(w http.ResponseWriter, r *http.Request) {
	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cout := zip.NewWriter(w)
	defer cout.Close()

	contestID := r.FormValue("contest")
	submitID := r.FormValue("submit")
	testingID := r.FormValue("testing")
	wz := writeToZip(cout)

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
			f.writeRemoteFileAs(r.Context(), wz, name, splits[len(splits)-2]+".o")
		}
		f.writeRemoteFileAs(r.Context(), wz, "submit/"+contestID+"/"+submitID+"/compiledModule", "solution")
		f.writeRemoteFileAs(r.Context(), wz, "submit/"+contestID+"/"+submitID+"/sourceModule", "solution")
	}

	if problemID := r.FormValue("problem"); problemID != "" {
		f.writeProblemData(r.Context(), cout, problemID)
	}
}

func (f *filerServer) downloadAsset(ctx context.Context, name, as string, limit int64) (*pb.Asset, error) {
	fr, err := f.backend.Download(ctx, name, common.DownloadOptions{})
	if err != nil {
		log.Infof("err: %v", err)
		return nil, err
	}
	defer fr.Body().Close()
	xr := pb.Asset{
		Name:         as,
		OriginalSize: fr.Size(),
		Truncated:    fr.Size() > limit,
	}
	bb := make([]byte, int(limit))
	n, err := fr.Body().Read(bb)
	if err != nil {
		return nil, err
	}
	bb = bb[:n]
	xr.Data = append([]byte(nil), bb...)
	return &xr, nil
}

func (f *filerServer) handleProtoPackage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v, _ := f.authChecker.Check(ctx, tokenFromHeader(r), pb.AuthAction_A_READ, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	contestID := r.FormValue("contest")
	submitID := r.FormValue("submit")
	testingID := r.FormValue("testing")
	problemID := r.FormValue("problem")
	sizeLimit := int64(1024)
	solutionSizeLimit := int64(128000)

	if !strings.HasPrefix(problemID, "problem/") {
		http.Error(w, "invalid problem ID: "+problemID, http.StatusNotFound)
		return
	}

	if sz := r.FormValue("sizeLimit"); sz != "" {
		if isz, err := strconv.ParseInt(sz, 10, 64); err == nil {
			sizeLimit = isz
		}
	}

	var result pb.TestingRecord
	var err error
	result.Solution, err = f.downloadAsset(ctx, "submit/"+contestID+"/"+submitID+"/sourceModule", "source", solutionSizeLimit)
	if err != nil {
		http.Error(w, "submit/"+contestID+"/"+submitID+"/sourceModule: "+err.Error(), http.StatusNotFound)
		return
	}

	prefix := problemID + "/"

	names, err := f.backend.List(ctx, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	testSet := make(map[int64]struct{})

	for _, name := range names {
		pname := strings.TrimPrefix(name, prefix)
		if pname == "checker" {
			continue
		}
		splits := strings.Split(pname, "/")
		if len(splits) != 3 || splits[0] != "tests" {
			continue
		}
		testID, err := strconv.ParseInt(splits[1], 10, 64)
		if err != nil {
			continue
		}
		testSet[testID] = struct{}{}
	}
	testList := make([]int64, 0, len(testSet))
	for i := range testSet {
		testList = append(testList, i)
	}
	sort.Slice(testList, func(i, j int) bool { return testList[i] < testList[j] })
	log.Infof("test list: %v", testList)

	for _, testID := range testList {
		outName := "submit/" + contestID + "/" + submitID + "/" + testingID + "/" + strconv.FormatInt(testID, 10) + "/output"
		out, err := f.downloadAsset(ctx, outName, "output", sizeLimit)
		if err != nil {
			continue
		}
		testRecord := pb.TestRecord{
			TestId: testID,
			Output: out,
		}
		testPrefix := prefix + "tests/" + strconv.FormatInt(testID, 10) + "/"
		if testRecord.Input, err = f.downloadAsset(ctx, testPrefix+"input.txt", "input", sizeLimit); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		testRecord.Answer, _ = f.downloadAsset(ctx, testPrefix+"answer.txt", "answer", sizeLimit)
		result.Test = append(result.Test, &testRecord)
	}

	b, err := proto.Marshal(&result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Add("Content-Length", strconv.FormatInt(int64(len(b)), 10))
	w.Write(b)
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := f.urlToPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
	if err != nil {
		log.Errorf("%q: %v", path, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *filerServer) handleTarDownload(w http.ResponseWriter, r *http.Request) {
	path := r.FormValue("path")
	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_READ, path); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	names, err := f.backend.List(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sort.Strings(names)

	fw := tar.NewWriter(w)
	defer fw.Close()
	wfw := writeToTar(fw)

	for _, v := range names {
		f.writeRemoteFileAs(r.Context(), wfw, v, v)
	}
}

func (f *filerServer) handleTarUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.handleTarDownload(w, r)
		return
	}
	if r.Method != http.MethodPut {
		return
	}

	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_WRITE, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var realSize, savedSize int64
	var icnt int

	fr := tar.NewReader(r.Body)
	for {
		h, err := fr.Next()
		if err == io.EOF {
			break
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Name == "" || strings.HasSuffix(h.Name, "/") {
			continue
		}
		fi := common.FileInfo{
			ModuleType:    h.Xattrs["user.fs_module_type"],
			Name:          h.Name,
			ContentLength: h.Size,
		}
		if !h.ModTime.IsZero() {
			fi.TimestampUnix = h.ModTime.Unix()
		}
		res, err := f.backend.Upload(r.Context(), fi, fr)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		icnt++
		if res.Hardlinked {
			savedSize += res.Size
		} else {
			realSize += res.Size
		}
	}
	fmt.Fprintf(w, "Files: %d, real size: %d, saved size: %d\n", icnt, realSize, savedSize)
}

func (f *filerServer) handleWipe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		return
	}

	if v, _ := f.authChecker.Check(r.Context(), tokenFromHeader(r), pb.AuthAction_A_WRITE, ""); !v {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	files, err := f.backend.List(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, v := range files {
		if v == "" {
			continue
		}
		if err = f.backend.Delete(r.Context(), v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	for _, v := range files {
		fmt.Fprintln(w, v)
	}
}
