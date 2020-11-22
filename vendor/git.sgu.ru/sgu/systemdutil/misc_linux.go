package systemdutil

import (
	"os"

	"git.sgu.ru/sgu/journalhook"
	"github.com/coreos/go-systemd/v22/activation"
)

func SetupJournalhook(what bool) {
	if !what || journalHookEnabled {
		return
	}
	journalHookEnabled = true
	journalhook.Enable()
}

func ActivationFiles() []*os.File {
	return activation.Files(true)
}

var journalHookEnabled bool

func findVar(names ...string) bool {
	for _, v := range names {
		if _, ok := os.LookupEnv(v); ok {
			return true
		}
	}
	return false
}

func jhInit() {
	if journalHookEnabled {
		return
	}
	if findVar("LISTEN_PID", "LISTEN_FDS") {
		SetupJournalhook(true)
	}
}
