//go:build linux

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/chopsticks-user/gsm/tests/internal/harness"
)

// A create whose install fails, retry included, leaves nothing behind, so
// the name is free for the next try.
func TestFailedCreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	out, err := h.Run([]string{"GSM_TEST_STEAMCMD_FAIL=1"}, "create", "palworld", "main")
	if err == nil {
		t.Fatalf("create with a failing steamcmd succeeded:\n%s", out)
	}
	harness.MustContain(t, out, "trying once more")
	if n := strings.Count(out, "fake steamcmd: failing as asked"); n != 2 {
		t.Fatalf("steamcmd ran %d times, want 2 (one retry):\n%s", n, out)
	}
	if _, err := os.Stat(h.Inst("main")); !os.IsNotExist(err) {
		t.Fatalf("instances/main after a failed create: %v", err)
	}
	harness.MustContain(t, h.OK("create", "palworld", "main"), "created main (Palworld, port 8211)")
}
