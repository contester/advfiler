package common

import (
	"context"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"sort"
	"sync"

	"google.golang.org/protobuf/proto"
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
	Digests() Digests
	LastModifiedTimestamp() int64
	Body() io.ReadSeekCloser
}

type UploadStatus struct {
	Digests    map[string]string
	Size       int64
	Hardlinked bool
}

type Hashes struct {
	Sha256, Sha1, Md5 hash.Hash
}

func (s *Hashes) Write(p []byte) (n int, err error) {
	if n, err = s.Md5.Write(p); err != nil {
		return n, err
	}
	s.Sha256.Write(p)
	return s.Sha1.Write(p)
}

type Backend interface {
	Upload(ctx context.Context, info FileInfo, body io.Reader) (UploadStatus, error)
	List(ctx context.Context, path string) ([]string, error)
	Download(ctx context.Context, path string, options DownloadOptions) (DownloadResult, error)
	Delete(ctx context.Context, path string) error
	Close()
}

type MultiBackend struct {
	mu     sync.RWMutex
	mounts map[string]Backend
}

func (s *MultiBackend) Mount(path string, backend Backend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mounts == nil {
		s.mounts = make(map[string]Backend)
	}
	s.mounts[path] = backend
}

func (s *MultiBackend) List(ctx context.Context, path string) ([]string, error) {
	s.mu.RLock()

	var found Backend
	for k, v := range s.mounts {
		if k == path {
			found = v
			break
		}
	}
	if found != nil {
		s.mu.RUnlock()
		return found.List(ctx, path)
	}
	backends := make([]Backend, 0, len(s.mounts))
	for _, v := range s.mounts {
		backends = append(backends, v)
	}
	s.mu.RUnlock()
	var result []string
	for _, v := range backends {
		r, err := v.List(ctx, path)
		if err != nil && err != NotFound {
			return nil, err
		}
		result = append(result, r...)
	}
	sort.Strings(result)
	return result, nil
}

type FileInfo struct {
	Name          string
	ContentLength int64
	ModuleType    string
	RecvDigests   map[string]string
	TimestampUnix int64
}

type DownloadOptions struct {
}

type DB interface {
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Del(ctx context.Context, key string) error
	Set(ctx context.Context, key string, value []byte) error
}
