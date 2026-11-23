//go:build linux

// Package harness runs gsm the way a user does, for the suites under tests/.
// It builds the gsm binary from cmd/gsm once per test run and gives each test
// a throwaway home with fakes for everything outside gsm. The test binary
// plays the fakes, picked by GSM_TEST_ROLE (see fakes.go), and tiny shell
// shims named steamcmd, steam-run and podman put them first on PATH, so gsm
// runs exactly as in production. The shims need a POSIX shell and the fake
// install is Palworld's Linux one, hence linux.
package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const gsmPkg = "github.com/chopsticks-user/gsm/cmd/gsm"

// Main is a suite's TestMain: it plays a fake when the test binary was
// started as one, and otherwise runs the tests.
func Main(m *testing.M) {
	switch os.Getenv("GSM_TEST_ROLE") {
	case "steamcmd":
		os.Exit(fakeSteamcmd(os.Args[1:]))
	case "server":
		os.Exit(fakeServer())
	}
	code := m.Run()
	if binDir != "" {
		os.RemoveAll(binDir)
	}
	os.Exit(code)
}

var (
	buildOnce sync.Once
	binDir    string
	gsmBin    string
	buildErr  error
)

// gsm returns the gsm binary, building it on first use. The build runs inside
// a test rather than in Main because go test records the files a test reads,
// to tell when a cached result is stale, only while tests run.
func gsm(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() { gsmBin, buildErr = build() })
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return gsmBin
}

func build() (string, error) {
	// Before building, so an edit made during the build reruns the suite next
	// time instead of leaving a cached pass for code the binary lacks.
	if err := readSources(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "gsm-test-bin-")
	if err != nil {
		return "", err
	}
	binDir = dir
	bin := filepath.Join(dir, "gsm")
	args := []string{"build", "-o", bin}
	if raceEnabled { // so the processes the tests start keep the race detector
		args = append(args, "-race")
	}
	if out, err := exec.Command("go", append(args, gsmPkg)...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("building gsm: %v\n%s", err, out)
	}
	return bin, nil
}

// readSources reads every file the gsm build reads. go test caches a pass by
// the test binary and the files and variables the test read; the suites don't
// import cmd/gsm, so without this a cached pass would hide a change to gsm.
func readSources() error {
	for _, k := range []string{"GOFLAGS", "CGO_ENABLED", "GOEXPERIMENT"} {
		os.Getenv(k) // read only so go test records it: the gsm build uses it too
	}
	out, err := exec.Command("go", "list", "-deps", "-json=Dir,GoFiles,CgoFiles,EmbedFiles,Module", gsmPkg).Output()
	if err != nil {
		return fmt.Errorf("listing gsm's sources: %w", err)
	}
	var files []string
	gomod := ""
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var p struct {
			Dir                           string
			GoFiles, CgoFiles, EmbedFiles []string
			Module                        *struct {
				Main  bool
				GoMod string
			}
		}
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("listing gsm's sources: %w", err)
		}
		if p.Module == nil || !p.Module.Main { // the standard library and modules are covered by go.sum and the Go version
			continue
		}
		for _, f := range append(append(p.GoFiles, p.CgoFiles...), p.EmbedFiles...) {
			files = append(files, filepath.Join(p.Dir, f))
		}
		gomod = p.Module.GoMod
	}
	if gomod != "" {
		files = append(files, gomod, filepath.Join(filepath.Dir(gomod), "go.sum"))
	}
	for _, f := range files {
		if _, err := os.ReadFile(f); err != nil {
			return err
		}
	}
	return nil
}

// Home is a throwaway $GSM_HOME, with its own $HOME and the fakes first on
// PATH, that runs the built gsm.
type Home struct {
	t   *testing.T
	gsm string
	Dir string // $GSM_HOME
	env []string
}

