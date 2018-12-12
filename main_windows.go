package main

import (
	"os"

	"github.com/dgraph-io/badger"
)

func setupJournalhook() {}

func activationFiles() []*os.File { return nil }

func modBadgerOpts(opts *badger.Options) {
	opts.Truncate = true
}
