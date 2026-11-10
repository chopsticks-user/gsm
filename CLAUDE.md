# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`gsm` is a cross-platform (Linux, Windows, macOS) CLI for managing self-hosted game servers, with multiple server instances per game, git-tracked configs and versioned save snapshots. A working subset exists (Palworld only: lifecycle, config history, snapshots, playit addon); `docs/tasks.md` tracks the rest. `scripts/palworld.sh` is the NixOS-only Bash script gsm grew out of.

## Agreed Design Decisions

- **Language:** Go. The output is a single binary named `gsm`, which is the only public interface.
- **Game support:** compile-time Go values in `internal/game`. Adding a game means adding a value and rebuilding. There are no runtime manifests and no `games/` folder.
- **Supervision:** no daemon. Servers run as detached processes. Each instance has a state file holding the PID, the process start time and the instance ID, and gsm checks all three before treating a PID as that instance's server. Never identify a server by binary name alone (like `scripts/palworld.sh` does with `pgrep -f`), because two instances of the same game share a binary. The server runs in its own process group, which gsm signals as a whole; a surviving group counts as running even if the launched process exited (steam-run does this), since the OS never reuses a PID that still names a live group.
- **Versioning:** configs (text) and `instance.toml` live in a per-instance git repo (the instance directory, whose `.gitignore` tracks only `config/` and `instance.toml`), managed by calling the `git` CLI. Saves (large binaries) go to a shared content-addressed, deduplicated, compressed snapshot store, never into git.
- **Backup:** built-in remotes (local path, SFTP, S3-compatible) hold the off-machine copy of the snapshot store, config repos and instance definitions. gsm pushes automatically after each snapshot and encrypts data sent to cloud targets. `gsm recover` rebuilds every instance on a fresh machine. Secrets are never uploaded.
- **Dependencies:** approved modules are `github.com/BurntSushi/toml` (instance.toml) and `github.com/klauspost/compress` (zstd). Ask before adding any other Go module.

## Core Concepts

- **Game:** a Go value describing the Steam app ID, supported platforms, the launch command per OS, an args template, default ports, config and save paths inside the install, the stop signal and timeout, and an optional save-flush method.
- **Instance:** one named server of a game. Names are unique across all games, so commands take just the name (`gsm start main`).
- **Addon:** a companion process tied to an instance's lifecycle, such as the playit tunnel container.

## Planned CLI Surface

```
gsm game list | show <game>
gsm create <game> <name> [--port N] [--set k=v]   gsm list   gsm remove <name>   gsm import <game> <name> --config DIR --saves DIR
gsm start|stop <name...>|--all   gsm restart|run <name>   gsm status [--json]   gsm logs <name> [-f]   gsm update <name>|--all
gsm config edit|diff|commit|log|restore|remote <name> ...
gsm save snapshot|list|show|diff|restore|prune|pin|tag|annotate|verify <name> ...
gsm remote add|remove|push   
gsm recover
gsm addon add|remove <name> <addon>
gsm doctor
```

Behavior rules:

- `start` auto-commits any uncommitted config changes, so every run maps to an exact config revision.
- Automatic snapshots are taken on `stop`, before `update` and before `restore`. `save restore` and `update` require the instance to be stopped.
- A snapshot taken while the server is running needs the game's save-flush method; without one, gsm warns.
- When a port is already used by another instance, gsm assigns the next free one.
- Restore is non-destructive: it takes a safety snapshot, writes into `saves.restore/`, checks hashes, then swaps directories. `--into <new-instance>` forks an instance from a snapshot instead. History is a single line per instance, with no branches.

## Snapshot Store

```
store/
  objects/<2-hex>/<sha256>          zstd-compressed file contents, keyed by the hash of the raw bytes
  snapshots/<instance>/<timestamp>-<id>.json
  lock                              only one gsm process writes at a time
```

A snapshot record holds its ID, instance, game, creation time, message, trigger (`manual`, `stop`, `update` or `restore`), `config_rev`, `server_build`, tags, a pinned flag, and a file list (path, size, mode, hash). Deduplication works on whole files, not chunks, because saves like Palworld's `Level.sav` are already compressed and get fully rewritten on every save.

