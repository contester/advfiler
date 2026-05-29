package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"time"

	"github.com/dgraph-io/badger/v4"
	pb "github.com/contester/advfiler/protos"
	"google.golang.org/protobuf/proto"
)

// Key prefixes for Polygon XML records.
const (
	prefixContest byte = 0x05
	prefixProblem byte = 0x06
)

// contestRecordKey: 0x05 + key
func contestRecordKey(key string) []byte {
	k := make([]byte, 0, 1+len(key))
	k = append(k, prefixContest)
	k = append(k, key...)
	return k
}

// problemRecordPrefix: 0x06 + key + 0x00  (all revisions of a problem)
func problemRecordPrefix(key string) []byte {
	k := make([]byte, 0, 1+len(key)+1)
	k = append(k, prefixProblem)
	k = append(k, key...)
	k = append(k, 0x00)
	return k
}

// problemRecordKey: 0x06 + key + 0x00 + be64(revision)
func problemRecordKey(key string, revision int64) []byte {
	k := problemRecordPrefix(key)
	var rev [8]byte
	binary.BigEndian.PutUint64(rev[:], uint64(revision))
	return append(k, rev[:]...)
}

func nowIfZero(ts int64) int64 {
	if ts == 0 {
		return time.Now().Unix()
	}
	return ts
}

// SetContest stores (or overwrites) a contest's XML and timestamp.
// ts == 0 means "use the current time".
func (s *Store) SetContest(ctx context.Context, key string, content []byte, ts int64) error {
	rec := pb.ContestRecord_builder{
		Content:       content,
		TimestampUnix: proto.Int64(nowIfZero(ts)),
	}.Build()
	return s.db.Update(func(tx *badger.Txn) error {
		return setProto(tx, contestRecordKey(key), rec)
	})
}

// GetContest returns a contest's XML and timestamp, or a wrapped fs.ErrNotExist.
func (s *Store) GetContest(ctx context.Context, key string) (content []byte, ts int64, err error) {
	err = s.db.View(func(tx *badger.Txn) error {
		rec, gerr := getProto[pb.ContestRecord](tx, contestRecordKey(key))
		if gerr == badger.ErrKeyNotFound {
			return fmt.Errorf("%w: contest %s", fs.ErrNotExist, key)
		}
		if gerr != nil {
			return gerr
		}
		content = rec.GetContent()
		ts = rec.GetTimestampUnix()
		return nil
	})
	return content, ts, err
}

// SetProblem stores (or overwrites) one revision of a problem.
// ts == 0 means "use the current time".
func (s *Store) SetProblem(ctx context.Context, key string, revision int64, content []byte, ts int64) error {
	rec := pb.ProblemRecord_builder{
		Content:       content,
		TimestampUnix: proto.Int64(nowIfZero(ts)),
		Revision:      proto.Int64(revision),
	}.Build()
	return s.db.Update(func(tx *badger.Txn) error {
		return setProto(tx, problemRecordKey(key, revision), rec)
	})
}

// GetProblem returns a specific revision's XML and timestamp.
func (s *Store) GetProblem(ctx context.Context, key string, revision int64) (content []byte, ts int64, err error) {
	err = s.db.View(func(tx *badger.Txn) error {
		rec, gerr := getProto[pb.ProblemRecord](tx, problemRecordKey(key, revision))
		if gerr == badger.ErrKeyNotFound {
			return fmt.Errorf("%w: problem %s revision %d", fs.ErrNotExist, key, revision)
		}
		if gerr != nil {
			return gerr
		}
		content = rec.GetContent()
		ts = rec.GetTimestampUnix()
		return nil
	})
	return content, ts, err
}

// GetLatestProblem returns the highest-numbered revision of a problem.
func (s *Store) GetLatestProblem(ctx context.Context, key string) (content []byte, revision int64, ts int64, err error) {
	prefix := problemRecordPrefix(key)
	err = s.db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = prefix
		it := tx.NewIterator(opts)
		defer it.Close()

		// Seek past the largest possible key under the prefix so reverse
		// iteration lands on the highest revision.
		seek := append(append([]byte{}, prefix...), 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
		it.Seek(seek)
		if !it.ValidForPrefix(prefix) {
			return fmt.Errorf("%w: problem %s", fs.ErrNotExist, key)
		}
		return it.Item().Value(func(v []byte) error {
			var rec pb.ProblemRecord
			if uerr := proto.Unmarshal(v, &rec); uerr != nil {
				return uerr
			}
			content = rec.GetContent()
			revision = rec.GetRevision()
			ts = rec.GetTimestampUnix()
			return nil
		})
	})
	return content, revision, ts, err
}
