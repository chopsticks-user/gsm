package addon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain doubles as a fake podman when the test binary runs under that
// name (a copy on PATH, see fakePodman): it logs its arguments to
// $GSM_FAKE_PODMAN_LOG and keeps one marker file per running container in
// $GSM_FAKE_PODMAN_DIR.
func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "podman" {
		os.Exit(podman(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func podman(args []string) int {
	f, err := os.OpenFile(os.Getenv("GSM_FAKE_PODMAN_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 2
	}
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	dir := os.Getenv("GSM_FAKE_PODMAN_DIR")
	switch {
	case len(args) == 3 && args[0] == "container" && args[1] == "exists":
		if _, err := os.Stat(filepath.Join(dir, args[2])); err != nil {
			return 1
		}
		return 0
	case len(args) > 0 && args[0] == "run":
		if os.Getenv("GSM_FAKE_PODMAN_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "Error: image pull failed")
			return 125
		}
		for i, a := range args {
			if a == "--name" && i+1 < len(args) {
				os.WriteFile(filepath.Join(dir, args[i+1]), nil, 0o644)
			}
		}
		return 0
	case len(args) == 2 && args[0] == "stop":
		os.Remove(filepath.Join(dir, args[1]))
		return 0
	}
	return 2
}

// fakeHome points HOME at an empty folder and PATH at an empty bin folder,
// so nothing from the machine leaks in, and returns both.
func fakeHome(t *testing.T) (home, bin string) {
	d := t.TempDir()
	home, bin = filepath.Join(d, "home"), filepath.Join(d, "bin")
	for _, p := range []string{home, bin} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	t.Setenv("PATH", bin)
	return home, bin
}

// fakePodman puts a copy of the test binary named podman on PATH and an env
// file in place, and returns the log of podman calls.
func fakePodman(t *testing.T) (log func() []string) {
	home, bin := fakeHome(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	name := "podman"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	env, _ := PlayitEnvFile()
	if err := os.MkdirAll(filepath.Dir(env), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("SECRET_KEY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(env))) != home {
		t.Fatalf("env file %s is not under the fake home %s", env, home)
	}
	logFile := filepath.Join(t.TempDir(), "podman.log")
	t.Setenv("GSM_FAKE_PODMAN_LOG", logFile)
	t.Setenv("GSM_FAKE_PODMAN_DIR", t.TempDir())
	t.Setenv("GORACE", "atexit_sleep_ms=0") // under -race each podman call would sleep 1s at exit
	return func() []string {
		b, _ := os.ReadFile(logFile)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// runs lists the logged podman calls that start with verb.
func runs(log []string, verb string) []string {
	var out []string
	for _, l := range log {
		if strings.HasPrefix(l, verb+" ") {
			out = append(out, l)
		}
	}
	return out
}

func TestCheck(t *testing.T) {
	if err := Check(Playit); err != nil {
		t.Fatal(err)
	}
	err := Check("ngrok")
	if err == nil || !strings.Contains(err.Error(), `unknown addon "ngrok"`) || !strings.Contains(err.Error(), "playit") {
		t.Fatalf("Check(ngrok) = %v, want an error naming playit", err)
	}
}

func TestProblems(t *testing.T) {
	fakeHome(t)
	p := Problems(Playit)
	if len(p) != 2 || !strings.Contains(p[0], "podman not found") || !strings.Contains(p[1], "missing (should contain SECRET_KEY=...)") {
		t.Fatalf("Problems without podman and env file = %q", p)
	}
	fakePodman(t)
	if p := Problems(Playit); len(p) != 0 {
		t.Fatalf("Problems with both = %q", p)
	}
}

func TestStartStop(t *testing.T) {
	log := fakePodman(t)
	env, _ := PlayitEnvFile()
	if err := Start(Playit, "main"); err != nil {
		t.Fatal(err)
	}
	want := "run -d --rm --name gsm-main-playit --network=host --env-file " + env + " ghcr.io/playit-cloud/playit-agent:latest"
	if r := runs(log(), "run"); len(r) != 1 || r[0] != want {
		t.Fatalf("podman run calls = %q, want [%q]", r, want)
	}
	if !Running(Playit, "main") {
		t.Fatal("not running after Start")
	}
	// A running container is left alone.
	if err := Start(Playit, "main"); err != nil {
		t.Fatal(err)
	}
	if r := runs(log(), "run"); len(r) != 1 {
		t.Fatalf("second Start ran podman again: %q", r)
	}
	// Each instance gets its own container.
	if err := Start(Playit, "second"); err != nil {
		t.Fatal(err)
	}
	if r := runs(log(), "run"); len(r) != 2 || !strings.Contains(r[1], "--name gsm-second-playit ") {
		t.Fatalf("podman run calls = %q, want one for gsm-second-playit", r)
	}
	if err := Stop(Playit, "main"); err != nil {
		t.Fatal(err)
	}
	if s := runs(log(), "stop"); len(s) != 1 || s[0] != "stop gsm-main-playit" {
		t.Fatalf("podman stop calls = %q", s)
	}
	if Running(Playit, "main") || !Running(Playit, "second") {
		t.Fatal("Stop should stop main's container only")
	}
	// No container, no podman stop.
	if err := Stop(Playit, "main"); err != nil {
		t.Fatal(err)
	}
	if s := runs(log(), "stop"); len(s) != 1 {
		t.Fatalf("Stop without a container ran podman stop: %q", s)
	}
}

func TestStartReportsPodmanFailure(t *testing.T) {
	fakePodman(t)
	t.Setenv("GSM_FAKE_PODMAN_FAIL", "1")
	err := Start(Playit, "main")
	if err == nil || !strings.Contains(err.Error(), "podman run") || !strings.Contains(err.Error(), "image pull failed") {
		t.Fatalf("Start with a failing podman = %v, want podman's output", err)
	}
}
