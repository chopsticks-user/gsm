# How gsm Works

This document explains gsm twice over. Part 1 is a step-by-step walkthrough: one user goes from an empty machine to two running Palworld servers, snapshots, a restore and an update, with the real output of each command and what happens underneath. Part 2 is the technical reference: data formats, algorithms, crash safety and per-OS behavior. Part 3 covers what is designed but not built yet.

Status: the code lives on the `demo` branch. It implements, for Palworld: `doctor`, `game`, `create`, `list`, `start`, `stop`, `restart`, `run`, `status`, `logs`, `update`, `config diff|log|commit`, `save snapshot|list|restore`, and the `playit` addon. Everything else is design only and is described in Part 3. `docs/tasks.md` tracks progress.

---

## Overview

```
                    you
                     |
                     v
   +----------------------------------+
   |  gsm (one binary, exits after    |      no daemon: every command reads
   |  every command)                  |      state from disk, acts, and exits
   +----------------------------------+
     |        |         |          |
     v        v         v          v
  steamcmd   git     the server   podman
  (install)  (config (detached,   (playit addon)
             history) own process
                      group)
                     |
                     v
   $GSM_HOME/
     instances/<name>/   one folder per server
     store/              shared snapshot store for saves
```

Four ideas shape everything:

1. **No daemon.** gsm starts a server and exits. Later commands find the server again through a state file. Nothing restarts a crashed server.
2. **The install is disposable.** User data (config, saves) lives outside the steamcmd install and is linked into it. Reinstalling or validating can't touch it.
3. **Text goes to git, binaries go to the store.** Configs are versioned in git. Saves go to a content-addressed, deduplicated, zstd-compressed snapshot store.
4. **Crash-safe writes.** Every file gsm writes goes to a temp file first and is renamed into place, so a crash leaves the old version or the new one, never half a file.

### Code map

```
cmd/gsm/            the CLI: dispatch, flag parsing, one file per command group
internal/home       where $GSM_HOME is
internal/game       built-in game definitions (Go values), args templates
internal/instance   instance.toml, name rules, port assignment
internal/fsutil     linking data into the install, atomic writes, safe copies
internal/proc       launching detached servers, finding them, stopping them
internal/steam      steamcmd, build IDs, NixOS steamclient.so setup
internal/gitrepo    thin wrapper over the git CLI
internal/store      the snapshot store
internal/addon      the playit tunnel
```

---

# Part 1: Walkthrough

## Step 0: Build

gsm is one static binary with no runtime to install.

```
nix develop path:./flake -c go build ./cmd/gsm
```

Cross-compiling needs no extra toolchain, only the target OS and CPU:

```
GOOS=windows GOARCH=amd64 go build ./cmd/gsm     # gsm.exe
GOOS=darwin  GOARCH=arm64 go build ./cmd/gsm     # Apple Silicon
```

Code that differs per OS lives in files the compiler picks by name or build tag: `*_unix.go`, `*_windows.go`, `*_linux.go`. Everything else is shared.

Two modules are used: `github.com/BurntSushi/toml` to read and write `instance.toml`, and `github.com/klauspost/compress` for zstd. Everything else is the standard library.

## Step 1: Check the machine

```
$ gsm doctor
home     /home/you/.local/share/gsm
ok       git (/run/current-system/sw/bin/git)
ok       steamcmd (/home/you/.nix-profile/bin/steamcmd)
ok       steam-run (/home/you/.nix-profile/bin/steam-run)
ok       steamclient.so (/home/you/.local/share/Steam/linux64/steamclient.so)
```

**What happens.** First, gsm resolves `$GSM_HOME`, the root of all its data. The first match wins:

1. `--home DIR` (a global flag, given before the command: `gsm --home /srv/gsm status`)
2. the `GSM_HOME` environment variable
3. the per-OS default:
   - Linux: `$XDG_DATA_HOME/gsm`, else `~/.local/share/gsm`
   - macOS: `~/Library/Application Support/gsm`
   - Windows: `%LOCALAPPDATA%\gsm`. Never `%APPDATA%`: that is the roaming profile, which can sync between machines, and a server install is gigabytes.

