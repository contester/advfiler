package main

import (
	"net/http"
	"os"

	"github.com/coreos/go-systemd/daemon"
	"github.com/dgraph-io/badger/v4"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/trace"
	"stingr.net/go/systemdutil"

	_ "net/http/pprof"
)

type config struct {
	ListenHTTP      []string `envconfig:"LISTEN_HTTP"`
	BadgerDir       string   `envconfig:"BADGER_DIR"`
	BadgerValueDir  string   `envconfig:"BADGER_VALUE_DIR"`
	ValidAuthTokens []string `envconfig:"VALID_AUTH_TOKENS"`
}

func main() {
	systemdutil.Init()

	var cfg config
	if err := envconfig.Process("advfiler", &cfg); err != nil {
		log.Fatal(err)
	}

	if cfg.BadgerDir == "" {
		log.Fatal("ADVFILER_BADGER_DIR must be specified")
	}

	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) { return true, true }
	http.Handle("/metrics", promhttp.Handler())

	_, httpSockets, _ := systemdutil.ListenSystemd(systemdutil.ActivationFiles())

	authCheck := AuthChecker{
		validTokens: make(map[string]struct{}, len(cfg.ValidAuthTokens)),
	}
	for _, v := range cfg.ValidAuthTokens {
		authCheck.validTokens[v] = struct{}{}
	}

	httpSockets = append(httpSockets, systemdutil.MustListenTCPSlice(cfg.ListenHTTP)...)

	opts := badger.DefaultOptions(cfg.BadgerDir).WithLogger(log.StandardLogger())
	if cfg.BadgerValueDir != "" {
		opts.ValueDir = cfg.BadgerValueDir
	}
	if err := os.MkdirAll(opts.Dir, os.ModePerm); err != nil {
		log.Fatalf("can't create badger dir: %v", err)
	}
	if err := os.MkdirAll(opts.ValueDir, os.ModePerm); err != nil {
		log.Fatalf("can't create badger value dir: %v", err)
	}

	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("can't open badger: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	defer store.Close()

	f := NewFiler(store, &authCheck)
	ms := NewMetadataServer(store)
	xs := NewXMLServer(store, &authCheck)
	http.Handle("/fs/", f)
	http.HandleFunc("/fs2/", f.HandlePackage)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.HandleFunc("/tar/", f.handleTarUpload)
	http.HandleFunc("/wipe/", f.handleWipe)
	http.HandleFunc("/protopackage/", f.handleProtoPackage)
	http.HandleFunc("/protopackage", f.handleProtoPackage)
	http.HandleFunc("/xml/contest/", xs.handleContest)
	http.HandleFunc("/xml/problem/", xs.handleProblem)
	systemdutil.ServeAll(nil, httpSockets, nil)
	daemon.SdNotify(false, daemon.SdNotifyReady)
	defer daemon.SdNotify(false, daemon.SdNotifyStopping)
	systemdutil.WaitSigint()
}
