package main // import "git.stingr.net/stingray/advfiler"

import (
	"flag"
	"log"
	"net/http"
	"time"

	bolt "go.etcd.io/bbolt"
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

	boltDb = flag.String("bolt", "", "Bolt file name")
)

func main() {
	flag.Parse()

	var fiKV, meKV filerKV

	if *boltDb != "" {
		db, err := bolt.Open(*boltDb, 0600, &bolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
		db.NoSync = true
		fiKV = NewBoltKV(db, "fs")
		meKV = NewBoltKV(db, "problems")
	} else {

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
		fiKV = NewRedisKV(rc1, *redisPrefixFiles)
		meKV = NewRedisKV(rc2, *redisPrefixProblems)
	}

	f := NewFiler(fiKV, &WeedClient{master: *weedBackend})
	ms := NewMetadataServer(meKV)
	http.Handle("/fs/", f)
	http.HandleFunc("/problem/set/", ms.handleSetManifest)
	http.HandleFunc("/problem/get/", ms.handleGetManifest)
	http.ListenAndServe(*listen, nil)
}
