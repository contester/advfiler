package main

import (
	"flag"
	"net/http"

	"gopkg.in/redis.v4"
)

var (
	listen = flag.String("listen", ":9094", "Listen port")

	weedBackend = flag.String("weed", "http://localhost:9333", "WeedFS backend")

	redisBackend  = flag.String("redis", "localhost:6379", "Redis backend")
	redisPassword = flag.String("redis_password", "", "Redis password")

	redisDbFiles     = flag.Int("redis_db_files", 0, "Redis DB for files")
	redisPrefixFiles = flag.String("redis_prefix_files", "fs/", "Prefix for redis keys for files")

	redisDbProblems     = flag.Int("redis_db_problems", 0, "Redis DB for problems")
	redisPrefixProblems = flag.String("redis_prefix_problems", "problem/", "Prefix for redis keys for problems")
)

func main() {
	flag.Parse()

	rc1 := redis.NewClient(&redis.Options{
		Addr:     *redisBackend,
		Password: *redisPassword,
		DB:       *redisDbFiles,
	})
	defer rc1.Close()

	rc2 := redis.NewClient(&redis.Options{
		Addr:     *redisBackend,
		Password: *redisPassword,
		DB:       *redisDbProblems,
	})
	defer rc2.Close()

	f := NewFiler(NewRedisKV(rc1, *redisPrefixFiles), &WeedClient{master: *weedBackend})
	ms := NewMetadataServer(NewRedisKV(rc2, *redisPrefixProblems))
	http.Handle("/fs/", f)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.ListenAndServe(*listen, nil)
}
