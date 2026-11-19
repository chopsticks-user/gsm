// Package home resolves the gsm data root ($GSM_HOME).
package home

import (
	"errors"
	"path/filepath"
)

// Resolve returns the data root. override (from --home) wins, then
// $GSM_HOME, then the per-OS default. getenv and userHome are passed in so
// every OS branch can be tested on any machine.
func Resolve(override, goos string, getenv func(string) string, userHome string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if d := getenv("GSM_HOME"); d != "" {
		return filepath.Abs(d)
	}
	switch goos {
	case "windows":
		// LOCALAPPDATA, never APPDATA: the roaming profile must not sync server installs.
		if d := getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "gsm"), nil
		}
		return "", errors.New("LOCALAPPDATA is not set")
	case "darwin":
		if userHome == "" {
			return "", errors.New("home directory unknown")
		}
		return filepath.Join(userHome, "Library", "Application Support", "gsm"), nil
	}
	if d := getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "gsm"), nil
	}
	if userHome == "" {
		return "", errors.New("home directory unknown")
	}
	return filepath.Join(userHome, ".local", "share", "gsm"), nil
}
