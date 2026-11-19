// Package steam installs and updates servers with steamcmd.
package steam

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/chopsticks-user/gsm/internal/fsutil"
)

// Command is the steamcmd executable; tests point it at a fake.
var Command = "steamcmd"

// Install installs or updates appID into dir (absolute) and validates files.
func Install(dir, appID string, out io.Writer) error {
	c := exec.Command(Command, "+force_install_dir", dir, "+login", "anonymous",
		"+app_update", appID, "validate", "+quit")
	c.Stdout, c.Stderr = out, out
	if err := c.Run(); err != nil {
		return fmt.Errorf("steamcmd: %w", err)
	}
	// steamcmd's exit code is unreliable, so check the result too.
	if _, err := BuildID(dir, appID); err != nil {
		return fmt.Errorf("steamcmd finished but the install is incomplete: %w", err)
	}
	return nil
}

var buildIDLine = regexp.MustCompile(`^\s*"buildid"\s+"(\d+)"`)

// BuildID reads the installed build from steamapps/appmanifest_<appID>.acf.
func BuildID(dir, appID string) (string, error) {
	f, err := os.Open(filepath.Join(dir, "steamapps", "appmanifest_"+appID+".acf"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if m := buildIDLine.FindStringSubmatch(s.Text()); m != nil {
			return m[1], nil
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no buildid in app manifest")
}

func IsNixOS() bool {
	_, err := os.Stat("/etc/NIXOS")
	return err == nil
}

// SetupNixOS copies steamclient.so into ~/.steam/sdk64, where Steam servers
// look for it. On NixOS it isn't there unless Steam put it there.
func SetupNixOS() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	src, err := FindSteamclient(home)
	if err != nil {
		return err
	}
	return fsutil.CopyFile(src, filepath.Join(home, ".steam", "sdk64", "steamclient.so"))
}

// FindSteamclient returns the first 64-bit steamclient.so from Steam or steamcmd.
func FindSteamclient(home string) (string, error) {
	for _, p := range []string{
		filepath.Join(home, ".local", "share", "Steam", "linux64", "steamclient.so"),
		filepath.Join(home, ".steam", "steam", "linux64", "steamclient.so"),
		filepath.Join(home, ".steam", "steamcmd", "linux64", "steamclient.so"),
	} {
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			return p, nil
		}
	}
	return "", errors.New("steamclient.so not found; run steamcmd once to download it")
}
