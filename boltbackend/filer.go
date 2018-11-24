package boltbackend

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

type DB interface {
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

type Filer struct {
	kv   DB
	weed WeedClient
}

func NewFiler(kv DB, weed WeedClient) *Filer {
	return &Filer{
		kv:   kv,
		weed: weed,
	}
}

const chunksize = 256 * 1024

func (f *Filer) List(ctx context.Context, path string) ([]string, error) {
	return f.kv.List(ctx, path)
}

func (f *Filer) writeChunks(ctx context.Context, w io.Writer, chunks []*pb.FileChunk, limitValue int64) error {
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

type downloadResult struct {
	fi *pb.FileInfo
	f  *Filer
}

func (r downloadResult) Size() int64          { return r.fi.GetSize_() }
func (r downloadResult) ModuleType() string   { return r.fi.GetModuleType() }
func (r downloadResult) Digests() *pb.Digests { return r.fi.GetDigests() }
func (r downloadResult) WriteTo(ctx context.Context, w io.Writer, limit int64) error {
	return r.f.writeChunks(ctx, w, r.fi.GetChunks(), limit)
}

func (f *Filer) Download(ctx context.Context, path string) (common.DownloadResult, error) {
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		return nil, err
	}

	return downloadResult{
		fi: fi,
		f:  f,
	}, nil
}

func (f *Filer) getFileInfo(ctx context.Context, path string) (*pb.FileInfo, error) {
	var fi pb.FileInfo
	if err := common.KVGetProto(ctx, f.kv, path, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

func (f *Filer) deleteFile(ctx context.Context, name string, fi *pb.FileInfo) error {
	err := f.kv.Del(ctx, name)
	f.deleteChunks(ctx, fi.Chunks)
	return err
}

func (f *Filer) Delete(ctx context.Context, path string) error {
	fi, err := f.getFileInfo(ctx, path)
	if err != nil {
		return err
	}
	return f.deleteFile(ctx, path, fi)
}

func (f *Filer) deleteChunks(ctx context.Context, chunks []*pb.FileChunk) error {
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

func (f *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	if fi, err := f.getFileInfo(ctx, info.Name); err == nil && fi != nil {
		if err = f.deleteFile(ctx, info.Name, fi); err != nil {
			return common.UploadStatus{}, err
		}
	}

	fi := pb.FileInfo{
		ModuleType: info.ModuleType,
	}
	hashes := common.NewHashes()

	contentLength := info.ContentLength

	for {
		buf := make([]byte, chunksize)
		n, err := io.ReadFull(body, buf)
		if n == 0 {
			if err != nil && err != io.EOF {
				f.deleteChunks(ctx, fi.Chunks)
				return common.UploadStatus{}, err
			}
			break
		}
		buf = buf[0:n]
		csum := sha1.Sum(buf)

		fid, err := f.weed.Upload(ctx, buf)
		if err != nil {
			f.deleteChunks(ctx, fi.Chunks)
			return common.UploadStatus{}, err
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
		return common.UploadStatus{}, nil
	}
	fi.Digests = hashes.Digests()
	stDigests := common.DigestsToMap(fi.Digests)
	if !common.CheckDigests(info.RecvDigests, stDigests) {
		f.deleteChunks(ctx, fi.Chunks)
		return common.UploadStatus{}, fmt.Errorf("checksum mismatch")
	}
	prb, err := proto.Marshal(&fi)
	if err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return common.UploadStatus{}, err
	}
	if err = f.kv.Set(ctx, info.Name, prb); err != nil {
		f.deleteChunks(ctx, fi.Chunks)
		return common.UploadStatus{}, err
	}
	return common.UploadStatus{
		Digests: stDigests,
		Size:    fi.Size_,
	}, nil
}
