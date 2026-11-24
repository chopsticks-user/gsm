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

// World is the folder under the saves dir where the fake Palworld server
// saves; EnshroudedSave is the file the fake Enshrouded server saves.
const (
	World          = "0/TESTWORLD"
	EnshroudedSave = "3ad85aea"
)

// fakeSteamcmd handles "[+@sSteamCmdForcePlatformType P] +force_install_dir
// DIR ... +app_update ID validate" like steamcmd: it lays out a
// Palworld-shaped Linux install or an Enshrouded-shaped Windows one, whose
// build is $GSM_TEST_BUILD (default 100). It refuses the wrong platform for
// either game, so a gsm that asks for the wrong build fails the test. With
// $GSM_TEST_CLOBBER set it first replaces Palworld's saves link with a real
// folder holding a stock save, as a validate restoring stock files would.
func fakeSteamcmd(args []string) int {
	var dir, platform, app string
	for i, a := range args {
		if i+1 >= len(args) {
			break
		}
		switch a {
		case "+force_install_dir":
			dir = args[i+1]
		case "+@sSteamCmdForcePlatformType":
			platform = args[i+1]
		case "+app_update":
			app = args[i+1]
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
	manifest := fmt.Sprintf("\"AppState\"\n{\n\t\"buildid\"\t\t\"%s\"\n}\n", build)
	var files map[string]string
	switch {
	case app == "2394010" && platform == "":
		if os.Getenv("GSM_TEST_CLOBBER") != "" {
			// Fail loudly rather than install normally, so a test can't
			// blame gsm for a clobber that never happened.
			saves := filepath.Join(dir, "Pal", "Saved", "SaveGames")
			stock := filepath.Join(saves, "0", "STOCK", "Level.sav")
			fi, err := os.Lstat(saves)
			if err == nil && fi.Mode()&os.ModeSymlink == 0 {
				err = fmt.Errorf("%s is not a link", saves)
			}
			if err == nil {
				err = os.Remove(saves)
			}
			if err == nil {
				err = os.MkdirAll(filepath.Dir(stock), 0o755)
			}
			if err == nil {
				err = os.WriteFile(stock, []byte("stock save"), 0o644)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "fake steamcmd: can't clobber the saves link:", err)
				return 1
			}
		}
		files = map[string]string{
			"steamapps/appmanifest_2394010.acf": manifest,
			// Like the real PalServer.sh, it runs the server as a child, not exec.
			"PalServer.sh":                "#!/bin/sh\nGSM_TEST_ROLE=server \"" + exe + "\" \"$@\"\n",
			"DefaultPalWorldSettings.ini": "[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Default\")\n",
		}
	case app == "2278520" && platform == "windows":
		files = map[string]string{
			"steamapps/appmanifest_2278520.acf": manifest,
			"enshrouded_server.exe":             "MZ fake Windows server", // run through the wine shim
		}
	default:
		fmt.Fprintf(os.Stderr, "fake steamcmd: no build of app %q for platform %q\n", app, platform)
		return 1
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
	fmt.Printf("Success! App '%s' fully installed.\n", app)
	return 0
}

// fakeServer rewrites a save every 100 ms through the linked saves folder and
// writes a final save when asked to stop, like a game saving on shutdown.
// Started by the wine shim it plays Enshrouded under Wine: it prints its
// WINEPREFIX and the config it reads through the install's link, and only
// SIGINT shuts it down with a save; SIGTERM ends it without one.
// $GSM_TEST_CRASH makes it fail at once; $GSM_TEST_EXIT_AFTER=N makes it
// save and exit by itself after N ticks.
func fakeServer() int {
	fmt.Println("Running fake server with", strings.Join(os.Args[1:], " "))
	if os.Getenv("GSM_TEST_CRASH") != "" {
		fmt.Fprintln(os.Stderr, "fake server: fatal error: cannot bind the port")
		return 3
	}
	save := filepath.Join("Pal", "Saved", "SaveGames", World, "Level.sav")
	stop := make(chan os.Signal, 1)
	term := make(chan os.Signal, 1) // stays empty unless Enshrouded
	if os.Getenv("GSM_TEST_GAME") == "enshrouded" {
		fmt.Println("WINEPREFIX=" + os.Getenv("WINEPREFIX"))
		cfg, err := os.ReadFile("enshrouded_server.json")
		fmt.Printf("config: %s (err %v)\n", cfg, err)
		save = filepath.Join("savegame", EnshroudedSave)
		signal.Notify(stop, os.Interrupt)
		signal.Notify(term, syscall.SIGTERM)
	} else {
		signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	}
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	exitAfter := -1
	fmt.Sscan(os.Getenv("GSM_TEST_EXIT_AFTER"), &exitAfter)
	// A watchdog, in case the test dies without its cleanup (a -timeout panic).
	giveUp := time.After(2 * time.Minute)
	for n := 0; ; n++ {
		select {
		case <-giveUp:
			return 1
		case <-term:
			fmt.Println("fake server: killed by SIGTERM without saving")
			return 1
		case <-stop:
			os.WriteFile(save, []byte(fmt.Sprintf("final save after %d ticks\n", n)), 0o644)
			return 0
		case <-time.After(100 * time.Millisecond):
			if n == exitAfter {
				os.WriteFile(save, []byte(fmt.Sprintf("shutdown save after %d ticks\n", n)), 0o644)
				fmt.Println("fake server: shutting down by itself")
				return 0
			}
			os.WriteFile(save, []byte(fmt.Sprintf("tick %d\n", n)), 0o644)
		}
	}
}
