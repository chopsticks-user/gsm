# gsm guide

This guide follows one user from an empty machine to two running Palworld servers, with snapshots, a restore and an update, showing each command's output and what it does. For how each step works underneath, follow the links to [architecture.md](architecture.md).

gsm currently supports Palworld (Linux, Windows) and Enshrouded (Linux through Wine, Windows; see [Enshrouded](#enshrouded)). It implements `doctor`, `game`, `create`, `list`, `start`, `stop`, `restart`, `run`, `status`, `logs`, `update`, `config diff|log|commit`, `save snapshot|list|restore`, `version` and the `playit` addon. What's planned is in [roadmap.md](roadmap.md).

## Step 0: Get gsm

gsm is one static binary with no runtime to install.

**From a release.** Download the binary for your OS and CPU from the [releases page](https://github.com/chopsticks-user/gsm/releases), along with `SHA256SUMS`, check it and put it on your `PATH`. On Linux x86-64:

```
$ sha256sum --ignore-missing -c SHA256SUMS
gsm-linux-amd64: OK
$ install -D -m 755 gsm-linux-amd64 ~/.local/bin/gsm
$ gsm version
gsm v0.1.0 (commit 1a2b3c4d5e6f, go1.27.3)
```

The files are `gsm-<os>-<arch>`, for `linux`, `windows` (`.exe`) and `darwin`, each on `amd64` and `arm64`. On macOS, check with `shasum -a 256 --ignore-missing -c SHA256SUMS`; on Windows, compare `(Get-FileHash gsm-windows-amd64.exe).Hash` in PowerShell with the file's line in `SHA256SUMS`.

**From source.** With Go (or Nix, see [development.md](development.md)):

```
go build -o build/ ./cmd/gsm     # build/gsm
```

**Where gsm keeps its data.** Everything lives under one folder, `$GSM_HOME`. The first match wins:

1. `--home DIR`, a global flag given before the command: `gsm --home /srv/gsm status`
2. the `GSM_HOME` environment variable
3. the per-OS default:
   - Linux: `$XDG_DATA_HOME/gsm`, else `~/.local/share/gsm`
   - macOS: `~/Library/Application Support/gsm`
   - Windows: `%LOCALAPPDATA%\gsm` (never the roaming `%APPDATA%`, since server installs are gigabytes)

A server install takes about 5 GB per instance, so pick a disk with room.

## Step 1: Check the machine

```
$ gsm doctor
home     /home/you/.local/share/gsm
ok       git (/run/current-system/sw/bin/git)
ok       steamcmd (/home/you/.nix-profile/bin/steamcmd)
ok       steam-run (/home/you/.nix-profile/bin/steam-run)
ok       steamclient.so (/home/you/.local/share/Steam/linux64/steamclient.so)
```

`doctor` checks for `git` and `steamcmd`. On NixOS it also checks for `steam-run` and `steamclient.so`, which Steam servers need there. Once an instance uses a game run through Wine, it checks for `wine`; once an instance uses an addon, it checks that addon's needs too (podman and `~/.config/playit/env` for playit). It exits non-zero when something is missing.

## Step 2: Pick a game

```
$ gsm game list
GAME      TITLE     PLATFORMS
palworld  Palworld  linux, windows

$ gsm game show palworld
name:          palworld
title:         Palworld
steam app:     2394010
platforms:     linux, windows
default port:  8211
args:          -port={port} -players={players} -useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS
settings:      players (set with --set k=v); port with --port
stop timeout:  30s
```

`settings` lists what an instance can set at `create`. Some games show more: `config file` and `port key` for a server configured by one JSON file (gsm sets the port in it), `runs through: Wine` for a Windows-only server on Linux, and `stop signal` when it isn't SIGTERM. Games are built into gsm ([Games](architecture.md#games)); Palworld ships no macOS server, so `create palworld` refuses on macOS.

## Step 3: Create an instance

```
$ gsm create palworld main --set players=4
installing Palworld with steamcmd (this can take a while)...
  ... steamcmd output ...
created main (Palworld, port 8211)
start it with: gsm start main
```

An instance is one named server. Names are unique across all games and must match `^[a-z0-9][a-z0-9_-]{0,62}$`, so every later command takes just `main`.

`create` installs the server with steamcmd (about 5 GB), copies the game's default settings into `config/`, and links `config/` and `saves/` into the install, so reinstalling or validating the server can never touch your data. It also starts a git repo for the config. If anything fails, it removes the half-made instance. When a second instance asks for a port that's taken, it gets the next free one:

```
$ gsm create palworld test
port 8211 is used by another instance, using 8212
```

The result:

```
$GSM_HOME/instances/main/
  .git/
  .gitignore
  instance.toml
  config/PalWorldSettings.ini
  saves/
  server/                      ~5 GB, disposable
    Pal/Saved/Config/LinuxServer -> $GSM_HOME/instances/main/config
    Pal/Saved/SaveGames          -> $GSM_HOME/instances/main/saves
  logs/
```

Details: [Creating an instance](architecture.md#creating-an-instance), [LinkTo](architecture.md#linkto).

## Step 4: Edit the config

Each game's stock defaults are in [examples/](../examples) in the repo, to read before or after `create`. Edit the files in `config/` directly:

```
$ $EDITOR ~/.local/share/gsm/instances/main/config/PalWorldSettings.ini
$ gsm config diff main
diff --git a/config/PalWorldSettings.ini b/config/PalWorldSettings.ini
-ServerName="Default Palworld Server"
+ServerName="My server"
```

Commit with a message (`gsm config commit main -m "rename server"`) or don't bother: `start` commits for you, so every run maps to an exact config revision. `gsm config log main` shows the history.

Edit `PalWorldSettings.ini` any time, including right after `create`: an edited file survives the first start whole (Palworld only drops its "sample file" comment header). A file nobody edited, though, is empty after the first start. To edit it then, copy `server/DefaultPalWorldSettings.ini` over `config/PalWorldSettings.ini` first; don't do that to a file you already edited, since it replaces your changes. Palworld also writes about 40 near-empty engine `.ini` files into `config/` on first boot; they get committed with the next start and are harmless.

## Step 5: Start the server

```
$ gsm start main test
main: committed config changes
main: started (pid 39692, port 8211, config 681f6e5)
test: started (pid 39715, port 8212, config f7ebf67)
```

gsm commits the config, launches the server detached in its own process group, writes a state file, and exits. The server keeps running without gsm. If the server dies within two seconds, gsm reports it instead of claiming success:

```
main: server exited right after start; see: gsm logs main
```

Starting an instance that's already running prints `already running`, so `start --all` is safe to repeat. Details: [Start](architecture.md#start).

## Step 6: Check status

```
$ gsm status
NAME  GAME      PORT  STATE    PID    ADDONS
main  palworld  8211  running  39692
test  palworld  8212  running  39715

$ gsm status --json
[
  {"name": "main", "game": "palworld", "port": 8211, "running": true, "pid": 39692, "addons": []},
  ...
]
```

gsm tells two instances of the same game apart, and won't mistake a reused PID for your server. Details: [Is it running?](architecture.md#is-it-running)

## Step 7: Follow the log

```
$ gsm logs main -f
=== gsm start 2026-11-19T19:13:39-05:00, config 681f6e5 ===
Game version is v1.0.5.102999
Running Palworld dedicated server on :8211
```

Each start appends a header with the config revision. Ctrl+C ends `-f`.

## Step 8: Take a snapshot

```
$ gsm save snapshot main -m "before boss fight"
warning: main is running and palworld has no save-flush method; the snapshot may miss recent progress
main: snapshot 3735cbf0 (4 files, 33.4 KiB)

$ gsm save list main
ID        CREATED              TRIGGER  FILES  SIZE      CONFIG   BUILD     MESSAGE
3735cbf0  2026-11-19 19:50:01  manual   4      33.4 KiB  681f6e5  25247047  before boss fight
```

Saves don't go into git: they're large binaries rewritten on every save. They go to a shared store that keeps each distinct file once, compressed, so unchanged files cost nothing. Every snapshot records the config revision and server build that produced it.

A snapshot of a running server works, but may miss the last few minutes of progress, hence the warning. Snapshots taken on stop are complete. Details: [Snapshot store](architecture.md#snapshot-store).

## Step 9: Stop the server

```
$ gsm stop main
main: stopping (up to 30s)...
main: stopped
main: snapshot 07c32289 (4 files, 33.4 KiB)
```

gsm asks the server to exit, kills it if it hasn't after the game's timeout (30s for Palworld), and takes an automatic snapshot, so every shutdown leaves a restore point. Other instances are untouched. `stop --all` stops every instance; `restart <name>` is `stop` then `start`. Details: [Stop](architecture.md#stop).

## Step 10: Run in the foreground

```
$ gsm run main
main: running in the foreground (pid 40029), Ctrl+C to stop
Game version is v1.0.5.102999
Running Palworld dedicated server on :8211
^C
main: stopping (up to 30s)...
main: snapshot e975a8e7 (4 files, 33.4 KiB)
```

`run` is `start` with the output in your terminal (and the log). Ctrl+C runs the normal stop, snapshot included. `gsm status` in another terminal still sees it.

## Step 11: Restore a snapshot

```
$ gsm save restore main 3735cbf0
main: safety snapshot 85c597d3
main: restored snapshot 3735cbf0 from 2026-11-19 19:50:01
```

IDs can be shortened to any unique prefix (`gsm save restore main 3735`); an empty or blank ID is an error. Restore requires the server to be stopped. It first snapshots the current saves, so a restore can always be undone by restoring that safety snapshot. It checks every file against the snapshot before touching the live saves, and a crash at any point is repaired on the next restore. Details: [Restore](architecture.md#restore).

## Step 12: Update the server

```
$ gsm update main
main: snapshot bc343959 (4 files, 33.4 KiB)
  ... steamcmd output ...
main: updated build 25247047 -> 25311802
```

`update` requires the server to be stopped. It snapshots first, runs steamcmd with `validate`, checks that the links to your data survived, and reports the old and new build (or `already up to date`). If a game update breaks your world, `save list` shows which build each snapshot came from, so you can restore the last good one.

## Step 13: Add the playit tunnel

```
$ gsm addon add main playit
main: added addon playit
```

[playit.gg](https://playit.gg) makes a server reachable without port forwarding. The addon runs the playit agent as a podman container, started after the server and stopped before it. It needs podman and an env file at `~/.config/playit/env` containing `SECRET_KEY=...` from your playit account. gsm never copies that file and warns if it's missing. `gsm addon remove main playit` turns it off. Details: [Addons](architecture.md#addons).

## Moving an instance to another machine

There is no command for this yet: `gsm recover` ([#14](https://github.com/chopsticks-user/gsm/issues/14)) will replace these steps. Until then, copy the instance by hand. Everything it needs is under `$GSM_HOME` (shown by `gsm doctor`), and the server install and Wine prefix are left behind because `update` rebuilds them. On the old machine, from `$GSM_HOME`:

```
$ gsm stop main
$ tar czf main.tgz --exclude=instances/main/server --exclude=instances/main/wine \
    instances/main store/objects store/snapshots/main
```

That holds the instance definition, the config and its history, the live saves, the logs and every snapshot. `store/objects` is shared by all instances, so it also carries content only other instances use; that costs space but nothing else. On the new machine, from its `$GSM_HOME`:

```
$ tar xzf main.tgz --keep-old-files
$ gsm update main
main: snapshot 3ffa7707 (4 files, 33.4 KiB)
  ... steamcmd output ...
main: updated build none -> 25311802
$ gsm start main
```

Until `update` has run, `start` stops with `server not installed; run: gsm update main`. `update` installs the server and links `config/` and `saves/` into it, and `save list main` shows the old machine's snapshots.

- **An existing `$GSM_HOME`** is fine: snapshot objects are named by their content, so merging them can't clash, and `--keep-old-files` leaves the existing files alone. The new machine must not already have an instance with the same name.
- **Ports** are not checked against the new machine's instances. If another instance there uses the same port, change `port` in `instance.toml` before starting.
- **The playit env file** (`~/.config/playit/env`) is outside `$GSM_HOME`; copy it yourself if the instance uses the addon.
- **Across OSes** snapshots and config work as they are. On Windows, use the built-in `tar` from the `%LOCALAPPDATA%\gsm` folder, and see [Enshrouded](#enshrouded) for the file symlink it needs.

## Enshrouded

Everything above works the same for Enshrouded (`gsm create enshrouded <name>`); these are the differences.

- **Wine on Linux.** Enshrouded ships only a Windows server, so on Linux gsm installs the Windows build and runs it through `wine`, which must be on PATH (`gsm doctor` checks once you have an Enshrouded instance). Each instance gets its own Wine prefix in `instances/<name>/wine/`; it holds nothing of yours, and if Wine misbehaves you can delete it while the server is stopped: the next start recreates it. On Windows the server runs natively.
- **Download size.** About 7.6 GB per instance.
- **One config file.** The whole config is `config/enshrouded_server.json`. gsm owns only `queryPort` (UDP, default 15637; a second instance gets 15638): it sets it to the instance's port before every start, so change the port with `--port` at `create`, not in the file. Everything else in the file is yours and goes into config history like Palworld's settings, except that `saveDirectory` and `logDirectory` must stay as the server writes them (`./savegame`, `./logs`): gsm snapshots only what lands in `savegame`, so a world saved elsewhere would have no snapshots.
- **Passwords in config history.** On its first start the server fills the file in, including random passwords for its user groups, and gsm commits it on the next start. Treat the config repo as secret ([Known limitations](design.md#known-limitations)).
- **Stopping.** gsm stops it with SIGINT, which makes it save on the way out (SIGTERM would kill Wine without saving).
- **Windows.** The server runs natively and passes the smoke test on a Windows Server CI runner, where gsm stops it with Ctrl+Break and it saves on the way out. It needs file symlinks for its config file, so on a desktop turn on Developer Mode or run gsm as admin.

## Command reference

```
gsm [--home DIR] <command> [args]

gsm doctor
gsm game list | show <game>
gsm create <game> <name> [--port N] [--set k=v]...
gsm list
gsm start|stop <name>... | --all
gsm restart|run <name>
gsm status [--json]
gsm logs <name> [-f]
gsm update <name>
gsm config diff|log|commit <name> [-m msg]
gsm save snapshot <name> [-m msg] | list <name> | restore <name> <id>
gsm addon add|remove <name> <addon>
gsm version | --version
```

Rules worth knowing:

- `start` commits uncommitted config changes first.
- Snapshots are taken automatically on `stop`, before `update` and before `restore`.
- `save restore` and `update` require the instance to be stopped.
- Port conflicts between instances are resolved by taking the next free port. Other programs on the machine aren't checked.
