package main // import "git.stingr.net/stingray/advfiler"

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"git.sgu.ru/sgu/systemdutil"
	"git.stingr.net/stingray/advfiler/filer"
	"git.stingr.net/stingray/advfiler/badgerbackend"
	"github.com/coreos/go-systemd/daemon"
	"github.com/dgraph-io/badger"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/trace"

	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"

	_ "net/http/pprof"
)

type conf3 struct {
	BoltDB           string   `envconfig:"BOLT_DB"`
	ListenHTTP       []string `envconfig:"LISTEN_HTTP"`
	WeedBackend      string   `envconfig:"WEED_BACKEND" default:"http://localhost:9333"`
	ManifestBadgerDB string   `envconfig:"MANIFEST_BDB"`
	FilerBadgerDB    string   `envconfig:"FILER_BDB"`
}

func badgerOpen(path string) (*badger.DB, error) {
	opt := badger.DefaultOptions

	opt.Dir = filepath.Join(path, "keys")
	opt.ValueDir = filepath.Join(path, "values")

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
	flag.Parse()

	setupJournalhook()
	systemdutil.Logger = log.StandardLogger()
	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) { return true, true }
	http.Handle("/metrics", prometheus.Handler())

	_, httpSockets, _ := systemdutil.ListenSystemd(activationFiles())

	var config conf3

	if err := envconfig.Process("advfiler", &config); err != nil {
		log.Fatal(err)
	}

	httpSockets = append(httpSockets, systemdutil.MustListenTCPSlice(config.ListenHTTP)...)

	var meKV filer.KV
	var filerBackend filer.Backend

	if config.ManifestBadgerDB != "" && config.FilerBadgerDB != "" {
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
		filerBackend, err = filer.NewBadger(fbdb)
		if err != nil {
			log.Fatalf("can't create badger filer: %v", err)
		}
		meKV = badgerbackend.NewKV(mbdb, nil)
	} else {
		if config.BoltDB == "" {
			log.Fatal("BOLT_DB needs to be specified")
		}
			db, err := bolt.Open(config.BoltDB, 0600, &bolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
		db.NoSync = true
		fiKV := NewBoltKV(db, "fs")
		meKV = NewBoltKV(db, "problems")
		filerBackend = filer.NewBolt(fiKV, &WeedClient{master: config.WeedBackend})
	}

	f := NewFiler(filerBackend)
	ms := NewMetadataServer(meKV)
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	for _, s := range httpSockets {
		go http.Serve(s, nil)
	}
	daemon.SdNotify(false, "READY=1")
	systemdutil.WaitSigint()
}
