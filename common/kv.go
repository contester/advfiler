package common

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/golang/protobuf/proto"

	pb "git.stingr.net/stingray/advfiler/protos"
)

var NotFound = errors.New("not found")

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

func KVGetProto(ctx context.Context, kv GetKV, key string, value proto.Message) error {
	res, err := kv.Get(ctx, key)
	if err != nil {
		return err
	}
	return proto.Unmarshal(res, value)
}

func maybeSetDigest(m map[string]string, name string, value []byte) {
	if len(value) > 0 {
		m[name] = base64.StdEncoding.EncodeToString(value)
	}
}

func DigestsToMap(d *pb.Digests) map[string]string {
	if d == nil {
		return nil
	}
	r := make(map[string]string)
	maybeSetDigest(r, "MD5", d.Md5)
	maybeSetDigest(r, "SHA", d.Sha1)
	if len(r) == 0 {
		return nil
	}
	return r
}

func CheckDigests(recv, computed map[string]string) bool {
	for k, v := range computed {
		if prev, ok := recv[k]; ok && v != prev {
			return false
		}
	}
	return true
}
