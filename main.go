package main // import "git.stingr.net/stingray/advfiler"

import (
	"context"
	"errors"
	"net/http"

	"github.com/contester/advfiler/efbackend"
	"github.com/contester/advfiler/ldbackend"
	"github.com/coreos/go-systemd/daemon"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	"golang.org/x/net/trace"
	"stingr.net/go/efstore/efcommon"
	"stingr.net/go/efstore/efroot"
	"stingr.net/go/systemdutil"

	log "github.com/sirupsen/logrus"

	_ "net/http/pprof"
)

type conf3 struct {
	ListenHTTP []string `envconfig:"LISTEN_HTTP"`
	ManifestDB string   `envconfig:"MANIFEST_DB"`

	FilerDB    string `envconfig:"FILER_DB"`
	FilerStore string `envconfig:"FILER_STORE"`

	ValidAuthTokens []string `envconfig:"VALID_AUTH_TOKENS"`
	EnableDebug     bool
}

func levelOpen(path string) (*leveldb.DB, error) {
	return leveldb.OpenFile(path, nil)
}

type leveldbAdapter struct {
	db *leveldb.DB
}

func (s *leveldbAdapter) Get(key []byte) ([]byte, error) {
	r, err := s.db.Get(key, nil)
	if err != nil && errors.Is(err, leveldb.ErrNotFound) {
		err = efcommon.ErrNotFound
	}
	return r, err
}

func (s *leveldbAdapter) Put(key, value []byte) error {
	return s.db.Put(key, value, nil)
}

func (s *leveldbAdapter) Delete(key []byte) error {
	return s.db.Delete(key, nil)
}

func (s *leveldbAdapter) Iterate(prefix []byte, f func(key []byte, value func() []byte) error) error {
	iter := s.db.NewIterator(util.BytesPrefix(prefix), nil)
	defer iter.Release()

	for iter.Next() {
		if err := f(iter.Key(), iter.Value); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	systemdutil.Init()

	var config conf3
	if err := envconfig.Process("advfiler", &config); err != nil {
		log.Fatal(err)
	}

	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) { return true, true }
	http.Handle("/metrics", promhttp.Handler())

	_, httpSockets, _ := systemdutil.ListenSystemd(systemdutil.ActivationFiles())

	authCheck := AuthChecker{
		validTokens: make(map[string]struct{}, len(config.ValidAuthTokens)),
	}

	for _, v := range config.ValidAuthTokens {
		authCheck.validTokens[v] = struct{}{}
	}

	httpSockets = append(httpSockets, systemdutil.MustListenTCPSlice(config.ListenHTTP)...)

	if config.ManifestDB == "" || config.FilerDB == "" {
		log.Fatal("database directories must be specified")
	}

	mbdb, err := levelOpen(config.ManifestDB)
	if err != nil {
		log.Fatalf("can't open manifest db: %v", err)
	}
	defer mbdb.Close()
	fldb, err := levelOpen(config.FilerDB)
	if err != nil {
		log.Fatalf("can't open filer db: %v", err)
	}
	defer fldb.Close()

	ffb, err := efroot.New(context.Background(), config.FilerStore, &leveldbAdapter{fldb}, 4*1024*1024*1024)
	if err != nil {
		log.Fatal("can't create efstore")
	}

	fb, err := efbackend.NewFiler(ffb)
	if err != nil {
		log.Fatalf("can't create ef filer: %v", err)
	}
	defer fb.Close()
	// if config.EnableDebug {
	// 	http.HandleFunc("/debugList/", fb.DebugList)
	// 	http.HandleFunc("/debugGC/", fb.DebugGC)
	// }

	f := NewFiler(fb, &authCheck)
	ms := NewMetadataServer(ldbackend.New(mbdb, nil))
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.HandleFunc("/tar/", f.handleTarUpload)
	http.HandleFunc("/wipe/", f.handleWipe)
	http.HandleFunc("/protopackage/", f.handleProtoPackage)
	http.HandleFunc("/protopackage", f.handleProtoPackage)
	systemdutil.ServeAll(nil, httpSockets, nil)
	daemon.SdNotify(false, daemon.SdNotifyReady)
	defer daemon.SdNotify(false, daemon.SdNotifyStopping)
	systemdutil.WaitSigint()
}
