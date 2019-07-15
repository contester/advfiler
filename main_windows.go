package main

import (
	"github.com/dgraph-io/badger"
)

func modBadgerOpts(opts *badger.Options) {
	opts.Truncate = true
}
