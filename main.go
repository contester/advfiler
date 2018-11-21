package main // import "git.stingr.net/stingray/advfiler"

import (
	"flag"
	"net/http"
	"time"

	"git.sgu.ru/sgu/systemdutil"
	"github.com/coreos/go-systemd/daemon"
	"git.stingr.net/stingray/advfiler/filer"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/trace"

	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"

	_ "net/http/pprof"
)

type conf3 struct {
	BoltDB      string   `envconfig:"BOLT_DB"`
	ListenHTTP  []string `envconfig:"LISTEN_HTTP"`
	WeedBackend string   `envconfig:"WEED_BACKEND",default:"http://localhost:9333"`
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

	if config.BoltDB == "" {
		log.Fatal("BOLT_DB needs to be specified")
	}

	httpSockets = append(httpSockets, systemdutil.MustListenTCPSlice(config.ListenHTTP)...)

	var fiKV, meKV filer.KV

	db, err := bolt.Open(config.BoltDB, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.NoSync = true
	fiKV = NewBoltKV(db, "fs")
	meKV = NewBoltKV(db, "problems")

	filer1 := filer.NewBolt(fiKV, &WeedClient{master: config.WeedBackend})

	f := NewFiler(filer1)
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
