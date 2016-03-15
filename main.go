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
	Name    string
	Size    int64
	Digests map[string]string
	Chunks  []redisChunk
}

type fileList struct {
	Entries []listFileEntry
}

type listFileEntry struct {
	Name string
}

func (f *filerServer) handleList(w http.ResponseWriter, r *http.Request, path string) error {
	prefix := redisKey(path)
	pattern := prefix + "*"
	var cursor int64
	seen := make(map[string]struct{})
	var names []string
	for {
		var keys []string
		var err error
		cursor, keys, err = f.redisClient.Scan(cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		for _, v := range keys {
			if pf, _ := redisKeyToPath(v); pf != "" {
				if _, ok := seen[pf]; !ok {
					seen[pf] = struct{}{}
					names = append(names, pf)
				}
			}
		}
		if cursor == 0 {
			break
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
		return err
	}
	addDigests(w.Header(), fi.Digests)
	w.Header().Add("Content-Length", strconv.FormatInt(fi.Size, 10))

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
	fmt.Printf("%s %d %d\n", path, len(res), strings.IndexByte(res, 0))
	var fi redisFileInfo
	if err = json.Unmarshal([]byte(res), &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *filerServer) handleDelete(w http.ResponseWriter, r *http.Request, path string) error {
	fi, err := f.getFileInfo(path)
	if err != nil {
		return err
	}

	for _, ch := range fi.Chunks {
		if err = f.deleteChunkFid(ch.Fid); err != nil {
			return err
		}
	}
	return f.redisClient.Del(redisKey(path)).Err()
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

func (f *filerServer) handleUpload(w http.ResponseWriter, r *http.Request, path string) error {
	if path == "" {
		return fmt.Errorf("can't upload to empty path")
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("can't upload to directory")
	}
	if fi, err := f.getFileInfo(path); err == nil && fi != nil {
		return fmt.Errorf("file already exists")
	}

	fi := redisFileInfo{
		Name: path,
	}
	hashes := map[string]hash.Hash{
		"MD5": md5.New(),
		"SHA": sha1.New(),
	}

	contentLength := int64(-1)

	if ch := r.Header.Get("Content-Length"); ch != "" {
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
		return fmt.Errorf("size mismatch")
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
			return fmt.Errorf("%s checksum mismatch", k)
		}
	}
	jb, err := json.Marshal(&fi)
	if err != nil {
		f.deleteChunks(fi.Chunks)
		return err
	}
	if err = f.redisClient.Set(redisKey(path), jb, 0).Err(); err != nil {
		f.deleteChunks(fi.Chunks)
		return err
	}
	w.Write(jb)
	return nil
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := urlToPath(r.URL.Path)
	if err == nil {
		switch r.Method {
		case "PUT", "POST":
			err = f.handleUpload(w, r, path)
		case "GET":
			err = f.handleDownload(w, r, path)
		case "DELETE":
			err = f.handleDelete(w, r, path)
		}
	}
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	fmt.Println(f.redisClient.Ping().Result())
	http.Handle("/fs/", &f)
	http.ListenAndServe(*listen, nil)
}
