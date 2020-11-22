package main

import (
	"github.com/dgraph-io/badger/v2"
)

func modBadgerOpts(opts *badger.Options) {
	opts.Truncate = true
}