Writes are crash-safe. Objects go to a temp file and are renamed into place, and the record is written last, so a snapshot only exists once its record does. Pruning applies the retention policy to records, then a cleanup pass deletes objects no remaining snapshot refers to.

## Data Layout

`$GSM_HOME` defaults to `$XDG_DATA_HOME/gsm` (falling back to `~/.local/share/gsm`) on Linux, `~/Library/Application Support/gsm` on macOS and `%LOCALAPPDATA%\gsm` on Windows. `%APPDATA%` must not be used: it's the roaming profile, which can sync between machines, and server installs are too large for it. `--home` overrides it.

```
instances/<name>/       git repo tracking only config/ and instance.toml
  instance.toml         game, ports, settings, addons, snapshot retention
  config/               linked into the server's config path
  saves/                live saves, linked into the server's save path
  server/               steamcmd install; disposable
  logs/server.log
  state.json            PID, start time, instance name while running
store/                  shared snapshot store
```

Invariant carried over from `scripts/palworld.sh`: user data never lives inside `server/`. It is linked in, with symlinks on Unix and directory junctions on Windows, so a reinstall or a steamcmd `validate` can't destroy it.

## Platform Notes

- **Linux:** detach into a new session (`setsid`). On NixOS, Steam binaries must be wrapped with `steam-run`, and the server needs `steamclient.so` copied into `~/.steam/sdk64` (often a symlink to the source folder, so copies must skip a file onto itself). `steam-run` gives the server a fresh `/tmp`, so `$GSM_HOME` can't be under `/tmp` there.
- **Windows:** start the server in a new process group with a hidden console; stop sends Ctrl+Break through a short-lived helper copy of gsm (`proc.HandleHelper`), then falls back to `taskkill /T /F`. Untested on real Windows so far.
- **macOS:** few dedicated servers ship macOS binaries; `create` refuses games that don't list the platform.

## Reference: palworld.sh

Usage: `bash scripts/palworld.sh {start|stop|run|status|logs|update}`. Check syntax with `bash -n scripts/palworld.sh`. Paths come from `PALWORLD_SERVER_DIR`, `PALWORLD_DATA_DIR` and `PALWORLD_STATE_DIR`, which are set by the user's Home Manager config (not in this repo).

Logic to port into gsm:

- `link_to`: moves an existing real directory into an empty target, and aborts if both have files.
- steamcmd install of app 2394010.
- `server_stop`: SIGTERM, wait 30s, then SIGKILL.
- The playit tunnel as a podman container (`--network=host`, `--rm`, env file `~/.config/playit/env`), which becomes the `playit` addon.

## Code Layout

```
cmd/gsm/            CLI: dispatch and flag parsing (main.go), commands (cmd_*.go)
internal/home       $GSM_HOME resolution
internal/game       game definitions, args templates
internal/instance   instance.toml, names, ports
internal/fsutil     LinkTo, atomic writes, safe copies (symlink / junction per OS)
internal/proc       detached launch, state file, stop (proc_unix.go / proc_windows.go)
internal/steam      steamcmd, build ID, NixOS steamclient.so setup
internal/gitrepo    git CLI wrapper
internal/store      snapshot store
internal/addon      playit podman addon
```

Tests use the real filesystem via `t.TempDir()`, and fake servers or steamcmd by re-running the test binary from `TestMain` with an env var set (see `internal/proc/proc_test.go`).

## Tooling

Go is not installed globally. Use `nix shell nixpkgs#go` (nixpkgs ships Go 1.26). `git`, `steamcmd`, `steam-run` and `podman` are available on the dev host.

- Build: `nix shell nixpkgs#go -c go build ./cmd/gsm`
- Test: `nix shell nixpkgs#go nixpkgs#gcc -c go test -race ./...` (the race detector needs gcc)
- Cross-compile check: `GOOS=windows go build ./cmd/gsm` and `GOOS=darwin GOARCH=arm64 go build ./cmd/gsm`
