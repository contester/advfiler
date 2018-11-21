package common

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/golang/protobuf/proto"
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
