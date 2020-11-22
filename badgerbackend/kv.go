package badgerbackend

import (
	"context"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger/v2"
)

type KV struct {
	db     *badger.DB
	prefix []byte
}

func NewKV(db *badger.DB, prefix []byte) *KV {
	s := &KV{
		db:     db,
		prefix: prefix,
	}
	return s
}

func (s *KV) makeKey(key string) []byte {
	if len(s.prefix) == 0 {
		return []byte(key)
	}
	result := make([]byte, 0, len(s.prefix)+len(key))
	result = append(result, s.prefix...)
	return append(result, []byte(key)...)
}

func updateWithRetry(db *badger.DB, f func(tx *badger.Txn) error) error {
	for {
		if err := db.Update(f); err != badger.ErrConflict {
			return err
		}
	}
}

func (s *KV) Get(_ context.Context, key string) (res []byte, err error) {
	mk := s.makeKey(key)
	err = s.db.View(func(tx *badger.Txn) error {
		r, err := tx.Get(mk)
		if err != nil {
			return err
		}
		res, err = r.ValueCopy(nil)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return nil, common.NotFound
	}
	return res, err
}

func (s *KV) List(_ context.Context, prefix string) (res []string, err error) {
	pfx := s.makeKey(prefix)
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	err = s.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()
		for iter.Seek(pfx); iter.ValidForPrefix(pfx); iter.Next() {
			res = append(res, string(iter.Item().Key()[len(s.prefix):]))
		}
		return nil
	})
	return res, err
}

func (s *KV) Del(_ context.Context, key string) error {
	pk := s.makeKey(key)
	return updateWithRetry(s.db, func(tx *badger.Txn) error {
		return tx.Delete(pk)
	})
}

func (s *KV) Set(_ context.Context, key string, value []byte) error {
	pk := s.makeKey(key)
	return updateWithRetry(s.db, func(tx *badger.Txn) error {
		return tx.Set(pk, value)
	})
}
