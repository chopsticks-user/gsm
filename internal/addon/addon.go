// Package addon runs companion processes tied to an instance's lifecycle.
// The only addon so far is playit, the playit.gg tunnel as a podman container.
package addon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const Playit = "playit"

const playitImage = "ghcr.io/playit-cloud/playit-agent:latest"

func Names() []string { return []string{Playit} }

func Check(name string) error {
	if name != Playit {
		return fmt.Errorf("unknown addon %q (available: %s)", name, strings.Join(Names(), ", "))
	}
	return nil
}

func container(instance string) string { return "gsm-" + instance + "-playit" }

// PlayitEnvFile holds SECRET_KEY=...; it is a secret and never leaves the machine.
func PlayitEnvFile() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".config", "playit", "env"), err
}

// Problems lists what is missing for the addon to run.
func Problems(name string) []string {
	var p []string
	if _, err := exec.LookPath("podman"); err != nil {
		p = append(p, "podman not found (needed by the playit addon)")
	}
	if env, err := PlayitEnvFile(); err != nil {
		p = append(p, err.Error())
	} else if _, err := os.Stat(env); err != nil {
		p = append(p, fmt.Sprintf("%s missing (should contain SECRET_KEY=...)", env))
	}
	return p
}

func Running(name, instance string) bool {
	return exec.Command("podman", "container", "exists", container(instance)).Run() == nil
}

func Start(name, instance string) error {
	if p := Problems(name); len(p) > 0 {
		return errors.New(strings.Join(p, "; "))
	}
	if Running(name, instance) {
		return nil
	}
	env, _ := PlayitEnvFile()
	out, err := exec.Command("podman", "run", "-d", "--rm", "--name", container(instance),
		"--network=host", "--env-file", env, playitImage).CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman run: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Stop(name, instance string) error {
	if !Running(name, instance) {
		return nil
	}
	out, err := exec.Command("podman", "stop", container(instance)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman stop: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
