package efbackend

import (
	"context"
	"io"

	"github.com/contester/advfiler/common"
	"stingr.net/go/efstore/efcommon"
	"stingr.net/go/efstore/efroot"

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
	st, err := s.r.Upload(ctx, []byte(info.Name), efroot.FileInfo{
		ContentLength: info.ContentLength,
		ModuleType:    info.ModuleType,
		TimestampUnix: info.TimestampUnix,
		RecvDigests:   efcommon.MapToDigests(info.RecvDigests),
	}, body)
	if err != nil {
		return common.UploadStatus{}, err
	}

	result := common.UploadStatus{
		Digests: efcommon.DigestsToMap(st.Digests),
		Size:    st.Size,
	}
	return result, err
}

func (f *Filer) Delete(ctx context.Context, path string) error {
	return f.r.Delete(ctx, []byte(path))
}

func (f *Filer) Download(ctx context.Context, path string, options common.DownloadOptions) (common.DownloadResult, error) {
	rr, err := f.r.Download(ctx, []byte(path), efroot.DownloadOptions{})
	if err != nil {
		return nil, err
	}
	return rr, nil
}

func (f *Filer) List(ctx context.Context, path string) ([]string, error) {
	rr, err := f.r.List(ctx, []byte(path))
	if err != nil {
		return nil, err
	}
	r := make([]string, len(rr))
	for i, v := range rr {
		r[i] = string(v)
	}
	return r, nil
}
