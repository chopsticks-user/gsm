package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	orig := version
	version = "v1.2.3"
	t.Cleanup(func() { version = orig })
	got := versionString()
	if !strings.HasPrefix(got, "gsm v1.2.3 (commit ") || !strings.HasSuffix(got, ", "+runtime.Version()+")") {
		t.Fatalf("versionString() = %q", got)
	}
}
