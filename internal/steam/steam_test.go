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
		if p := os.Getenv("GSM_FAKE_STEAMCMD_ARGS"); p != "" {
			os.WriteFile(p, []byte(strings.Join(os.Args[1:], " ")), 0o644)
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
