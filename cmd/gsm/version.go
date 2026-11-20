package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is set at release build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// versionString describes this binary: its version, the commit it was built
// from (when the build recorded it) and the Go toolchain.
func versionString() string {
	commit, dirty := "unknown", false
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value[:min(12, len(s.Value))]
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	if dirty {
		commit += "-dirty"
	}
	return fmt.Sprintf("gsm %s (commit %s, %s)", version, commit, runtime.Version())
}