// New makes a Home for t and stops every server it started when t ends, even
// when t fails.
func New(t *testing.T) *Home {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	bin := gsm(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// On NixOS gsm refuses a home under /tmp (steam-run hides /tmp from the
	// server), where temp dirs usually are, so use the user's cache dir there.
	parent := os.TempDir()
	if _, err := os.Stat("/etc/NIXOS"); err == nil {
		if parent, err = os.UserCacheDir(); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.MkdirTemp(parent, "gsm-test-")
	if errors.Is(err, fs.ErrNotExist) {
		// Only when missing: MkdirAll stats the folder, and go test would key
		// the cached result on its mtime, which every run changes.
		if err = os.MkdirAll(parent, 0o755); err == nil {
			root, err = os.MkdirTemp(parent, "gsm-test-")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shimDir := filepath.Join(root, "bin")
	fakeHome := filepath.Join(root, "home")
	shims := map[string]string{
		"steamcmd":  "#!/bin/sh\nGSM_TEST_ROLE=steamcmd exec \"" + exe + "\" \"$@\"\n",
		"steam-run": "#!/bin/sh\nexec \"$@\"\n",
		"podman":    "#!/bin/sh\nexit 1\n", // no container ever exists or starts
	}
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range shims {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A non-empty steamclient.so where gsm looks for it on NixOS.
	so := filepath.Join(fakeHome, ".local", "share", "Steam", "linux64", "steamclient.so")
	if err := os.MkdirAll(filepath.Dir(so), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(so, []byte("fake library"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Home{t: t, gsm: bin, Dir: filepath.Join(root, "gsm")}
	for _, kv := range os.Environ() {
		// GIT_* (set inside git hooks) would point gsm's git calls at this
		// repo; XDG config could bring in the developer's git config.
		k, _, _ := strings.Cut(kv, "=")
		if k == "PATH" || k == "HOME" || k == "GSM_HOME" || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "GIT_") {
			continue
		}
		h.env = append(h.env, kv)
	}
	// Under -race every gsm process would sleep 1s at exit (GORACE atexit_sleep_ms).
	h.env = append(h.env, "GORACE=atexit_sleep_ms=0", "HOME="+fakeHome, "PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		if out, err := h.Run(nil, "stop", "--all"); err != nil {
			t.Errorf("cleanup: gsm stop --all: %v\n%s", err, out)
		}
	})
	return h
}

// Run runs gsm with args and extra environment, returning stdout and stderr.
func (h *Home) Run(extraEnv []string, args ...string) (string, error) {
	c := exec.Command(h.gsm, append([]string{"--home", h.Dir}, args...)...)
	c.Env = append(append([]string{}, h.env...), extraEnv...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// OK runs gsm and fails the test if it fails.
func (h *Home) OK(args ...string) string {
	h.t.Helper()
	out, err := h.Run(nil, args...)
	if err != nil {
		h.t.Fatalf("gsm %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// Fails runs gsm and fails the test if it succeeds.
func (h *Home) Fails(args ...string) string {
	h.t.Helper()
	out, err := h.Run(nil, args...)
	if err == nil {
		h.t.Fatalf("gsm %s succeeded, want an error\n%s", strings.Join(args, " "), out)
	}
	return out
}

// Inst is a path inside the instance name's folder.
func (h *Home) Inst(name string, parts ...string) string {
	return filepath.Join(append([]string{h.Dir, "instances", name}, parts...)...)
}

// StatusRow is one entry of gsm status --json.
type StatusRow struct {
	Name    string   `json:"name"`
	Game    string   `json:"game"`
	Port    int      `json:"port"`
	Running bool     `json:"running"`
	PID     int      `json:"pid"`
	Addons  []string `json:"addons"`
}

// Status returns gsm status --json by instance name.
func (h *Home) Status() map[string]StatusRow {
	h.t.Helper()
	var rows []StatusRow
	dec := json.NewDecoder(strings.NewReader(h.OK("status", "--json")))
	dec.DisallowUnknownFields() // a renamed field in gsm's output fails here, not silently
	if err := dec.Decode(&rows); err != nil {
		h.t.Fatal(err)
	}
	m := map[string]StatusRow{}
	for _, r := range rows {
		m[r.Name] = r
	}
	return m
}

// SnapshotID returns the ID of the newest snapshot of name with trigger.
func (h *Home) SnapshotID(name, trigger string) string {
	h.t.Helper()
	id := ""
	for _, line := range strings.Split(h.OK("save", "list", name), "\n") {
		if f := strings.Fields(line); len(f) > 3 && f[3] == trigger {
			id = f[0]
		}
	}
	if id == "" {
		h.t.Fatalf("no %s snapshot of %s", trigger, name)
	}
	return id
}

// MustContain fails the test unless out contains every want.
func MustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("output lacks %q:\n%s", w, out)
		}
	}
}

// ReadFile returns p's content, failing the test if it can't be read.
func ReadFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// WaitFor polls cond for up to 10 seconds.
func WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
