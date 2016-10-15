package main

import (
	"context"
	"encoding/json"
	"fmt"

	"gopkg.in/redis.v4"
)

type redisKV struct {
	client *redis.Client
	prefix string
}

func NewRedisKV(client *redis.Client, prefix string) *redisKV {
	return &redisKV{
		client: client,
		prefix: prefix,
	}
}

func (s *redisKV) toInternal(key string) string {
	return s.prefix + key
}

func (s *redisKV) fromInternal(key string) (string, error) {
	return trimOr(key, s.prefix, "redis key")
}

func (s *redisKV) Get(_ context.Context, key string) ([]byte, error) {
	fmt.Println(s.toInternal(key))
	res, err := s.client.Get(s.toInternal(key)).Result()
	if err != nil {
		return nil, err
	}
	return []byte(res), nil
}

func (s *redisKV) List(_ context.Context, prefix string) ([]string, error) {
	keys, err := getAllKeys(s.client, s.toInternal(prefix)+"*")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(keys))
	for _, v := range keys {
		if pf, _ := s.fromInternal(v); pf != "" {
			names = append(names, pf)
		}
	}
	if len(names) == 0 {
		names = nil
	}
	return names, nil
}

func (s *redisKV) Del(_ context.Context, key string) error {
	return s.client.Del(s.toInternal(key)).Err()
}

func (s *redisKV) Set(_ context.Context, key string, value []byte) error {
	return s.client.Set(s.toInternal(key), value, 0).Err()
}

func getAllKeys(client *redis.Client, pattern string) ([]string, error) {
	var cursor uint64
	seen := make(map[string]struct{})
	var result []string
	for {
		var keys []string
		var err error
		if keys, cursor, err = client.Scan(cursor, pattern,
			0).Result(); err != nil {
			return nil, err
		}
		for _, v := range keys {
			if _, ok := seen[v]; !ok {
				result = append(result, v)
				seen[v] = struct{}{}
			}
		}
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

type GetKV interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

func KVGetJson(ctx context.Context, kv GetKV, key string, value interface{}) error {
	res, err := kv.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(res, value)
}
