package main

import (
	"flag"
	"fmt"
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
	f := filerServer{
		redisClient: redis.NewClient(&redis.Options{
			Addr:     *redisBackend,
			Password: *redisPassword,
			DB:       *redisDb,
		}),
		weed: &WeedClient{master: "http://localhost:9333"},
	}
	ms := metadataServer{
		redisClient: f.redisClient,
	}
	fmt.Println(f.redisClient.Ping().Result())
	http.Handle("/fs/", &f)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.ListenAndServe(*listen, nil)
}
