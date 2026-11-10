# How gsm Works

This document walks through gsm from a user's perspective, following one user from install to disaster recovery, with the technical reasoning behind each behavior.

Status: a first working subset exists for Palworld: doctor, game, create, list, start, stop, restart, run, status, logs, update, config diff/log/commit, save snapshot/list/restore, and the playit addon. The rest (remotes, recover, prune, import, remove and others) is design only; `docs/tasks.md` tracks it. Points the design has not decided are marked as open.

## 1. Installing and the home directory

You install one binary, `gsm`. Nothing runs in the background, so there is no service to enable. On first run gsm picks a data root, `$GSM_HOME`:

- Linux: `$XDG_DATA_HOME/gsm`, falling back to `~/.local/share/gsm`
- macOS: `~/Library/Application Support/gsm`
- Windows: `%LOCALAPPDATA%\gsm`. It never uses `%APPDATA%`, because that is the roaming profile and it would try to sync gigabytes of server files between machines.

`--home` overrides it, which lets you keep separate, isolated setups side by side.

`gsm doctor` checks that the tools gsm depends on are present: `git`, `steamcmd`, and on NixOS `steam-run` and `~/.steam/sdk64/steamclient.so`. Run it first.

## 2. Picking a game

```
gsm game list
gsm game show palworld
```

A game is a Go value compiled into the binary (`internal/game`). The code that installs, launches and stops servers is the same for every game; the game definition only supplies the facts that differ. Supporting a new game means adding a value and making a new release. Runtime manifests were considered and rejected for now: they need a parser, override rules and backup handling, which only pay off if other people add games.

A game definition describes:

