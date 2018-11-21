package filer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"sync"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
)

type KV interface {
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Del(ctx context.Context, key string) error
	Set(ctx context.Context, key string, value []byte) error
}

type WeedClient interface {
	Get(ctx context.Context, fileID string) (*http.Response, error)
	Upload(ctx context.Context, buf []byte) (string, error)
	Delete(ctx context.Context, fileID string) error
}

type boltServer struct {
	kv   KV
	weed WeedClient
}

func NewBolt(kv KV, weed WeedClient) Backend {
	return &boltServer{
		kv:   kv,
		weed: weed,
	}
}

const chunksize = 256 * 1024

func (f *boltServer) List(ctx context.Context, path string) ([]string, error) {
	return f.kv.List(ctx, path)
}

func (f *boltServer) writeChunks(ctx context.Context, w io.Writer, chunks []*pb.FileChunk, limitValue int64) error {
	for _, ch := range chunks {
		resp, err := f.weed.Get(ctx, ch.Fid)
		if err != nil {
			return err
		}
		var lr io.Reader
		if limitValue != -1 {
			lr = io.LimitReader(resp.Body, limitValue)
		} else {
			lr = resp.Body
		}
		n, err := io.Copy(w, lr)
		if limitValue != -1 {
			limitValue -= n
		}
		resp.Body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type DownloadResult interface {
	Size() int64
	ModuleType() string
	Digests() *pb.Digests
	WriteTo(ctx context.Context, w io.Writer, limit int64) error
}

type boltDownloadResult struct {
	size       int64
	moduleType string
	digests    *pb.Digests
	chunks     []*pb.FileChunk
	f          *boltServer
}

func (r boltDownloadResult) Size() int64          { return r.size }
func (r boltDownloadResult) ModuleType() string   { return r.moduleType }
func (r boltDownloadResult) Digests() *pb.Digests { return r.digests }
func (r boltDownloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, r.chunks, limit)
}

func (f *boltServer) Download(ctx context.Context, path string, limit int64) (DownloadResult, error) {
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		return nil, err
	}

	return boltDownloadResult{
		size:       fi.Size_,
		moduleType: fi.ModuleType,
		digests:    fi.Digests,
		chunks:     fi.Chunks,
	}, nil
}

func (f *boltServer) getFileInfo(ctx context.Context, path string) (*pb.FileInfo, error) {
	var fi pb.FileInfo
	if err := common.KVGetProto(ctx, f.kv, path, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *boltServer) deleteFile(ctx context.Context, name string, fi *pb.FileInfo) error {
	err := f.kv.Del(ctx, name)
	f.deleteChunks(ctx, fi.Chunks)
	return err
}

func (f *boltServer) Delete(ctx context.Context, path string) error {
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		return err
	}
	return f.deleteFile(ctx, path, fi)
}

func (f *boltServer) deleteChunks(ctx context.Context, chunks []*pb.FileChunk) error {
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

func (f *boltServer) Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error) {
	if fi, err := f.getFileInfo(ctx, info.Name); err == nil && fi != nil {
		if err = f.deleteFile(ctx, info.Name, fi); err != nil {
			return UploadStatus{}, err
		}
	}

	fi := pb.FileInfo{
		ModuleType: info.ModuleType,
	}
	hashes := newHashes()

	contentLength := info.ContentLength

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				f.deleteChunks(ctx, fi.Chunks)
				return UploadStatus{}, err
			}
			break
		}
		buf = buf[0:n]
		csum := sha1.Sum(buf)

		fid, err := f.weed.Upload(ctx, buf)
		if err != nil {
			f.deleteChunks(ctx, fi.Chunks)
			return UploadStatus{}, err
		}
		hashes.Write(buf)
		fi.Size_ += int64(n)
		fi.Chunks = append(fi.Chunks, &pb.FileChunk{
			Fid:     fid,
			Sha1Sum: csum[:],
			Size_:   int64(n),
		})
	}
	if contentLength >= 0 && fi.Size_ != contentLength {
		f.deleteChunks(ctx, fi.Chunks)
		return UploadStatus{}, nil
	}
	fi.Digests = hashes.toDigests()
	stDigests := common.DigestsToMap(fi.Digests)
	if !common.CheckDigests(info.RecvDigests, stDigests) {
		f.deleteChunks(ctx, fi.Chunks)
		return UploadStatus{}, fmt.Errorf("checksum mismatch")
	}
	prb, err := proto.Marshal(&fi)
	if err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return UploadStatus{}, err
	}
	if err = f.kv.Set(ctx, info.Name, prb); err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return UploadStatus{}, err
	}
	return UploadStatus{
		Digests: stDigests,
		Size:    fi.Size_,
	}, nil
}
