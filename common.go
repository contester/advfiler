package main

import "gopkg.in/redis.v4"

func getAllKeys(client *redis.Client, pattern string) ([]string, error) {
	var cursor uint64
	seen := make(map[string]struct{})
	var result []string
	for {
		var keys []string
		var err error
		keys, cursor, err = client.Scan(cursor, pattern, 100).Result()
		if err != nil {
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