- the Steam app ID (2394010 for Palworld)
- which OSes have a server binary
- the launch command for each OS, plus an args template that gets filled with the instance's ports and settings
- default ports
- where configs and saves live inside the install
- how to stop the server (signal and timeout, like `../scripts/palworld.sh`'s SIGTERM, 30s, then SIGKILL)
- optionally, a save-flush method that makes a running server write its world to disk (for example an RCON `save` command)

## 3. Creating an instance

```
gsm create palworld main --port 8211 --set ServerName=Friends
```

An instance is one named server. Names are unique across all games, so every later command takes just `main`, never `palworld main`. You can run `main` and `test` from the same game side by side.

What `create` does under the hood:

1. It checks the game lists your OS. On macOS most games don't, and `create` refuses rather than failing later.
2. It assigns ports. If 8211 is already taken by another instance, it picks the next free one, so two instances never collide.
3. It builds the layout:
   ```
   instances/main/   git repo that tracks only config/ and instance.toml
     instance.toml   game, port, settings, addons, retention policy
     config/         server config
     saves/          live save data
     server/         steamcmd install (disposable)
     logs/
   ```
4. It runs steamcmd to install the server into `server/`.
5. It links `config/` and `saves/` into the paths the game expects inside `server/`, using symlinks on Unix and directory junctions on Windows. This is the key safety rule carried over from `../scripts/palworld.sh`: your data never lives inside `server/`, so a reinstall or a steamcmd `validate` can wipe `server/` and lose nothing. If the game already created a real directory there, gsm moves its contents into `config/` or `saves/` first, and aborts if both sides have files rather than guess (`../scripts/palworld.sh`'s `link_to`).
6. It runs `git init` in the instance directory, whose `.gitignore` limits the repo to `config/` and `instance.toml`, and commits them. For Palworld it first copies the server's `DefaultPalWorldSettings.ini` into `config/PalWorldSettings.ini`, so there is something to edit.

If you already run a server by hand, `gsm import palworld main --config DIR --saves DIR` adopts its existing data the same way.

## 4. Starting, stopping, watching

```
gsm start main            # or: gsm start main test, or --all
gsm status [--json]
gsm logs main -f
gsm stop main
gsm restart main
gsm run main              # foreground, for debugging
```

**Start.** gsm first auto-commits any uncommitted config edits, so each server run maps to an exact config revision. That revision is recorded later in snapshots. Then it launches the server detached from your terminal: in a new session via `setsid` on Linux (wrapped in `steam-run` on NixOS), in a new process group on Windows. Output goes to `logs/`. gsm then exits. Nothing supervises the server, so if it crashes it stays down until you start it again.

**Tracking without a daemon.** Each instance gets a state file holding three values: the PID, the process start time, and the instance ID. For `status`, `stop` or `logs`, gsm checks all three before treating that PID as the server:

- the PID alone is not enough, because after a reboot the OS can reuse that number for an unrelated process
- the start time catches reuse, because a recycled PID will have a different start time
- the instance ID matters because `main` and `test` run the same binary. `../scripts/palworld.sh` finds its server with `pgrep -f`, which can't tell two Palworld servers apart and could kill the wrong one.

The server runs in its own process group, and gsm signals the whole group. That matters because the process gsm launches is often a wrapper: `PalServer.sh` starts the real binary as a child, and on NixOS `steam-run` exits while the server keeps running. So the server counts as running while its process group is alive, even if the launched process is gone. This is safe because the OS never reuses a PID that still names a live process group.

**Stop.** gsm sends the game's stop signal, waits for the timeout, and sends a hard kill if the server is still alive. Then it takes an automatic snapshot (trigger `stop`), so every shutdown leaves a restore point.

## 5. Config management

```
gsm config edit main      # opens your editor
gsm config diff main
gsm config commit main -m "raise exp rate"
gsm config log main
gsm config restore main <rev>
gsm config remote main ...
```

This is a thin layer over a normal git repo, driven by calling the `git` CLI. Configs are small text files, so git is the right tool: real diffs, history and blame. Because `start` auto-commits, history stays complete even if you edit files directly and never run `config commit`.

## 6. Save snapshots

```
gsm save snapshot main -m "before boss fight"
gsm save list main
gsm save show|diff main <id>
gsm save restore main <id> [--into newname]
gsm save prune main
gsm save pin|tag|annotate main <id> ...
gsm save verify main
```

Saves do not go into git. They are large binaries that change completely on every save, so git history would grow without limit. They go to a shared store instead:

```
store/
  objects/ab/ab12...ef      zstd-compressed file content
  snapshots/main/20261109T120000-x7k2.json
  lock
```

**How a snapshot is taken.** gsm walks `saves/` and computes the SHA-256 of each file's raw bytes. If `objects/<first 2 hex chars>/<hash>` already exists, the file is unchanged since some earlier snapshot, of any instance, and isn't stored again. Otherwise gsm zstd-compresses it to a temp file and renames it into place. A rename is atomic, so a crash can't leave a half-written object under a valid name. Last, it writes the JSON record: ID, instance, game, time, message, trigger (`manual`, `stop`, `update`, `restore`), the config git revision, the server build, tags, the pinned flag, and a list of files with path, size, mode and hash.

The record is written last on purpose: a snapshot exists only once its record does. If gsm crashes halfway, you get a few orphan objects that cleanup later removes, never a corrupt snapshot.

**Why whole files, not chunks.** Palworld's `Level.sav` is already compressed and rewritten in full on every save, so chunking to find shared pieces would gain almost nothing. Small files that don't change, like player files and metadata, still dedupe for free.

**Snapshots of a running server.** If the game has a save-flush method, gsm triggers it first so the files on disk are current. Without one, gsm warns that the snapshot may be stale or caught mid-write.

**Restore** requires the instance to be stopped, and it is non-destructive:

1. take a safety snapshot of the current saves (trigger `restore`)
2. write the chosen snapshot's files into `saves.restore/`
3. check every file's hash against the record
4. swap the directories only if every hash matches

If anything fails, your live saves are untouched. `--into newname` instead creates a new instance from the snapshot, which is handy for trying something on a copy of the world. History is one straight line per instance, with no branches.

**Prune** applies the retention policy from `instance.toml` to snapshot records, skipping pinned ones, then deletes objects that no remaining record refers to. Open: the retention policy's format. `verify` re-hashes stored objects to detect disk corruption.

**Concurrency.** `store/lock` lets only one gsm process write to the store at a time, so a cron snapshot and a manual `stop` can't corrupt each other.

## 7. Updating the server

```
gsm update main           # or --all
```

This requires the instance to be stopped. gsm takes a snapshot (trigger `update`), then runs steamcmd on `server/`. Because your data is only linked into `server/`, the update can't touch it. Snapshots record `server_build`, so if an update breaks your world you can see which build last worked and restore that snapshot.

## 8. Addons

```
gsm addon add main playit
```

An addon is a companion process whose lifecycle follows the instance: it starts with `start` and stops with `stop`. The first one is `playit`, the tunnel `../scripts/palworld.sh` already runs as a podman container (`--network=host --rm`, env file `~/.config/playit/env`). It's stored in `instance.toml`. Addons are built into gsm, like games.

## 9. Off-machine backup and recovery

```
gsm remote add ...        # local path, SFTP or S3-compatible
gsm remote push
gsm recover
```

A remote holds a copy of three things: the snapshot store, each instance's config repo, and the instance definitions. After every snapshot gsm pushes automatically, so you don't have to remember. The store design suits this well: objects are named by hash and never change, so a push only uploads objects the remote lacks, plus the new JSON records. Data sent to cloud targets is encrypted. Secrets, such as admin passwords or the playit env file, are never uploaded.

`gsm recover` on a fresh machine, with only the remote configured, rebuilds everything: it downloads instance definitions, clones config repos, reinstalls each server via steamcmd (since `server/` was disposable anyway), restores the latest snapshot into `saves/`, and recreates the links. You then supply the secrets again by hand.

## How the pieces fit

| Data | Size and shape | Where it lives | Why |
|---|---|---|---|
| Server binaries | Large, downloadable | `server/` | Disposable, so steamcmd can rebuild it any time |
| Configs, `instance.toml` | Small text | git repo | Readable diffs and history |
| Saves | Large binaries, fully rewritten | content-addressed store | Dedupe and compression, without git bloat |
| Runtime state | PID, start time, ID | state file | Safe process tracking with no daemon |
| Secrets | Tiny | local only | Never leave the machine |

Each snapshot records the config revision and server build, so any snapshot tells you exactly which config and which game version produced that world.

## Open questions

- Retention policy format
- Encryption scheme for cloud remotes
- How secrets are stored locally and re-entered after `recover`
