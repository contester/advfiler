package ldbackend

import (
	"context"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type KV struct {
	db     *leveldb.DB
	prefix []byte
}

func New(db *leveldb.DB, prefix []byte) *KV {
	return &KV{
		db:     db,
		prefix: prefix,
	}
}

func (s *KV) makeKey(key string) []byte {
	if len(s.prefix) == 0 {
		return []byte(key)
	}
	result := make([]byte, 0, len(s.prefix)+len(key))
	result = append(result, s.prefix...)
	return append(result, []byte(key)...)
}

func (s *KV) Get(ctx context.Context, key string) ([]byte, error) {
	return s.db.Get(s.makeKey(key), nil)
}

func (s *KV) List(ctx context.Context, prefix string) (result []string, err error) {
	iter := s.db.NewIterator(util.BytesPrefix(s.makeKey(prefix)), nil)
	defer iter.Release()

	for iter.Next() {
		result = append(result, string(iter.Key()[len(s.prefix):]))
	}

	return result, nil
}

func (s *KV) Del(ctx context.Context, key string) error {
	return s.db.Delete(s.makeKey(key), nil)
}

func (s *KV) Set(ctx context.Context, key string, value []byte) error {
	return s.db.Put(s.makeKey(key), value, nil)
}
