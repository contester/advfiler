package main

import (
	"os"

	"github.com/dgraph-io/badger"
	"github.com/coreos/go-systemd/activation"
	"github.com/wercker/journalhook"
)

func setupJournalhook() {
	journalhook.Enable()
}

func activationFiles() []*os.File {
	return activation.Files(true)
}

func modBadgerOpts(opts *badger.Options) {}