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

func (f *filerServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	fmt.Printf("Upload %s: ", path)

	var chunks []string
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
			fmt.Print(err)
			return
		}
		chunks = append(chunks, fid)
	}
	fmt.Printf("%+v\n", chunks)
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "PUT", "POST":
		f.handleUpload(w, r)
	}
}

var (
	listen = flag.String("listen", ":9094", "")
)

func main() {
	flag.Parse()
	f := filerServer{
		weedMaster: "http://localhost:9333",
	}
	http.Handle("/", &f)
	http.ListenAndServe(*listen, nil)
}
