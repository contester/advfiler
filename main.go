package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

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

type redisFileInfo struct {
	Name   string
	Chunks []string
}

func (f *filerServer) handleDownload(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	st := f.redisClient.Get("fs/" + path)
	if st.Err() != nil {
		return st.Err()
	}
	ust, err := st.Bytes()
	if err != nil {
		return err
	}
	var fi redisFileInfo
	if err = json.Unmarshal(ust, &fi); err != nil {
		return err
	}

	for _, ch := range fi.Chunks {
		volurl, err := lookupVolUrl(f.weedMaster, ch)
		if err != nil {
			return err
		}
		resp, err := http.Get("http://" + volurl + "/" + ch)
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

func (f *filerServer) handleDelete(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	st := f.redisClient.Get("fs/" + path)
	if st.Err() != nil {
		return st.Err()
	}
	ust, err := st.Bytes()
	if err != nil {
		return err
	}
	var fi redisFileInfo
	if err = json.Unmarshal(ust, &fi); err != nil {
		return err
	}

	for _, ch := range fi.Chunks {
		volurl, err := lookupVolUrl(f.weedMaster, ch)
		if err != nil {
			return err
		}
		req, err := http.NewRequest("DELETE", "http://"+volurl+"/"+ch, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	return f.redisClient.Del("fs/" + path).Err()
}

func (f *filerServer) handleUpload(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	fmt.Printf("Upload %s: ", path)

	fi := redisFileInfo{
		Name: path,
	}

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(r.Body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				fmt.Printf("err: %s", err)
			}
			break
		}
		buf = buf[0:n]
		fid, err := f.uploadChunk(buf)
		if err != nil {
			return err
		}
		fi.Chunks = append(fi.Chunks, fid)
	}
	jb, err := json.Marshal(&fi)
	if err != nil {
		return err
	}
	if err = f.redisClient.Set("fs/"+fi.Name, jb, 0).Err(); err != nil {
		return err
	}
	return nil
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.Method {
	case "PUT", "POST":
		err = f.handleUpload(w, r)
	case "GET":
		err = f.handleDownload(w, r)
	case "DELETE":
		err = f.handleDelete(w, r)
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
	http.Handle("/", &f)
	http.ListenAndServe(*listen, nil)
}
