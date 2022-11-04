package efbackend

import (
	"context"
	"io"
	"time"

	"github.com/contester/advfiler/common"
	"google.golang.org/protobuf/proto"
	"stingr.net/go/efstore/efroot"

	pb "github.com/contester/advfiler/protos"
	log "github.com/sirupsen/logrus"
)

var _ = log.Info

type Filer struct {
	r                  *efroot.Root
	stopChan, doneChan chan struct{}
}

func NewFiler(r *efroot.Root) (*Filer, error) {
	result := Filer{r: r, stopChan: make(chan struct{}, 1), doneChan: make(chan struct{}, 1)}
	return &result, nil
}

func (f *Filer) Close() {
	f.r.Close()
}

func (s *Filer) Upload(ctx context.Context, info common.FileInfo, body io.Reader) (common.UploadStatus, error) {
	hashes := common.NewHashes()
	var cd common.Digests
	rd := common.MapToDigests(info.RecvDigests)
	if info.TimestampUnix == 0 {
		info.TimestampUnix = time.Now().Unix()
	}

	_, err := s.r.Upload(ctx, []byte(info.Name), info.ContentLength, body,
		efroot.UploadOptions{
			Embedded:  info.ContentLength < 4096,
			Overwrite: true,

			Hasher: hashes,
			HashChecker: func() error {
				cd = hashes.Digests()
				if err := common.MaybeCompareHashes(cd, rd); err != nil {
					return err
				}
				return nil
			},
			ExtraMetadata: func() []byte {
				v := &pb.FileMetadata{
					ModuleType: info.ModuleType,
					Digests: &pb.Digests{
						Md5:    cd.MD5,
						Sha1:   cd.SHA1,
						Sha256: cd.SHA256,
					},
					TimestampUnix: info.TimestampUnix,
				}

				b, _ := proto.Marshal(v)
				return b
			},
		})
	if err != nil {
		return common.UploadStatus{}, err
	}

	result := common.UploadStatus{
		Digests: common.DigestsToMap(cd),
		Size:    info.ContentLength,
	}
	return result, err
}

func (f *Filer) Delete(ctx context.Context, path string) error {
	return f.r.Delete(ctx, []byte(path))
}

type downloadResult struct {
	size                  int64
	moduleType            string
	digests               common.Digests
	lastModifiedTimestamp int64
	body                  io.ReadSeekCloser
}

func (s downloadResult) Size() int64 {
	return s.size
}

func (s downloadResult) ModuleType() string {
	return s.moduleType
}

func (s downloadResult) Digests() common.Digests {
	return s.digests
}

func (s downloadResult) LastModifiedTimestamp() int64 {
	return s.lastModifiedTimestamp
}

func (s downloadResult) Body() io.ReadSeekCloser {
	return s.body
}

func (f *Filer) Download(ctx context.Context, path string, options common.DownloadOptions) (common.DownloadResult, error) {
	rr, err := f.r.Download(ctx, []byte(path), efroot.DownloadOptions{})
	if err != nil {
		return nil, err
	}

	v := &pb.FileMetadata{}
	if err := proto.Unmarshal(rr.ExtraMetadata(), v); err != nil {
		rr.Body().Close()
		return nil, err
	}

	r := downloadResult{
		size:                  rr.Size(),
		moduleType:            v.GetModuleType(),
		digests:               common.DigestsFromProto(v.GetDigests()),
		lastModifiedTimestamp: v.GetTimestampUnix(),
		body:                  rr.Body(),
	}

	return r, nil
}

func (f *Filer) List(ctx context.Context, path string) (result []string, err error) {
	err = f.r.Iterate(ctx, []byte(path), func(key []byte, metadata func() (efroot.ObjectMetadata, error)) error {
		result = append(result, string(key))
		return nil
	})
	return result, err
}
