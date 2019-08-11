package main // import "git.stingr.net/stingray/advfiler"

import (
	"net/http"
	"os"
	"path/filepath"

	"git.sgu.ru/sgu/systemdutil"
	"git.stingr.net/stingray/advfiler/badgerbackend"
	"github.com/coreos/go-systemd/daemon"
	"github.com/dgraph-io/badger"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/trace"

	log "github.com/sirupsen/logrus"

	_ "net/http/pprof"
)

type conf3 struct {
	ListenHTTP       []string `envconfig:"LISTEN_HTTP"`
	ManifestBadgerDB string   `envconfig:"MANIFEST_BDB"`
	FilerBadgerDB    string   `envconfig:"FILER_BDB"`
	ValidAuthTokens  []string `envconfig:"VALID_AUTH_TOKENS"`
	EnableDebug      bool
}

func badgerOpen(path string) (*badger.DB, error) {
	opt := badger.DefaultOptions

	opt.Dir = filepath.Join(path, "keys")
	opt.ValueDir = filepath.Join(path, "values")
	modBadgerOpts(&opt)

	if err := os.MkdirAll(opt.Dir, os.ModePerm); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opt.ValueDir, os.ModePerm); err != nil {
		return nil, err
	}

	opt.SyncWrites = false

	return badger.Open(opt)
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

	if config.ManifestBadgerDB == "" || config.FilerBadgerDB == "" {
		log.Fatal("database directories must be specified")
	}

	mbdb, err := badgerOpen(config.ManifestBadgerDB)
	if err != nil {
		log.Fatalf("can't open manifest db: %v", err)
	}
	defer mbdb.Close()
	fbdb, err := badgerOpen(config.FilerBadgerDB)
	if err != nil {
		log.Fatalf("can't open filer db: %v", err)
	}
	defer fbdb.Close()
	fb, err := badgerbackend.NewFiler(fbdb)
	if err != nil {
		log.Fatalf("can't create badger filer: %v", err)
	}
	defer fb.Close()
	if config.EnableDebug {
		http.HandleFunc("/debugList/", fb.DebugList)
		http.HandleFunc("/debugGC/", fb.DebugGC)
	}

	f := NewFiler(fb, &authCheck)
	ms := NewMetadataServer(badgerbackend.NewKV(mbdb, nil))
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.HandleFunc("/tar/", f.handleTarUpload)
	http.HandleFunc("/wipe/", f.handleWipe)
	http.HandleFunc("/protopackage/", f.handleProtoPackage)
	http.HandleFunc("/protopackage", f.handleProtoPackage)
	systemdutil.ServeAll(nil, httpSockets, nil)
	daemon.SdNotify(false, "READY=1")
	systemdutil.WaitSigint()
	log.Infof("stopping")
	daemon.SdNotify(false, "STOPPING=1")
}
