package steam

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain doubles as a fake steamcmd when GSM_FAKE_STEAMCMD is set: it
// writes an app manifest into the +force_install_dir directory.
func TestMain(m *testing.M) {
	if mode := os.Getenv("GSM_FAKE_STEAMCMD"); mode != "" {
		if p := os.Getenv("GSM_FAKE_STEAMCMD_RUNS"); p != "" { // one line per run
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			f.WriteString("run\n")
			f.Close()
		}
		if p := os.Getenv("GSM_FAKE_STEAMCMD_ARGS"); p != "" {
			os.WriteFile(p, []byte(strings.Join(os.Args[1:], " ")), 0o644)
		}
		// "flaky" fails its first run, like a fresh steamcmd's "Missing configuration".
		if mode == "flaky" {
			marker := os.Getenv("GSM_FAKE_STEAMCMD_MARKER")
			if _, err := os.Stat(marker); err != nil {
				os.WriteFile(marker, nil, 0o644)
				os.Exit(8)
			}
			mode = "ok"
		}
		if mode == "ok" {
			for i, a := range os.Args {
				if a == "+force_install_dir" {
					dir := filepath.Join(os.Args[i+1], "steamapps")
					os.MkdirAll(dir, 0o755)
					os.WriteFile(filepath.Join(dir, "appmanifest_42.acf"), []byte(manifest), 0o644)
				}
			}
		}
		os.Exit(0) // "broken" exits 0 without installing, like steamcmd sometimes does
	}
	os.Exit(m.Run())
}

const manifest = `"AppState"
{
	"appid"		"42"
	"buildid"		"18519483"
	"installdir"		"x"
}
`

func TestInstall(t *testing.T) {
	Command = os.Args[0]
	dir := t.TempDir()

	t.Setenv("GSM_FAKE_STEAMCMD", "broken")
	if err := Install(dir, "42", "", io.Discard); err == nil {
		t.Fatal("want error when steamcmd exits 0 without installing")
	}

	t.Setenv("GSM_FAKE_STEAMCMD", "ok")
	if err := Install(dir, "42", "", io.Discard); err != nil {
		t.Fatal(err)
	}
	if id, err := BuildID(dir, "42"); err != nil || id != "18519483" {
		t.Fatalf("BuildID = %q, %v", id, err)
	}
}

// A forced platform comes first, before steamcmd reads the install dir.
func TestInstallPlatform(t *testing.T) {
	Command = os.Args[0]
	dir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("GSM_FAKE_STEAMCMD", "ok")
	t.Setenv("GSM_FAKE_STEAMCMD_ARGS", argsFile)
	for platform, want := range map[string]string{
		"windows": "+@sSteamCmdForcePlatformType windows +force_install_dir " + dir + " +login anonymous +app_update 42 validate +quit",
		"":        "+force_install_dir " + dir + " +login anonymous +app_update 42 validate +quit",
	} {
		if err := Install(dir, "42", platform, io.Discard); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(argsFile); string(b) != want {
			t.Errorf("platform %q: args %q, want %q", platform, b, want)
		}
	}
}

// A failed first attempt is retried once; the retry's result counts.
func TestInstallRetriesOnce(t *testing.T) {
	Command = os.Args[0]
	dir := t.TempDir()
	t.Setenv("GSM_FAKE_STEAMCMD", "flaky")
	t.Setenv("GSM_FAKE_STEAMCMD_MARKER", filepath.Join(t.TempDir(), "ran"))
	var out strings.Builder
	if err := Install(dir, "42", "", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "trying once more") {
		t.Fatalf("no retry message in %q", out.String())
	}
}

// A steamcmd that keeps failing runs exactly twice, and the error is returned;
// a missing steamcmd isn't retried at all.
func TestInstallGivesUpAfterOneRetry(t *testing.T) {
	Command = os.Args[0]
	runs := filepath.Join(t.TempDir(), "runs")
	t.Setenv("GSM_FAKE_STEAMCMD", "broken")
	t.Setenv("GSM_FAKE_STEAMCMD_RUNS", runs)
	if err := Install(t.TempDir(), "42", "", io.Discard); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(runs); strings.Count(string(b), "run") != 2 {
		t.Fatalf("steamcmd ran %q, want twice", b)
	}
	Command = "gsm-test-no-such-steamcmd" // not on PATH, like a host without steamcmd
	var out strings.Builder
	if err := Install(t.TempDir(), "42", "", &out); err == nil || strings.Contains(out.String(), "trying once more") {
		t.Fatalf("missing steamcmd: err %v, output %q", err, out.String())
	}
}

// steamclient.so is looked up in Steam's own folder first, then
// ~/.steam/steam, then steamcmd's; an empty file doesn't count.
func TestFindSteamclient(t *testing.T) {
	home := t.TempDir()
	if _, err := FindSteamclient(home); err == nil || !strings.Contains(err.Error(), "run steamcmd once") {
		t.Fatalf("no steamclient.so: err = %v", err)
	}
	put := func(content string, parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	steamcmd := put("lib", ".steam", "steamcmd", "linux64", "steamclient.so")
	put("", ".local", "share", "Steam", "linux64", "steamclient.so") // empty: skipped
	if got, err := FindSteamclient(home); err != nil || got != steamcmd {
		t.Fatalf("got %q, %v; want steamcmd's %q", got, err, steamcmd)
	}
	steam := put("lib", ".steam", "steam", "linux64", "steamclient.so")
	if got, _ := FindSteamclient(home); got != steam {
		t.Fatalf("got %q, want ~/.steam/steam's %q", got, steam)
	}
	first := put("lib", ".local", "share", "Steam", "linux64", "steamclient.so")
	if got, _ := FindSteamclient(home); got != first {
		t.Fatalf("got %q, want Steam's own %q", got, first)
	}
}

func TestSetupNixOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if err := SetupNixOS(); err == nil {
		t.Fatal("SetupNixOS without steamclient.so succeeded")
	}
	src := filepath.Join(home, ".local", "share", "Steam", "linux64", "steamclient.so")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("steam's library"), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a second setup is a no-op copy, not an error
		if err := SetupNixOS(); err != nil {
			t.Fatal(err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".steam", "sdk64", "steamclient.so")); err != nil || string(b) != "steam's library" {
		t.Fatalf("sdk64 copy = %q, %v", b, err)
	}
}
