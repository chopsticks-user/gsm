# Architecture

How gsm is built: its parts, the data it keeps, and what each command does underneath. For why it is built this way, see [design.md](design.md). For using it, see [guide.md](guide.md).

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

## Code map

```
cmd/gsm/            the CLI: dispatch and flag parsing (main.go), one file per command group
  cmd_instance.go     doctor, game, create, list
  cmd_server.go       start, stop, restart, run, status, logs, update
  cmd_data.go         config, save and addon commands
  version.go          version (handled in main.go before the data dir is resolved)
internal/home       where $GSM_HOME is
internal/game       built-in game definitions (Go values), args templates
internal/instance   instance.toml, name rules, port assignment, instance paths
internal/fsutil     linking data into the install, atomic writes, safe copies, setting a JSON config key
internal/proc       launching detached servers, the state file, finding and stopping servers
internal/steam      steamcmd, build IDs, NixOS steamclient.so setup
internal/gitrepo    thin wrapper over the git CLI
internal/store      the snapshot store
internal/addon      the playit tunnel
```

`cmd/gsm` is the only package that prints or reads flags. The `internal` packages return errors and take everything they need as parameters, so they can be tested without the CLI.

Code that differs per OS lives in files the compiler picks by name or build tag: `*_unix.go`, `*_windows.go`, `*_linux.go`. Everything else is shared.

## Core concepts

- **Game:** a Go value describing one game's dedicated server.
- **Instance:** one named server of a game, with its own folder under `instances/`. Names are unique across all games, so commands take just the name.
- **Addon:** a companion process started and stopped with an instance, such as the playit tunnel.

### Games

Games are compiled into the binary, in `internal/game`. The code that installs, launches and stops servers is identical for every game; a definition only supplies the facts that differ:

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

Paths are written with `/` and converted with `filepath.FromSlash`, so one definition works on Windows too. A game supports an OS exactly when it has a launch entry for it.

**Args templates.** Each argument may contain `{name}` placeholders, filled from the game defaults overridden by the instance's settings, plus the port. An unknown placeholder is an error, never an empty string, so a typo can't silently reach the server. The placeholder names (except `port`, which has its own flag) are exactly what `create --set` accepts.

**Config files.** A launch entry names either a config folder (`ConfigDir`, like Palworld) or a single config file (`ConfigFile`) for servers that keep their whole config in one file in the install. That file lives in `config/<file name>`, and the install gets a file symlink to it (`LinkFileTo`, below). If the game sets `ConfigPortKey`, the file is JSON and gsm owns only that top-level key: before every start (and on `create` and `update`) it sets the key to the instance's port, rewriting only the value's bytes so the rest of the file and its formatting stay as they were and config history shows only real changes (`fsutil.SetJSONNumber`). A duplicated key gets every copy set, since parsers differ on which one wins, and a leading UTF-8 BOM is kept. A missing file is created holding just that key, for the server to fill in on its first start; a file that is not a valid JSON object (`null` included) fails with the parse error and is left alone. A hand edit of the port is put back on the next start, since `instance.toml` is the source of the port.

`internal/game/game_test.go` checks every built-in definition (`TestBuiltinGamesAreValid`).

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
    format                   store layout version
