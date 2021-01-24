package main

import (
	"github.com/dgraph-io/badger/v3"
)

func modBadgerOpts(opts *badger.Options) {
	opts.Truncate = true
}
