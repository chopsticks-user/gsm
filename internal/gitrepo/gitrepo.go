// Package gitrepo is a thin wrapper over the git CLI.
package gitrepo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct{ Dir string }

func (r Repo) git(args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", r.Dir, "--no-pager"}, args...)...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Init creates the repo if it doesn't exist yet.
func Init(dir string) (Repo, error) {
	r := Repo{dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return r, nil
	}
	_, err := r.git("init", "-q")
	return r, err
}

// identity returns -c flags with a fallback author when the user has none,
// so commits never fail on a fresh machine.
func (r Repo) identity() []string {
	var flags []string
	if out, _ := r.git("config", "user.name"); strings.TrimSpace(out) == "" {
		flags = append(flags, "-c", "user.name=gsm")
	}
	if out, _ := r.git("config", "user.email"); strings.TrimSpace(out) == "" {
		flags = append(flags, "-c", "user.email=gsm@localhost")
	}
	return flags
}

// CommitAll commits every change, if there is any.
func (r Repo) CommitAll(msg string) (committed bool, err error) {
	if _, err := r.git("add", "-A"); err != nil {
		return false, err
	}
	status, err := r.git("status", "--porcelain")
	if err != nil || strings.TrimSpace(status) == "" {
		return false, err
	}
	_, err = r.git(append(r.identity(), "commit", "-q", "-m", msg)...)
	return err == nil, err
}

// Head returns the short hash of HEAD, or "" before the first commit.
func (r Repo) Head() string {
	out, err := r.git("rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Diff shows uncommitted changes, including new files.
func (r Repo) Diff() (string, error) {
	// --intent-to-add makes new files visible to diff without staging content.
	if _, err := r.git("add", "-A", "--intent-to-add"); err != nil {
		return "", err
	}
	return r.git("diff", "HEAD")
}

func (r Repo) Log() (string, error) {
	return r.git("log", "--date=format:%Y-%m-%d %H:%M", "--format=%h  %ad  %s")
}
