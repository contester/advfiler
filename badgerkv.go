package main

import (
	"context"

	"git.stingr.net/stingray/advfiler/common"
	"github.com/dgraph-io/badger"
)

type badgerKV struct {
	db     *badger.DB
	prefix byte
}

func NewBadgerKV(db *badger.DB, prefixByte byte) *badgerKV {
	s := &badgerKV{
		db:     db,
		prefix: prefixByte,
	}
	return s
}

func (s *badgerKV) makeKey(key string) []byte {
	result := make([]byte, 1, len(key)+1)
	result[0] = s.prefix
	return append(result, []byte(key)...)
}

func (s *badgerKV) Get(_ context.Context, key string) (res []byte, err error) {
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

func (s *badgerKV) List(_ context.Context, prefix string) (res []string, err error) {
	pfx := s.makeKey(prefix)
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	err = s.db.View(func(tx *badger.Txn) error {
		iter := tx.NewIterator(opts)
		defer iter.Close()
		for iter.Seek(pfx); iter.ValidForPrefix(pfx); iter.Next() {
			res = append(res, string(iter.Item().Key()[1:]))
		}
		return nil
	})
	return res, err
}

func (s *badgerKV) Del(_ context.Context, key string) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Delete(s.makeKey(key))
	})
}

func (s *badgerKV) Set(_ context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *badger.Txn) error {
		return tx.Set(s.makeKey(key), value)
	})
}
