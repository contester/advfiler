package common

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"
	"io"

	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
)

var NotFound = errors.New("not found")

type GetKV interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

func KVGetJson(ctx context.Context, kv GetKV, key string, value interface{}) error {
	res, err := kv.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(res, value)
}

func KVGetProto(ctx context.Context, kv GetKV, key string, value proto.Message) error {
	res, err := kv.Get(ctx, key)
	if err != nil {
		return err
	}
	return proto.Unmarshal(res, value)
}

func maybeSetDigest(m map[string]string, name string, value []byte) {
	if len(value) > 0 {
		m[name] = base64.StdEncoding.EncodeToString(value)
	}
}

func DigestsToMap(d *pb.Digests) map[string]string {
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

func CheckDigests(recv, computed map[string]string) bool {
	for k, v := range computed {
		if prev, ok := recv[k]; ok && v != prev {
			return false
		}
	}
	return true
}

type DownloadResult interface {
	Size() int64
	ModuleType() string
	Digests() *pb.Digests
	WriteTo(ctx context.Context, w io.Writer, limit int64) error
	io.Reader
}

type UploadStatus struct {
	Digests map[string]string
	Size    int64
}

type Hashes struct {
	Sha1, Md5 hash.Hash
}

func (s *Hashes) Write(p []byte) (n int, err error) {
	if n, err = s.Md5.Write(p); err != nil {
		return n, err
	}
	return s.Sha1.Write(p)
}

func (s *Hashes) Digests() *pb.Digests {
	return &pb.Digests{
		Md5:  s.Md5.Sum(nil),
		Sha1: s.Sha1.Sum(nil),
	}
}

func NewHashes() *Hashes {
	return &Hashes{
		Sha1: sha1.New(),
		Md5:  md5.New(),
	}
}

type Backend interface {
	Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error)
	List(ctx context.Context, path string) ([]string, error)
	Download(ctx context.Context, path string) (DownloadResult, error)
	Delete(ctx context.Context, path string) error
}

type FileInfo struct {
	Name          string
	ContentLength int64
	ModuleType    string
	RecvDigests   map[string]string
	Compression   pb.CompressionType
}

type DB interface {
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Del(ctx context.Context, key string) error
	Set(ctx context.Context, key string, value []byte) error
}
