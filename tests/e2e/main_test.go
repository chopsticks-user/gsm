//go:build linux

// Package e2e drives the gsm binary through whole user journeys, with fakes
// for Steam and the game servers (see tests/internal/harness).
package e2e

import (
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

func TestMain(m *testing.M) { harness.Main(m) }