The resolver is a pure function that takes the OS name and environment as parameters, so the unit tests check every OS branch on any machine.

Then `doctor` looks up `git` and `steamcmd` on `PATH`. On NixOS (detected by `/etc/NIXOS`), it also checks for `steam-run` and a non-empty `steamclient.so` in one of `~/.local/share/Steam/linux64`, `~/.steam/steam/linux64` or `~/.steam/steamcmd/linux64`. If any instance uses an addon, it checks that addon's needs too (podman and `~/.config/playit/env` for playit). It exits non-zero when something is missing, so scripts can use it.

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
settings:      port, players (set with --set k=v)
stop timeout:  30s
```

**What happens.** Games are Go values compiled into the binary, in `internal/game`. The code that installs, launches and stops servers is identical for every game. A game definition only supplies the facts that differ:

| Field | Palworld |
|---|---|
| Steam app ID | `2394010` |
| Launch per OS | Linux: `PalServer.sh`; Windows: `PalServer.exe` |
| Config path in the install | `Pal/Saved/Config/LinuxServer` (or `WindowsServer`) |
| Saves path in the install | `Pal/Saved/SaveGames` |
| Args template | `-port={port} -players={players} -useperfthreads ...` |
| Defaults | `players=16` |
| Default port | `8211` |
| Stop timeout | 30s |
| Default config | `DefaultPalWorldSettings.ini`, copied to `PalWorldSettings.ini` |

Paths are written with `/` and converted with `filepath.FromSlash`, so one definition works on Windows too. A game "supports" an OS exactly when it has a launch entry for it. Palworld has none for macOS, because Valve ships no macOS server.

**Args templates.** Each argument may contain `{name}` placeholders. When gsm renders them, it fills each one from the instance's values. An unknown placeholder is an error, never an empty string, so a typo can't silently reach the server. The placeholder names (except `port`, which has its own flag) are exactly what `--set` accepts.

Runtime manifests (TOML files users drop in a folder) were considered and rejected for now. They would need a parser, override rules, validation at load time, and backup handling during recovery, which only pays off once other people add games.

## Step 3: Create an instance

```
$ gsm create palworld main --set players=4
installing Palworld with steamcmd (this can take a while)...
  ... steamcmd output ...
