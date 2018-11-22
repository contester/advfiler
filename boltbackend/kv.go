package boltbackend

import (
	"bytes"
	"context"

	"git.stingr.net/stingray/advfiler/common"

	bolt "go.etcd.io/bbolt"
)

type KV struct {
	db     *bolt.DB
	bucket []byte
}

func NewKV(db *bolt.DB, bucket []byte) *KV {
	s := &KV{
		db:     db,
		bucket: bucket,
	}
	s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(s.bucket)
		return err
	})
	return s
}

func (s *KV) Get(_ context.Context, key string) (res []byte, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		r := tx.Bucket(s.bucket).Get([]byte(key))
		if r == nil {
			return common.NotFound
		}
		res = append(res, r...)
		return nil
	})
	return res, err
}

func (s *KV) List(_ context.Context, prefix string) (res []string, err error) {
	pr := []byte(prefix)
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(s.bucket).Cursor()
		for k, _ := c.Seek(pr); k != nil && bytes.HasPrefix(k, pr); k, _ = c.Next() {
			res = append(res, string(k))
		}
		return nil
	})
	return res, err
}

func (s *KV) Del(_ context.Context, key string) error {
	return s.db.Batch(func(tx *bolt.Tx) error {
		return tx.Bucket(s.bucket).Delete([]byte(key))
	})
}

func (s *KV) Set(_ context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(s.bucket).Put([]byte(key), value)
	})
}