```

`$GSM_HOME` is resolved by a pure function that takes the OS name and environment as parameters, so the unit tests check every OS branch on any machine. The first match wins: `--home`, then `GSM_HOME`, then the per-OS default (see [Cross-platform behavior](#cross-platform-behavior)).

User data never lives inside `server/`. It is linked in, so a reinstall or a steamcmd `validate` can't destroy it.

## File formats

### Format versions

Every format gsm writes carries a version: `format` in `instance.toml`, `state.json` and each snapshot record, and the file `store/format` (one number and a newline) for the store layout: the `objects/`, `snapshots/` and `lock` structure. All are 1 so far. Each has its own constant (`instance.Format`, `proc.Format`, `store.Format`, `store.RecordFormat`), bumped only when that format changes. The lock has no version of its own: it is part of the store layout.

- **Missing version:** v0.1.0 wrote none, so a missing version reads as 1. `store/format` is created by the first `store.Lock`, while holding the lock, so it can never overwrite a newer gsm's version.
- **Newer version:** refused with `<file> has format N, this gsm knows up to M: written by a newer gsm; upgrade gsm` (`fsutil.CheckFormat`). gsm never reads data it might misunderstand or writes over it. `list`, `status` and `start|stop --all` still act on the other instances, then fail naming the refused ones rather than hiding them. A refused `state.json` fails the command for that instance rather than reporting the server stopped, which could start a second one; `stop` checks it before stopping addons.
- **Older version:** read as is. When a format first changes, the new gsm will either read both versions or migrate the old one; a migration writes its new data first and the version last, so an interrupted one still reads as the old version and simply runs again.

**`instance.toml`**

```toml
format = 1
game = "palworld"
port = 8211
addons = ["playit"]

[settings]
  players = "4"

[retention]
  keep = 50
