package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitDiffLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Head() != "" {
		t.Fatal("HEAD before first commit")
	}
	os.WriteFile(filepath.Join(dir, "a.ini"), []byte("x=1\n"), 0o644)
	if ok, err := r.CommitAll("first"); !ok || err != nil {
		t.Fatalf("commit: %v %v", ok, err)
	}
	if ok, err := r.CommitAll("nothing"); ok || err != nil {
		t.Fatalf("empty commit: %v %v", ok, err)
	}
	os.WriteFile(filepath.Join(dir, "a.ini"), []byte("x=2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.ini"), []byte("y=1\n"), 0o644)
	diff, err := r.Diff()
	if err != nil || !strings.Contains(diff, "+x=2") || !strings.Contains(diff, "new.ini") {
		t.Fatalf("diff %q, %v", diff, err)
	}
	if ok, err := r.CommitAll("second"); !ok || err != nil {
		t.Fatalf("commit: %v %v", ok, err)
	}
	log, err := r.Log()
	if err != nil || strings.Count(log, "\n") != 2 || !strings.Contains(log, "second") {
		t.Fatalf("log %q, %v", log, err)
	}
}
