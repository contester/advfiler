package main

import (
	"gopkg.in/redis.v3"
	"net/http"
	"fmt"
	"flag"
	"io"
	"encoding/json"
)

type filerServer struct {
	redisClient *redis.Client
	weedMaster string
}

const chunksize = 256 * 1024

type assignResp struct {
	Count int `json:"count"`
	Fid string `json:"fid"`
	Url string `json:"url"`
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
	fmt.Printf("%+v", &ar)
	return ar.Fid, nil
}

func (f *filerServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	fmt.Printf("Upload %s", path)
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
			return
		}
		chunks = append(chunks, fid)
	}
}

func (f *filerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "PUT":
		f.handleUpload(w, r)
	}
}

var (
	listen = flag.String("listen", ":9094", "")
)


func main() {
	flag.Parse()
	f := filerServer{
		weedMaster: "http://turboo.sgu.ru:9333",
	}
	http.Handle("/", &f)
	http.ListenAndServe(*listen, nil)
}