```

| Key | Type | Meaning |
|---|---|---|
| `format` | int | format version (see [Format versions](#format-versions)) |
| `game` | string | game name, e.g. `palworld` |
| `port` | int | game port |
| `settings` | table of strings | values for args placeholders, overriding game defaults |
| `addons` | array of strings | e.g. `["playit"]` |
| `retention.keep` | int | recorded, not enforced yet |

**`.gitignore`** of an instance:

```
/*
!/.gitignore
!/instance.toml
!/config/
```

`/*` ignores everything at the top level, and the `!` lines bring back what is versioned. Saves, the install, logs and the state file can't be committed by accident.

**`state.json`:**

```json
{"format":1,"pid":39692,"start_time":"525318","instance":"main"}
```

`start_time` is an OS-specific string, compared only for equality.

**Snapshot record** (`store/snapshots/<instance>/<time>-<id>.json`):

```json
{
  "format": 1,
  "id": "3735cbf0",
  "instance": "main",
  "game": "palworld",
  "created": "2026-11-20T00:50:01Z",
  "message": "before boss fight",
  "trigger": "manual",
  "config_rev": "681f6e5",
  "server_build": "25247047",
  "pinned": false,
  "files": [
    {"path": "0/5367AE08ED5E4715BF9E626522422050/Level.sav", "size": 15185, "mode": 420, "hash": "3f9a...c1"},
    ...
  ]
}
```

The ID is 8 random hex characters. The file name starts with the UTC time, so a folder listing is in time order. Paths always use `/`, so a record written on Windows reads correctly on Linux. `mode` is the file's permission bits. `trigger` is one of `manual`, `stop`, `update`, `restore`. `tags` (omitted when empty) and `pinned` exist for the planned `pin` and `tag` commands.

**Objects:** a standard zstd frame holding the raw file bytes, named by the SHA-256 of those raw bytes. Any `zstd -d` can read one.

## Creating an instance

`gsm create <game> <name>`, in order:

1. **Look up the game and check the platform.** An unsupported OS stops here, before anything is written.
2. **Validate the name** against `^[a-z0-9][a-z0-9_-]{0,62}$`: safe as a folder name on every OS (no separators, no `..`, no reserved characters, no case collisions on case-insensitive filesystems).
3. **Validate `--set`:** each key must be a placeholder of the args template.
4. **Claim the name** with `os.Mkdir` (not `MkdirAll`) on `instances/<name>`. Mkdir fails atomically if the folder exists, so two racing `create` commands can't both succeed.
5. **Assign the port:** the requested port (or the game's default), moved up until no other instance uses it. Only gsm's own instances are checked, not other programs.
6. **Make the layout:** `config/`, `saves/`, `server/`, `logs/`.
7. **Install** with `steamcmd +force_install_dir <abs server/> +login anonymous +app_update <app> validate +quit`, output streamed. steamcmd's exit code is unreliable (it can exit 0 without installing), so gsm also requires `server/steamapps/appmanifest_<app>.acf` with a `"buildid"` line.
8. **Seed the config:** if `config/` is empty and the install has the game's default config, copy it in. Palworld empties an unedited copy of its defaults on the first start (an edited one survives), so don't re-seed an empty `PalWorldSettings.ini`: Palworld would empty it again on every start.
9. **Link the data into the install** with `LinkTo` (below), or `LinkFileTo` for a config file, whose port key is set here too.
10. **Write `instance.toml` and `.gitignore`**, `git init`, and commit `create <name>`.

If any step fails, gsm deletes `instances/<name>` entirely: nothing in it belongs to the user yet, and leaving it would block the name.

### LinkTo

`LinkTo(path, target)` makes `path` (inside `server/`) a link to `target` (the instance's folder). Symlinks on Unix, directory junctions on Windows. It handles every starting state:

| What is at `path` | What LinkTo does |
|---|---|
| nothing | create the link |
| a link already pointing at `target` | nothing |
| a link pointing somewhere else | remove the link (never its target), create the right one |
| a real folder, `target` empty | move the folder's contents into `target`, then link |
| a real folder, `target` has files | stop with an error and touch nothing |
| a regular file | stop with an error |

The "both have files" case is deliberate: gsm never guesses which copy of a world is the right one. `start` and `update` call `LinkTo` again, since steamcmd `validate` could replace a link; it is a no-op when the links are fine.

`LinkFileTo(path, target)` is the same for one file: nothing at `path` creates the link (`target` may not exist yet; the server creates it through the link), a correct link is kept, a wrong link is replaced, a real file moves to `target` if `target` doesn't exist, and if both exist nothing is touched. It always makes a symlink, first under a temp name (`<path>.gsm-link`) and then renamed into place, so a failed symlink changes nothing and a crash between moving the file and placing the link is finished by the next call. On Windows a file symlink needs Developer Mode or admin rights, and gsm fails with that message rather than falling back to a hard link: gsm's atomic writes and git checkouts replace the file, which would silently split a hard link into two diverging copies.

## Config history

`internal/gitrepo` runs `git -C <instance dir> --no-pager ...`, capturing output and turning git's stderr into the error. `config diff` first runs `git add -A --intent-to-add`, so new files show up without their content being staged, then `git diff HEAD`. Without a configured git identity, gsm passes `-c user.name=gsm -c user.email=gsm@localhost` for its own commits, so a fresh machine never fails to commit. `start` commits any uncommitted change as `auto: before start`, so every run maps to an exact revision, which goes into the log header and every snapshot.

## Process supervision

### Start

`gsm start <name>`, in order:

1. **Already running?** (the check below) Print `already running` and move on, so `start --all` is safe to repeat.
2. **Check the platform:** a game without a launch entry for this OS fails with `<game> has no server for <os>`.
3. **Re-check the links** with `LinkTo` (or `LinkFileTo`) and set a config file's port key, before the commit so it records the port the server runs with.
4. **Commit the config** (`auto: before start`).
5. **NixOS preparation:** refuse a `$GSM_HOME` under `/tmp`, because `steam-run` gives the server a fresh, empty `/tmp`; copy `steamclient.so` into `~/.steam/sdk64/`. That folder is often a symlink to the folder the file comes from, so `CopyFile` compares device and inode first and skips a copy onto itself rather than replacing Steam's own file with a copy of itself.
6. **Find the launcher** in `server/`. If it is missing (a deleted or failed install), fail with `server not installed (...); run: gsm update <name>`. The config commit from step 4 stays.
7. **Build the command line** from the args template. On NixOS it is prefixed with `steam-run`. The working directory is `server/`.
8. **Open the log** (`logs/server.log`, appended) and write a header: `=== gsm start <RFC 3339 time>, config <rev> ===`.
9. **Launch detached** (`proc.Launch`): a new session (`setsid`), so closing the terminal sends no SIGHUP and the server's PID is also its process group ID; stdin from the null device, stdout and stderr to the log; read the start time; write `state.json` atomically.
10. **Catch an immediate crash:** reap the child in the background and wait 2 seconds. If the server is already gone, clear the state file and report `server exited right after start`. Reaping matters: an unreaped dead child stays a zombie, still a member of its group, and would look alive.
11. **Start addons.** A failing addon warns; the server keeps running.

Then gsm exits.

### The process tree

On NixOS, a running Palworld instance looks like this:

```
session/group 39692
  steam-run ...               <- PID 39692, what gsm launched (may exit early)
    bwrap (sandbox)
      sh PalServer.sh -port=8211 ...
        PalServer-Linux-Shipping Pal -port=8211 ...   <- the real server
```

`PalServer.sh` doesn't `exec` the real binary; it runs it as a child, and the process gsm launched can exit while its children keep running. This is why gsm tracks the whole group, not one PID.

### Is it running?

There is no daemon to ask, so the state file has to be enough, and a plain PID is not: PIDs get reused after a process dies or after a reboot, and two instances of a game run the same binary, so a name match can't tell them apart. The state file records the PID, the process start time and the instance name:

| Situation | Verdict |
|---|---|
| state file missing, unreadable, or for another instance name | stopped |
| a process with that PID exists, same start time | running |
| a process with that PID exists, different start time | stopped (the PID was reused) |
| no process with that PID, but its process group still has members | running (the launched wrapper exited, the server didn't) |
| no process with that PID, no group | stopped |

The fourth row relies on a POSIX guarantee: the OS never reuses a PID while it is still the ID of a live process group, so a group with our PGID can only be ours. `kill(-pgid, 0)` reports whether the group exists without delivering a signal.

**Start time per OS:**

- **Linux:** field 22 of `/proc/<pid>/stat`, in clock ticks since boot. The process name (field 2) is in parentheses and may contain spaces, so gsm splits after the last `)`. State `Z` or `X` counts as gone.
- **macOS:** `ps -o lstart= -p <pid>`, with one-second resolution.
- **Windows:** the creation time from `GetProcessTimes`, after `GetExitCodeProcess` reports `STILL_ACTIVE`.

### Stop

1. **Stop addons** first, so the tunnel goes down before the game.
2. **Not running?** Print `not running`, remove a stale state file, done.
3. **SIGTERM to the whole group** (`kill(-pgid, SIGTERM)`; Ctrl+Break on Windows). Signalling only the launched PID would miss the real server. If sending the request fails, gsm warns and carries on with step 4 anyway: the request may have been delivered, and the server may be saving.
4. **Wait,** checking every 200 ms with the running check, up to the game's stop timeout.
5. **SIGKILL to the group** if it's still there, wait up to 10 more seconds, report `did not exit in time, killed`.
6. **Clear `state.json`.**
7. **Snapshot** with trigger `stop`. The server has exited, so the files are final.

`restart` is `stop` then `start`.

### Run in the foreground

`run` shares steps 2-9 of [Start](#start) (platform check, config commit, NixOS preparation, links, launcher, command line, log, detached launch), except:

- **Already running** is an error (`already running in the background (pid N)`, non-zero exit), not a no-op.
- **No immediate-crash check:** `run` stays attached, so it sees the exit directly. Addons start right after launch.
- **Output** goes to the terminal and the log at once, through a combined writer, which makes Go copy the output through a pipe. A pipe works even though the server is in its own session (or console).
- **Ctrl+C** reaches only gsm, since the server is in another session. gsm catches it (and SIGTERM) and runs the stop sequence, snapshot included.
- **The state file** is still written, so `status` elsewhere sees the server.

If the server exits on its own, `run` stops the addons, clears the state and takes the `stop` snapshot.

### Logs

`logs` prints the file; with `-f` it checks the size every 500 ms and prints what's new, starting over if the file shrank. Polling works the same on every OS.

## Snapshot store

### Taking a snapshot

1. **Warn if running** and the game has no save-flush method: a running server may be mid-write or hold progress in memory.
2. **Take the store lock.**
3. **Walk `saves/`.** For each regular file:
   1. hash it with SHA-256, counting the bytes, and read its permissions from the same open file: the recorded size, mode and hash always describe one version of the file, even if the server rewrites or replaces it after the walk listed it. A file that disappears in between (a temp file renamed away) fails the snapshot with `changed while it was being saved; try again`
   2. if `objects/<first 2 hex>/<hash>` exists, the content is already stored (by any snapshot of any instance)
   3. otherwise stream it through zstd into a temp file in the same folder, **hashing it a second time while compressing**. Different hashes mean the server changed the file in between, so gsm fails with `changed while it was being saved; try again` rather than store content under the wrong name.
   4. `fsync` the temp file and rename it into place

   Anything that isn't a regular file or a folder (a symlink, a socket) stops the snapshot rather than being skipped silently.
4. **Write the record last.**

Because the record comes last, a snapshot exists exactly when its record does. A crash halfway leaves objects no record mentions; they waste space but never corrupt anything.

### The store lock

`store/lock` holds the owner's PID and start time. gsm first refuses a store with a newer `store/format` (see [Format versions](#format-versions)), then writes the lock content to a temp file in `store/` and hard-links it to `store/lock`; the link fails if the lock exists, so exactly one process gets it, and a live holder's lock is never empty or half-written. If it exists, gsm checks whether the owner still runs (PID and start time, without the process-group rule, since a gsm process is not a server). A dead owner means a crash left it behind, and a lock that isn't valid JSON can only come from a crash or an older gsm: either way gsm deletes it and retries. If it can't delete it, gsm fails right away with the reason. A live owner means gsm polls every 200 ms for up to 30 s, then fails with `snapshot store is locked by another gsm process (pid N)`. Once it holds the lock, gsm writes `store/format` if the store has none.

Commands that write to the store take it: `save snapshot`, the automatic snapshots in `stop`, `run` and `update`, and `save restore`, which holds it across both the safety snapshot and the swap.

### Restore

`save restore` refuses while the server runs. IDs can be any unique prefix. Under the store lock:

1. **Repair an interrupted earlier restore** (table below).
2. **Safety snapshot** of the current saves, trigger `restore`, message `before restoring <id>`.
3. **Stage:** decompress every file of the chosen snapshot into `saves.restore/`, checking size and hash against the record. One mismatch deletes the staging folder and stops; the live saves are untouched.
4. **Swap** with two renames, `saves` to `saves.old` and `saves.restore` to `saves`, then delete `saves.old`. If the second rename fails, the first is undone.

The link in `server/` points at the path `instances/<name>/saves`, so after the swap it reaches the restored folder with no change.

| Crash during | State left on disk | What happens next |
|---|---|---|
| safety snapshot | live saves intact, maybe orphan objects | nothing to repair |
| staging | live saves intact, partial `saves.restore/` | the next restore deletes it first |
| between the two renames | `saves.old/` exists, `saves/` doesn't | the next restore renames `saves.old` back |
| after the swap, before cleanup | both `saves/` (new) and `saves.old/` | the next restore deletes `saves.old` |

## Updates

`update` refuses while the server runs, then: a snapshot with trigger `update` (recording the old build), a config commit (`auto: before update`, since `validate` may write stock files through the config link), steamcmd with `validate` on `server/`, `LinkTo` (or `LinkFileTo`) on both links (it stops if steamcmd replaced a link with a real folder holding files), and the build ID read before and after to report `already up to date` or the change.

## Addons

The `playit` addon runs the playit agent as a podman container:

```
podman run -d --rm --name gsm-<instance>-playit --network=host \
  --env-file ~/.config/playit/env ghcr.io/playit-cloud/playit-agent:latest
```

`start` and `run` start it after the server; `stop` stops it first. Each instance gets its own container name. `addon add` on a running instance starts it right away, and `addon remove` stops it. The env file holds `SECRET_KEY=...`; gsm never copies it and warns if it's missing.

## Atomic writes

`WriteAtomic` (for `state.json`, `instance.toml`, `.gitignore`, snapshot records, `store/format` and a config file's port key), `CopyFile` (the seeded config, `steamclient.so`) and the object writer follow one pattern:

1. create a temp file in the same folder as the destination, so the rename stays on one filesystem
2. write, then `fsync`
3. set permissions
4. rename over the destination (atomic on POSIX; `MoveFileEx` replaces in place on Windows)
5. on any error, delete the temp file

Readers see the complete old file or the complete new one. The containing folder isn't `fsync`ed after the rename, so a power cut might lose the rename itself (the old file remains): the latest snapshot record could be lost, never corrupted.

Other writes are not atomic, on purpose: `logs/server.log` is appended, and restore staging files are verified by hash before the swap.

## Crash safety summary

| Interrupted operation | Worst case | Recovery |
|---|---|---|
| `create` | half-made instance folder | gsm deletes it on error; after a hard crash, delete it by hand |
| writing `instance.toml` / `state.json` | none: old file kept | none needed |
| `start` after launch, before `state.json` | server running, gsm doesn't know | stop it by hand (rare: one write after launch) |
| snapshot | orphan objects, no record | none needed; cleanup (planned) reclaims space |
| restore | see [Restore](#restore) | automatic on the next restore |
| store lock holder crashes | stale lock file | taken over automatically |

## Cross-platform behavior

| Concern | Linux | macOS | Windows |
|---|---|---|---|
| data root | `$XDG_DATA_HOME/gsm` or `~/.local/share/gsm` | `~/Library/Application Support/gsm` | `%LOCALAPPDATA%\gsm` |
| link data into install | symlink | symlink | directory junction, no admin needed |
| detach server | new session (`setsid`) | new session (`setsid`) | new process group + hidden console |
| process start time | `/proc/<pid>/stat` field 22 | `ps -o lstart=` | `GetProcessTimes` |
| survivor detection | process group | process group | launched process only |
| graceful stop | SIGTERM to the group | SIGTERM to the group | Ctrl+Break to the group |
| force stop | SIGKILL to the group | SIGKILL to the group | `taskkill /T /F` |
| Palworld | yes (NixOS: via `steam-run`) | refused at `create` | yes |

**Filesystem.** `$GSM_HOME` must be on a filesystem with links: symlinks or junctions for `LinkTo`, hard links for the store lock. ext4, btrfs, xfs, APFS and NTFS qualify; FAT and exFAT don't.

**NixOS** (detected by `/etc/NIXOS`): Steam binaries need `steam-run`, which gives the server a fresh `/tmp` (so `$GSM_HOME` can't be under `/tmp`), and the server needs `steamclient.so` in `~/.steam/sdk64`. `doctor` checks for both.

**Windows.** There is no SIGTERM; the closest is a Ctrl+Break console event, which `GenerateConsoleCtrlEvent` can send to a process group. The sender must share the server's console, so the server gets a hidden console of its own (`CREATE_NO_WINDOW`). Attaching to it would disturb gsm's own terminal, so gsm starts a short-lived copy of itself (`gsm __gsm-ctrl-break <pid>`, handled by `proc.HandleHelper`) that does `FreeConsole`, `AttachConsole(pid)` and `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid)`. The force-stop fallback, `taskkill /T`, walks the tree by parent PID. It runs when the graceful stop times out, including when the helper failed (for example when it can't attach to the console), which gsm reports as a warning. Directory junctions appear in Go 1.23+ as `ModeIrregular`, not symlinks, which `LinkTo` accounts for. The unit tests for all of this run on Windows in CI; no real server has run on Windows yet.

**macOS.** Few dedicated servers ship macOS binaries, so `create` refuses games without a macOS launch entry. The rest works as on Linux, except the start time lookup.
