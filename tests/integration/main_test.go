//go:build linux

// Package integration checks one guarantee at a time where gsm meets the
// OS or another process: process groups, several servers at once, the
// snapshot store lock, cleanup after a failed install. It drives the gsm
// binary with the fakes in tests/internal/harness.
package integration

import (
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

func TestMain(m *testing.M) { harness.Main(m) }