created main (Palworld, port 8211)
start it with: gsm start main
```

An instance is one named server. Names are unique across all games, so every later command takes just `main`.

**What happens, in order:**

1. **Look up the game, check the platform.** On macOS this stops with `Palworld ships no dedicated server for darwin (supported: linux, windows)`, before anything is written.

2. **Validate the name.** It must match `^[a-z0-9][a-z0-9_-]{0,62}$`. That is safe as a folder name on every OS: no path separators, no `..`, no reserved characters like `:`, and no case collisions on case-insensitive filesystems.

3. **Validate `--set`.** Each `k=v` must name a placeholder of the game's args template. `--set nope=1` fails with the list of valid keys.

4. **Claim the name.** gsm calls `os.Mkdir` (not `MkdirAll`) on `instances/main`. Mkdir fails if the folder exists, and the operating system does this atomically. So two `create main` commands racing each other can't both succeed, and a name taken by any game is rejected:
   ```
   $ gsm create palworld main
   gsm: instance "main" already exists
   ```

5. **Assign the port.** gsm collects the ports of all existing instances and takes the requested port (or the game's default), moving up until it finds one nobody uses:
   ```
   $ gsm create palworld test
   port 8211 is used by another instance, using 8212
   ```
   This only knows about gsm's own instances. It does not probe whether another program on the machine holds the port.

6. **Make the layout.** gsm creates `config/`, `saves/`, `server/` and `logs/`.

7. **Install with steamcmd.** gsm runs
   ```
   steamcmd +force_install_dir <abs path to server/> +login anonymous +app_update 2394010 validate +quit
   ```
   with output streamed to your terminal. steamcmd's exit code is unreliable (it can exit 0 without installing anything), so gsm also checks the result: `server/steamapps/appmanifest_2394010.acf` must exist and contain a `"buildid"` line. That build ID is read again later for every snapshot.

8. **Seed the config.** If `config/` is empty and the install has `DefaultPalWorldSettings.ini`, gsm copies it to `config/PalWorldSettings.ini`, so you have a file to edit.

9. **Link the data into the install.** This is the core safety rule, carried over from `palworld.sh`'s `link_to`:
   ```
   server/Pal/Saved/Config/LinuxServer  ->  instances/main/config
   server/Pal/Saved/SaveGames           ->  instances/main/saves
   ```
   The server reads and writes through these links without noticing, while the real files live outside `server/`. The links are symlinks on Unix and directory junctions on Windows. `LinkTo(path, target)` handles every starting state:

   | What is at `path` | What LinkTo does |
   |---|---|
   | nothing | create the link |
   | a link already pointing at `target` | nothing |
   | a link pointing somewhere else | remove the link (never its target), create the right one |
   | a real folder, `target` empty | move the folder's contents into `target`, then link |
   | a real folder, `target` has files | stop with an error and touch nothing |
   | a regular file | stop with an error |

   The "both have files" case is deliberate: gsm never guesses which copy of a world is the right one.

10. **Write `instance.toml`:**
    ```toml
    game = "palworld"
    port = 8211

    [settings]
      players = "4"

    [retention]
      keep = 50
    ```
    `[retention]` is recorded now, but nothing enforces it until `save prune` exists.

11. **Start config history.** gsm turns the instance folder into a git repo whose `.gitignore` tracks only the config:
    ```
    /*
    !/.gitignore
    !/instance.toml
    !/config/
    ```
    `/*` ignores everything at the top level, and the `!` lines bring back what should be versioned. Saves, the install, logs and the state file can never be committed by accident. gsm then commits with the message `create main`.

**If any step fails**, gsm deletes `instances/main` entirely. Nothing in it belongs to you yet, and leaving it behind would block the name.

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

## Step 4: Edit the config

Edit the file directly:

```
$ $EDITOR ~/.local/share/gsm/instances/main/config/PalWorldSettings.ini
$ gsm config diff main
diff --git a/config/PalWorldSettings.ini b/config/PalWorldSettings.ini
-ServerName=Edited
+ServerName=Diffed
```

You can commit with a message (`gsm config commit main -m "rename server"`) or not bother: `start` commits for you.

**What happens.** `gitrepo` runs `git -C <instance dir> --no-pager ...`, capturing output and turning git's stderr into the error message. `config diff` first runs `git add -A --intent-to-add`, so brand-new files show up in the diff without their content being staged, then `git diff HEAD`. If you have no git identity configured, gsm passes `-c user.name=gsm -c user.email=gsm@localhost` for its own commits, so a fresh machine never fails to commit.

## Step 5: Start the server

```
$ gsm start main test
main: committed config changes
main: started (pid 39692, port 8211, config 681f6e5)
test: started (pid 39715, port 8212, config f7ebf67)
```

**What happens, in order:**

1. **Already running?** If the state file says so (see Step 6 for how that's checked), gsm prints `already running` and moves on. This is not an error, so `start --all` is safe to repeat.

2. **Commit the config.** Any uncommitted change is committed as `auto: before start`. Each run now maps to an exact config revision (`681f6e5`), which goes into the log header and into every snapshot.

3. **NixOS preparation:**
   - Refuse a `$GSM_HOME` under `/tmp`. `steam-run` gives the server a fresh, empty `/tmp`, so the server would not find its own files.
   - Copy `steamclient.so` into `~/.steam/sdk64/`, where Steam servers look for it. That folder is often a symlink to the very folder the file comes from. Opening the destination for writing would then truncate the source, destroying the library. `CopyFile` compares both files' identity first (device and inode) and skips the copy if they are the same file.

4. **Re-check the links.** `update` runs steamcmd with `validate`, which could replace a link. `LinkTo` is a no-op when the links are fine.

5. **Build the command line.** The args template is filled with the game defaults, overridden by the instance's settings, plus the port:
   ```
   PalServer.sh -port=8211 -players=4 -useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS
   ```
   On NixOS the whole thing is prefixed with `steam-run`, which runs it inside an FHS-compatible sandbox. The working directory is `server/`.

6. **Open the log.** Output is appended to `logs/server.log`, after a header:
   ```
   === gsm start 2026-11-09T19:47:39-05:00, config 41870a7 ===
   ```

7. **Launch detached.** `proc.Launch`:
   - starts the process in a new session (`setsid`). It has no controlling terminal, so closing your terminal sends it no SIGHUP. As the session leader, its PID is also its process group ID (PGID).
   - leaves stdin as the null device, with stdout and stderr going to the log file.
   - reads the new process's start time (Step 6 explains why).
   - writes `state.json` atomically:
     ```json
     {"pid":39692,"start_time":"525318","instance":"main"}
     ```

8. **Catch an immediate crash.** gsm reaps its child in the background and waits 2 seconds. If the server is already gone (a missing library, a bad argument), gsm clears the state file and reports it instead of claiming success:
   ```
   main: server exited right after start; see: gsm logs main
   ```
   Reaping matters here: a dead child that isn't reaped stays a zombie, which still counts as a member of its process group and would look alive.

9. **Start addons.** If an addon fails, gsm warns and the server keeps running. A broken tunnel shouldn't take the game down.

Then gsm exits. The server keeps running on its own.

### The process tree

On NixOS, a running Palworld instance looks like this:

```
session/group 39692
  steam-run ...               <- PID 39692, what gsm launched (may exit early)
    bwrap (sandbox)
      sh PalServer.sh -port=8211 ...
        PalServer-Linux-Shipping Pal -port=8211 ...   <- the real server
```

`PalServer.sh` doesn't `exec` the real binary; it runs it as a child. In testing, the process gsm launched also exited while its children kept running. This is why gsm tracks the whole group, not one PID.

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

**What happens.** For each instance, gsm reads `state.json` and decides whether that server still runs. There is no daemon to ask, so the state file has to be enough, and a plain PID is not:

- **PIDs get reused.** After the server dies, or after a reboot, the OS can hand its PID to an unrelated process.
- **Binary names are shared.** Two Palworld instances run the same binary. `palworld.sh` finds its server with `pgrep -f`, which can't tell them apart. It even matches itself: while testing, `pgrep -f PalServer-Linux` found the shell running that very command.

So the state file records the PID, the process start time and the instance name, and the check goes:

| Situation | Verdict |
|---|---|
| state file missing, unreadable, or for another instance name | stopped |
| a process with that PID exists, same start time | running |
| a process with that PID exists, different start time | stopped (the PID was reused) |
| no process with that PID, but its process group still has members | running (the launched wrapper exited, the server didn't) |
| no process with that PID, no group | stopped |

The fourth row is safe because of a POSIX guarantee: the OS never reuses a PID while it is still the ID of a live process group. A group with our PGID can therefore only be ours. The test for "any member left" is `kill(-pgid, 0)`, which delivers no signal and only reports whether the group exists.

**Start time per OS:**
- **Linux:** field 22 of `/proc/<pid>/stat`, in clock ticks since boot. The process name (field 2) is in parentheses and may contain spaces, so gsm splits after the last `)`. A process in state `Z` (zombie) or `X` counts as gone.
- **macOS:** `ps -o lstart= -p <pid>`. It has one-second resolution, which is enough unless a PID is reused within the same second.
- **Windows:** the creation time from `GetProcessTimes`, after checking that `GetExitCodeProcess` reports `STILL_ACTIVE`.

## Step 7: Follow the log

```
$ gsm logs main -f
=== gsm start 2026-11-09T19:47:39-05:00, config 41870a7 ===
fake server args: -port=8211 -players=4 ...
```

**What happens.** gsm prints the file, then with `-f` checks its size every 500 ms and prints anything new. If the file shrinks (it was truncated), gsm starts over from the top. Ctrl+C ends it. Polling instead of `tail -f` or inotify works the same on every OS.

## Step 8: Take a snapshot

```
$ gsm save snapshot main -m "before boss fight"
warning: main is running and palworld has no save-flush method; the snapshot may miss recent progress
main: snapshot 3735cbf0 (1 files, 7 B)
```

Saves don't go into git. They are large binaries rewritten in full on every save, so git history would grow without limit. They go to the shared store:

```
store/
  objects/3f/3f9a...c1        zstd-compressed content, named by the SHA-256 of the raw bytes
  snapshots/main/20261110T005001Z-3735cbf0.json
  lock
```

**What happens, in order:**

1. **Warn if running.** A running server may be mid-write, or may hold progress in memory. Palworld has no save-flush method in gsm yet (its REST API `/v1/api/save` is the candidate), so gsm warns but proceeds.

2. **Take the store lock** (details in Part 2). Only one gsm process writes to the store at a time.

3. **Walk `saves/`.** For each regular file:
   1. Hash it with SHA-256.
   2. If `objects/<first 2 hex>/<hash>` exists, the content is already stored, by any earlier snapshot of any instance. Nothing more to do.
   3. Otherwise, stream the file through zstd into a temp file in the same folder, **hashing it a second time while compressing**. If the two hashes differ, the server changed the file in between, and storing it would put content under the wrong name. gsm fails with `changed while it was being saved; try again`.
   4. `fsync` the temp file, then rename it to its final name. A rename within one folder is atomic.

   Anything that isn't a regular file or a folder (a symlink, a socket) stops the snapshot rather than being skipped silently.

4. **Write the record last:**
   ```json
   {
     "id": "3735cbf0",
     "instance": "main",
     "game": "palworld",
     "created": "2026-11-10T00:50:01Z",
     "message": "before boss fight",
     "trigger": "manual",
     "config_rev": "681f6e5",
     "server_build": "18519483",
     "pinned": false,
     "files": [
       {"path": "0/W/Level.sav", "size": 7, "mode": 420, "hash": "3f9a...c1"}
     ]
   }
   ```
   The ID is 8 random hex characters. The file name starts with the UTC time, so a folder listing is in time order. Paths always use `/`, so a record written on Windows reads correctly on Linux.

Because the record comes last, **a snapshot exists exactly when its record does.** A crash halfway leaves some objects that no record mentions. They waste space but never corrupt anything, and a future cleanup pass deletes them.

**Why whole files, not chunks.** Palworld's `Level.sav` is already compressed and rewritten in full on every save, so splitting it into chunks to find shared pieces would gain almost nothing. Files that don't change between saves, like player files, dedupe for free as whole files.

```
$ gsm save list main
ID        CREATED              TRIGGER  FILES  SIZE  CONFIG   BUILD     MESSAGE
3735cbf0  2026-11-09 19:50:01  manual   1      7 B   681f6e5  18519483  before boss fight
07c32289  2026-11-09 19:50:01  stop     1      7 B   681f6e5  18519483
```

Every snapshot links a world to the exact config revision and server build that produced it.

## Step 9: Stop the server

```
$ gsm stop main
main: stopping (up to 30s)...
main: stopped
main: snapshot 07c32289 (1 files, 7 B)
```

**What happens, in order:**

1. **Stop addons** first, so the tunnel goes down before the game.
2. **Not running?** Print `not running`, remove any stale state file, done.
3. **Ask the group to exit.** gsm sends SIGTERM to the whole process group (`kill(-pgid, SIGTERM)`). Signalling only the launched PID would miss the real server, which is a child of `PalServer.sh`.
4. **Wait.** gsm checks every 200 ms, up to the game's timeout (30 s for Palworld), whether the server is gone. "Gone" uses the same check as `status`.
5. **Kill if needed.** If the server is still there, gsm sends SIGKILL to the group, waits up to 10 more seconds, and reports `did not exit in time, killed`.
6. **Clear `state.json`.**
7. **Take an automatic snapshot** with trigger `stop`. The server has exited, so the files on disk are final and the snapshot is consistent. Every shutdown leaves a restore point.

The other instance is untouched, because it is a different process group:

```
$ gsm status
NAME  GAME      PORT  STATE    PID    ADDONS
main  palworld  8211  stopped  -
test  palworld  8212  running  39715
```

`stop --all` stops every instance; `restart <name>` is `stop` followed by `start`.

## Step 10: Run in the foreground

```
$ gsm run main
main: running in the foreground (pid 40029), Ctrl+C to stop
fake server args: -port=8211 -players=4 ...
^C
main: stopping (up to 30s)...
main: snapshot e975a8e7 (1 files, 7 B)
```

**What happens.** `run` uses the same preparation and launch as `start`, with three differences:

- **Output.** It goes to your terminal and the log at once. gsm passes a combined writer instead of a file, which makes Go copy the output through a pipe. A pipe works even though the server runs in its own session (or, on Windows, its own console).
- **Ctrl+C.** The server is in another session, so your terminal's Ctrl+C reaches only gsm. gsm catches it (and SIGTERM) and runs exactly the stop sequence from Step 9, including the snapshot.
- **State file.** It is still written, so `gsm status` in another terminal sees the foreground server.

If the server exits on its own, `run` stops the addons, clears the state and takes the `stop` snapshot too.

## Step 11: Restore a snapshot

```
$ gsm save restore main 700d938f
main: safety snapshot 85c597d3
main: restored snapshot 700d938f from 2026-11-09 19:50:17
```

IDs can be shortened to any unique prefix (`gsm save restore main 700d`).

**What happens.** Restore refuses to run while the server runs (`stop the server first: gsm stop main`), since a running server would overwrite the restored files. Then, under the store lock:

1. **Repair an interrupted earlier restore**, if any (see the table below).
2. **Safety snapshot.** The current saves are snapshotted with trigger `restore` and the message `before restoring 700d938f`. A restore can always be undone by restoring the safety snapshot.
3. **Stage.** Every file of the chosen snapshot is decompressed into `saves.restore/`. While writing, gsm hashes the output and compares both size and hash with the record. One mismatch (disk corruption, a missing object) deletes the staging folder and stops. The live saves have not been touched yet.
4. **Swap** with two renames:
   ```
   saves          -> saves.old
   saves.restore  -> saves
   ```
   then delete `saves.old`. If the second rename fails, the first is undone.

The link in `server/` points at the path `instances/main/saves`, not at the folder itself, so after the swap it reaches the restored folder with no change.

A crash at any point is recoverable:

| Crash during | State left on disk | What happens next |
|---|---|---|
| safety snapshot | live saves intact, maybe orphan objects | nothing to repair |
| staging | live saves intact, partial `saves.restore/` | the next restore deletes it first |
| between the two renames | `saves.old/` exists, `saves/` doesn't | the next restore renames `saves.old` back |
| after the swap, before cleanup | both `saves/` (new) and `saves.old/` | the next restore deletes `saves.old` |

The unit tests cover the corrupt-object case (live saves stay byte-identical) and the crash between the renames.

## Step 12: Update the server

```
$ gsm update main
main: snapshot bc343959 (1 files, 7 B)
  ... steamcmd output ...
main: updated build 18519483 -> 18602117
```

**What happens.** It refuses to run while the server runs. Then:

1. a snapshot with trigger `update` (its `server_build` is the old build)
2. steamcmd with `validate`, on `server/`
3. the links are re-checked. If steamcmd replaced a link with a real folder holding files, `LinkTo` stops instead of merging.
4. the build ID is read before and after. gsm reports `already up to date` or the change.

If a game update breaks your world, `save list` shows which build each snapshot came from, and you can restore the last good one.

## Step 13: Add the playit tunnel

```
$ gsm addon add main playit
main: added addon playit
```

**What happens.** `playit` is added to `addons` in `instance.toml`. From then on, `start` and `run` start the container after the server, and `stop` stops it first:

```
podman run -d --rm --name gsm-main-playit --network=host \
  --env-file ~/.config/playit/env ghcr.io/playit-cloud/playit-agent:latest
```

Each instance gets its own container name, so two instances can each have a tunnel. `--rm` deletes the container when it stops. If the instance is already running, `addon add` starts the tunnel right away, and `addon remove` stops it. The env file holds `SECRET_KEY=...`. It is a secret, never copied anywhere, and gsm warns if it's missing.

---

# Part 2: Technical reference

## Data layout

```
$GSM_HOME/
  instances/
    <name>/                  git repo (tracks only .gitignore, instance.toml, config/)
      .gitignore
      instance.toml
      config/                linked into the server's config path
      saves/                 linked into the server's save path
      saves.restore/         only during a restore
      saves.old/             only during a restore
      server/                steamcmd install, disposable
      logs/server.log        appended, one header per run
      state.json             only while running
  store/
    objects/<2 hex>/<sha256>
    snapshots/<instance>/<YYYYMMDDTHHMMSSZ>-<id>.json
    lock                     only while a gsm process writes
```

## File formats

**`instance.toml`**

| Key | Type | Meaning |
|---|---|---|
| `game` | string | game name, e.g. `palworld` |
| `port` | int | game port |
| `settings` | table of strings | values for args placeholders, overriding game defaults |
| `addons` | array of strings | e.g. `["playit"]` |
| `retention.keep` | int | recorded, not enforced yet |

**`state.json`:** `pid` (int), `start_time` (string, an OS-specific format, compared only for equality), `instance` (string).

**Snapshot record:** see Step 8. `mode` is the file's permission bits. `trigger` is one of `manual`, `stop`, `update`, `restore`. `tags` (omitted when empty) and `pinned` exist for the planned `pin` and `tag` commands.

**Objects:** a standard zstd frame holding the raw file bytes. Any `zstd -d` can read one.

## Atomic writes

`WriteAtomic` (used for `state.json`, `instance.toml`, `.gitignore` and snapshot records) and the object writer follow the same pattern:

1. create a temp file in the **same folder** as the destination, so the rename stays on one filesystem
2. write, then `fsync` the file
3. set permissions
4. rename over the destination. POSIX guarantees the rename is atomic, and Windows `MoveFileEx` replaces in place.
5. on any error, delete the temp file

Readers see either the complete old file or the complete new one.

One limit: the containing folder isn't `fsync`ed after the rename, so after a power cut the rename itself might be lost (the old file would remain). That would lose the latest snapshot record, but never corrupt one.

## The store lock

`store/lock` is created with `O_CREATE|O_EXCL`, which the OS performs atomically: exactly one process can create it. It contains the owner's PID and start time.

If the lock exists, gsm reads it and checks whether the owner still runs, comparing the PID and start time exactly, without the process-group rule (a gsm process is not a server). A dead owner means a crash left the lock behind, so gsm deletes it and retries. A live owner means gsm waits, polling every 200 ms for up to 30 s, then fails with `snapshot store is locked by another gsm process (pid N)`.

Commands that write to the store take the lock: `save snapshot`, the automatic snapshots in `stop`, `run` and `update`, and `save restore`. Restore holds it across both the safety snapshot and the swap.

## Crash safety summary

| Interrupted operation | Worst case | Recovery |
|---|---|---|
| `create` | half-made instance folder | gsm deletes it on error; after a hard crash, delete it by hand |
| writing `instance.toml` / `state.json` | none: old file kept | none needed |
| `start` after launch, before `state.json` | server running, gsm doesn't know | stop it by hand (rare: one write after launch) |
| snapshot | orphan objects, no record | none needed; cleanup (planned) reclaims space |
| restore | see Step 11 | automatic on the next restore |
| store lock holder crashes | stale lock file | taken over automatically |

## Cross-platform behavior

| Concern | Linux | macOS | Windows |
|---|---|---|---|
| data root | `$XDG_DATA_HOME/gsm` or `~/.local/share/gsm` | `~/Library/Application Support/gsm` | `%LOCALAPPDATA%\gsm` |
| link data into install | symlink | symlink | directory junction (`mklink /J`), no admin needed |
| detach server | new session (`setsid`) | new session (`setsid`) | new process group + hidden console |
| process start time | `/proc/<pid>/stat` field 22 | `ps -o lstart=` | `GetProcessTimes` |
| survivor detection | process group | process group | launched process only |
| graceful stop | SIGTERM to the group | SIGTERM to the group | Ctrl+Break to the group |
| force stop | SIGKILL to the group | SIGKILL to the group | `taskkill /T /F` |
| Palworld | yes (NixOS: via `steam-run`) | refused at `create` | yes |

**Windows details.** Windows has no SIGTERM. The closest equivalent is a Ctrl+Break console event, which `GenerateConsoleCtrlEvent` can send to a process group. The sender must share the server's console, so the server is started with a hidden console of its own (`CREATE_NO_WINDOW`). To send the event, gsm must detach from its own console and attach to the server's, which would disturb gsm's terminal. So gsm starts a short-lived copy of itself (`gsm __gsm-ctrl-break <pid>`) that does `FreeConsole`, `AttachConsole(pid)` and `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid)`, then exits. The force-stop fallback, `taskkill /T`, walks the process tree by parent PID. A Job Object would be more thorough (planned). Directory junctions show up in Go 1.23+ as `ModeIrregular` rather than symlinks, which `LinkTo` accounts for. All of this compiles, but none of it has run on Windows yet.

**macOS.** Palworld ships no macOS server, so `create palworld` refuses. The rest works the same as Linux, except for the start time lookup.

## gsm compared with palworld.sh

| | palworld.sh | gsm |
|---|---|---|
| Platforms | Linux (NixOS) | Linux, Windows, macOS |
| Instances | one | any number, names unique across games |
| Finding the server | `pgrep -f` by binary path | state file + start time + process group |
| Stopping | `pkill` by name | signal to that instance's group only |
| Config history | none | git, auto-commit on start |
| Saves | none | deduplicated snapshots, automatic on stop/update/restore |
| Data safety | `link_to` | the same `link_to` logic, plus atomic writes everywhere |
| Shipping | needs bash, procps, coreutils | one binary |

## Testing

Tests use the real filesystem in temporary folders, never mocks. Processes are faked by re-running the test binary: `TestMain` checks an environment variable and acts as an "obedient" server (exits on SIGTERM), a "stubborn" one (ignores it), or a "wrapper" that starts a child like `PalServer.sh` does, optionally exiting early like `steam-run`. steamcmd is faked the same way. This works identically on every OS.

The CLI was also run end to end with a fake `steamcmd` and a fake `PalServer.sh` that spawns a child and rewrites a save every second. That test found two bugs that unit tests had missed: the early-exiting `steam-run` wrapper, and `steam-run` hiding `/tmp`.

---

# Part 3: Planned

These follow the same design but aren't built yet.

**Instances.** `gsm remove <name>` refuses while running, confirms, and keeps the instance's snapshots. `gsm import <game> <name> --config DIR --saves DIR` adopts an existing server's data, for example `palworld.sh`'s.

**Config.** `config edit` opens your editor; `config restore <rev>` checks out an old revision; `config remote` pushes the repo somewhere.

**Snapshots.**
- `save show` and `save diff` inspect records.
- `save verify` re-hashes every object to detect disk corruption.
- `save restore --into <new>` forks a new instance from a snapshot. History stays one straight line per instance, with no branches.
- `pin`, `tag` and `annotate` edit records. Pinned snapshots survive pruning.
- `save prune` applies the retention policy to records, then deletes objects no remaining record mentions.
- A save-flush method per game (for Palworld, the REST API) makes snapshots of a running server consistent.

**Off-machine backup and recovery.**

```
gsm remote add ...        # local path, SFTP or S3-compatible
gsm remote push
gsm recover
```

A remote holds a copy of the snapshot store, each instance's config repo and the instance definitions. gsm pushes automatically after each snapshot. Objects are named by hash and never change, so a push uploads only objects the remote lacks, plus new records. Data sent to cloud targets is encrypted. Secrets (admin passwords, the playit env file) are never uploaded.

`gsm recover` on a fresh machine rebuilds everything: it fetches instance definitions, clones config repos, reinstalls each server (`server/` was disposable anyway), restores the latest snapshots, recreates the links, and lists the secrets you need to enter again.

## Known limitations

- **No save flush:** a snapshot of a running server may miss recent progress. Snapshots taken on `stop` are consistent.
- **Graceful shutdown:** it's unverified whether Palworld saves on SIGTERM. If it doesn't, up to the last autosave interval of progress can be lost on stop.
- **Port checks:** port assignment checks only other gsm instances, not other programs. Palworld's optional REST API defaults to port 8212, the same port a second instance gets by default.
- **Secrets in git:** `AdminPassword` lives in `PalWorldSettings.ini`, which the local config repo tracks. That must be solved before remotes exist.
- **Log size:** `logs/server.log` grows forever.
- **Windows:** the code is untested on real Windows.

## Open questions

- Retention policy format
- Encryption scheme for cloud remotes
- How secrets are stored locally and re-entered after `recover`
