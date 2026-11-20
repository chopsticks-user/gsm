package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is set at release build time: -ldflags "-X main.version=v1.2.3".
// Without it, the module version from the build info is used when there is one.
var version = "dev"

// versionString describes this binary: its version, the commit it was built
// from (when the build recorded it) and the Go toolchain.
func versionString() string {
	v, commit, dirty := version, "unknown", false
	if bi, ok := debug.ReadBuildInfo(); ok {
		// go install module@v1.2.3 and builds from a tagged checkout record it.
		if v == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			v = bi.Main.Version
		}
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
	return fmt.Sprintf("gsm %s (commit %s, %s)", v, commit, runtime.Version())
}
