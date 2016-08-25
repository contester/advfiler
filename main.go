package main

import (
	"flag"
	"net/http"

	"gopkg.in/redis.v4"
)

var (
	listen = flag.String("listen", ":9094", "")

	redisBackend  = flag.String("redis", "localhost:6379", "")
	redisPassword = flag.String("redis_password", "", "")
	redisDb       = flag.Int("redis_db", 0, "")
)

func main() {
	flag.Parse()

	rc := redis.NewClient(&redis.Options{
		Addr:     *redisBackend,
		Password: *redisPassword,
		DB:       *redisDb,
	})

	f := NewFiler(rc, &WeedClient{master: "http://localhost:9333"})
	ms := NewMetadataServer(rc)
	http.Handle("/fs/", f)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.ListenAndServe(*listen, nil)
}
