package main

import (
	"bytes"
	"context"
	"errors"

	"github.com/boltdb/bolt"
)

type boltKV struct {
	db     *bolt.DB
	bucket []byte
}

func NewBoltKV(db *bolt.DB, bucket string) *boltKV {
	s := &boltKV{
		db:     db,
		bucket: []byte(bucket),
	}
	s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(s.bucket)
		return err
	})
	return s
}

var NotFound = errors.New("not found")

func (s *boltKV) Get(_ context.Context, key string) (res []byte, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		r := tx.Bucket(s.bucket).Get([]byte(key))
		if r == nil {
			return NotFound
		}
		res = append(res, r...)
		return nil
	})
	return res, err
}

func (s *boltKV) List(_ context.Context, prefix string) (res []string, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(s.bucket).Cursor()
		pr := []byte(prefix)
		for k, _ := c.Seek(pr); bytes.HasPrefix(k, pr); k, _ = c.Next() {
			res = append(res, string(k))
		}
		return nil
	})
	return res, err
}

func (s *boltKV) Del(_ context.Context, key string) error {
	return s.db.Batch(func(tx *bolt.Tx) error {
		return tx.Bucket(s.bucket).Delete([]byte(key))
	})
}

func (s *boltKV) Set(_ context.Context, key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(s.bucket).Put([]byte(key), value)
	})
}
