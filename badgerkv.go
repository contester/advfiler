package main

import (
	"context"

	"github.com/dgraph-io/badger"
)

type badgerKV struct {
	db     *badger.DB
	prefix string
}

func NewBadgerKV(db *badger.DB, bucket string) *boltKV {
	s := &badgerKV{
		db:     db,
		prefix: bucket,
	}
	return s
}

func (s *badgerKV) makeKey(key string) string {
	return s.prefix + "|" + key
}

func (s *badgerKV) Get(_ context.Context, key string) (res []byte, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		r := tx.Bucket(s.bucket).Get([]byte(s.makeKey(key)))
		if r == nil {
			return NotFound
		}
		res = append(res, r...)
		return nil
	})
	return res, err
}

func (s *badgerKV) List(_ context.Context, prefix string) (res []string, err error) {
	pfx := s.makeKey(prefix)
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	err = s.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()

		c := tx.Bucket(s.bucket).Cursor()
		pr := []byte(pfx)
		for iter.Seek(pr); iter.ValidForPrefix(pr); iter.Next() {
			res = append(res, string(iter.Item().Key()))
		}
		return nil
	})
	return res, err
}

func (s *badgerKV) Del(_ context.Context, key string) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Delete([]byte(s.makeKey(key)))
	})
}

func (s *badgerKV) Set(_ context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Set([]byte(s.makeKey(key)), value)
	})
}
