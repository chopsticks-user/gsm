//go:build linux

package harness

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// World is the folder under the saves dir where the fake server saves.
const World = "0/TESTWORLD"

// fakeSteamcmd handles "+force_install_dir DIR ... +app_update ID validate"
// by laying out a Palworld-shaped Linux install whose build is
// $GSM_TEST_BUILD (default 100).
func fakeSteamcmd(args []string) int {
	var dir string
	for i, a := range args {
		if a == "+force_install_dir" && i+1 < len(args) {
			dir = args[i+1]
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "fake steamcmd: no +force_install_dir")
		return 1
	}
	build := os.Getenv("GSM_TEST_BUILD")
	if build == "" {
		build = "100"
	}
	exe, _ := os.Executable()
	files := map[string]string{
		"steamapps/appmanifest_2394010.acf": fmt.Sprintf("\"AppState\"\n{\n\t\"buildid\"\t\t\"%s\"\n}\n", build),
		// Like the real PalServer.sh, it runs the server as a child, not exec.
		"PalServer.sh":                "#!/bin/sh\nGSM_TEST_ROLE=server \"" + exe + "\" \"$@\"\n",
		"DefaultPalWorldSettings.ini": "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Default\")\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	fmt.Println("Success! App '2394010' fully installed.")
	return 0
}

// fakeServer rewrites a save every 100 ms through the linked saves folder and
// writes a final save when asked to stop, like a game saving on shutdown.
func fakeServer() int {
	fmt.Println("Running fake server with", strings.Join(os.Args[1:], " "))
	save := filepath.Join("Pal", "Saved", "SaveGames", World, "Level.sav")
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	// A watchdog, in case the test dies without its cleanup (a -timeout panic).
	giveUp := time.After(2 * time.Minute)
	for n := 0; ; n++ {
		select {
		case <-giveUp:
			return 1
		case <-stop:
			os.WriteFile(save, []byte(fmt.Sprintf("final save after %d ticks\n", n)), 0o644)
			return 0
		case <-time.After(100 * time.Millisecond):
			os.WriteFile(save, []byte(fmt.Sprintf("tick %d\n", n)), 0o644)
		}
	}
}
