package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	origV, origC := version, commit
	version, commit = "v1.2.3", "abc1234"
	t.Cleanup(func() { version, commit = origV, origC })
	got := versionString()
	if !strings.HasPrefix(got, "gsm v1.2.3 (commit abc1234, ") || !strings.HasSuffix(got, ", "+runtime.Version()+")") {
		t.Fatalf("versionString() = %q", got)
	}
}

// version must work before (and without) a resolvable data dir.
func TestVersionNeedsNoHome(t *testing.T) {
	for _, k := range []string{"HOME", "GSM_HOME", "XDG_DATA_HOME", "LOCALAPPDATA"} {
		t.Setenv(k, "")
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if err := run(args); err != nil {
			t.Fatalf("gsm %v: %v", args, err)
		}
	}
}
