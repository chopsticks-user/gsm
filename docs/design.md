# Design

Why gsm is built the way it is, what it deliberately doesn't do, and where it's going. For how the pieces work, see [architecture.md](architecture.md).

## Goals

gsm manages self-hosted game servers on one machine, for someone who runs a few servers for friends rather than a hosting business:

- **Several servers per game,** each with its own config, saves and port, managed by name.
- **Nothing to lose.** Config changes are versioned, saves are snapshotted automatically, and a reinstall or game update can't destroy either.
- **One binary on Linux, Windows and macOS,** with no runtime, daemon or service to set up.
- **Off-machine backup and full recovery** on a fresh machine (planned, [#14](https://github.com/chopsticks-user/gsm/issues/14)).

## Non-goals

- **Supervising servers.** gsm doesn't restart a crashed server or schedule restarts. A systemd timer, cron job or Task Scheduler entry calling `gsm start` covers that without a daemon.
- **Hosting at scale:** clusters, containers per server, web panels, billing.
- **Game mods and plugins.**
- **Games without a dedicated server** that steamcmd can install.

## Principles

1. **No daemon.** gsm starts a server and exits; later commands find the server again through a state file. There is no service to install, keep running or secure, and gsm can be upgraded at any time without touching running servers.
2. **The install is disposable.** User data lives outside the steamcmd install and is linked into it, so reinstalling or validating can't touch it. This invariant comes from `scripts/palworld.sh`'s `link_to`, the script gsm grew out of.
3. **Text goes to git, binaries go to the store.** Configs are small text files where diffs and history help, so they go in git. Saves are large binaries rewritten in full, which would bloat git without limit, so they go to a content-addressed, deduplicated, compressed store.
4. **Crash-safe writes.** Every file gsm writes goes to a temp file first and is renamed into place, so a crash leaves the old version or the new one, never half a file. A snapshot exists only once its record is written.
5. **Never guess with user data.** When two copies of a world exist, gsm stops and asks the user to merge, rather than picking one.

## Decisions

| Decision | Chosen | Rejected, and why |
|---|---|---|
| Language | Go: one static binary, easy cross-compiling, a good standard library for processes and files | Bash (what palworld.sh is): Linux only, fragile process handling. Python: needs a runtime. |
| Game definitions | Compile-time Go values in `internal/game`; adding a game means adding a value and rebuilding | Runtime manifests (TOML files in a `games/` folder): a parser, override rules, load-time validation and backup handling, which only pay off once many people add games |
| Addon definitions | Built in, like games | A plugin system, for the same reason |
| Supervision | No daemon; detached processes in their own process group, found through a state file | A daemon or service: something to install, run and secure on three OSes |
| Identifying a server | PID + process start time + instance name, plus the process group | `pgrep -f` by binary path (palworld.sh): can't tell two instances of a game apart, and matches unrelated processes. A PID alone: reused after exit or reboot. |
| Stopping a server | Signal the whole process group | Signal the launched PID: misses the real server when a wrapper (`steam-run`, `PalServer.sh`) runs it as a child |
| Config history | One git repo per instance, through the `git` CLI | A Go git library: a large dependency for a few commands. Snapshotting configs with saves: loses diffs and messages. |
| Save history | Content-addressed store with whole-file dedup and zstd | Git or Git LFS: unbounded growth for binaries rewritten in full. Chunk-level dedup: saves like Palworld's `Level.sav` are already compressed and rewritten entirely, so chunks would rarely match. Plain copies: no dedup for unchanged files. |
| History shape | One straight line per instance; `restore --into` forks a new instance | Branches inside an instance: more concepts than a game server needs |
| Restore | Safety snapshot, stage, verify hashes, swap | Overwriting in place: a failed restore would destroy the live world |
| Data root | Per-OS user data folder (`XDG_DATA_HOME`, Application Support, `%LOCALAPPDATA%`) | `%APPDATA%`: the roaming profile, which can sync gigabytes between machines |
| Data outside the install | Linked in: symlinks on Unix, directory junctions on Windows (no admin needed) | Copying in and out: slow and loses data on a crash. Windows symlinks: need admin or developer mode. |
| CLI parsing | Standard library `flag`, with a helper that allows flags after positional arguments | A CLI framework: a dependency for what `flag` does |
| Dependencies | Standard library first. Approved modules: `github.com/BurntSushi/toml` (instance.toml), `github.com/klauspost/compress` (zstd). Any other module needs approval. | |
| Platforms | A game supports an OS exactly when it has a launch entry for it; `create` refuses others | Pretending to support macOS for games that ship no macOS server |

### Compared with palworld.sh

`scripts/palworld.sh` is the NixOS-only script gsm grew out of, kept for reference:

| | palworld.sh | gsm |
|---|---|---|
| Platforms | Linux (NixOS) | Linux, Windows, macOS |
| Instances | one | any number, names unique across games |
| Finding the server | `pgrep -f` by binary path | state file + start time + process group |
| Stopping | `pkill` by name | signal to that instance's group only |
| Config history | none | git, auto-commit on start |
| Saves | none | deduplicated snapshots, automatic on stop/update/restore |
| Data safety | `link_to` | the same logic, plus atomic writes everywhere |
| Shipping | needs bash, procps, coreutils | one binary |

## Roadmap

Releases are planned as [GitHub milestones](https://github.com/chopsticks-user/gsm/milestones):

- **v0.1.0:** the first public release: end-to-end tests and CI, versioned release builds, and the open source project setup.
- **v0.2.0:** full Enshrouded support ([#19](https://github.com/chopsticks-user/gsm/issues/19)).

Planned features follow the same design but aren't built yet. Each has a backlog issue:

- **Instances** ([#9](https://github.com/chopsticks-user/gsm/issues/9)): `gsm remove <name>` refuses while running, confirms, and keeps the instance's snapshots. `gsm import <game> <name> --config DIR --saves DIR` adopts an existing server's data, for example palworld.sh's.
- **Config** ([#10](https://github.com/chopsticks-user/gsm/issues/10)): `config edit` opens your editor, `config restore <rev>` checks out an old revision, `config remote` pushes the repo somewhere.
- **Snapshots** ([#11](https://github.com/chopsticks-user/gsm/issues/11)): `save show` and `save diff` inspect records; `save verify` re-hashes every object to detect disk corruption; `save restore --into <new>` forks a new instance; `pin`, `tag` and `annotate` edit records, and pinned snapshots survive pruning; `save prune` applies the retention policy to records, then deletes objects no remaining record mentions.
- **Games** ([#8](https://github.com/chopsticks-user/gsm/issues/8)): a test that checks every built-in game definition, and a save-flush method per game (for Palworld, its REST API `/v1/api/save`) so snapshots of a running server are consistent.
- **Updates** ([#12](https://github.com/chopsticks-user/gsm/issues/12)): `gsm update --all`.
- **Addons** ([#13](https://github.com/chopsticks-user/gsm/issues/13)): exercising the playit tunnel for real.
- **Cross-platform** ([#15](https://github.com/chopsticks-user/gsm/issues/15)): CI on Windows and macOS, and a Job Object instead of the `taskkill` fallback on Windows.
- **Off-machine backup and recovery** ([#14](https://github.com/chopsticks-user/gsm/issues/14)):

  ```
  gsm remote add ...        # local path, SFTP or S3-compatible
  gsm remote push
  gsm recover
  ```

  A remote holds a copy of the snapshot store, each instance's config repo and the instance definitions. gsm pushes automatically after each snapshot; a failed push warns but doesn't fail the snapshot. Objects are named by hash and never change, so a push uploads only objects the remote lacks, plus new records. Data sent to cloud targets is encrypted. Secrets (admin passwords, the playit env file) are never uploaded. `gsm recover` on a fresh machine fetches instance definitions, clones config repos, reinstalls each server (`server/` was disposable anyway), restores the latest snapshots, recreates the links, and lists the secrets to enter again.

## Known limitations

- **No save flush:** a snapshot of a running server may miss recent progress. Snapshots taken on `stop` are consistent.
- **Graceful shutdown:** it's unverified whether Palworld saves on SIGTERM. If it doesn't, up to the last autosave interval of progress can be lost on stop.
- **Seeded Palworld settings:** Palworld empties the seeded `PalWorldSettings.ini` on first start ([#20](https://github.com/chopsticks-user/gsm/issues/20)).
- **Port checks:** port assignment checks only other gsm instances, not other programs. Palworld's optional REST API defaults to port 8212, the same port a second instance gets by default.
- **Secrets in git:** `AdminPassword` lives in `PalWorldSettings.ini`, which the local config repo tracks. That must be solved before remotes exist.
- **Snapshot growth:** nothing prunes snapshots yet, and Palworld keeps its own world backups inside `saves/`, which gsm snapshots too.
- **Log size:** `logs/server.log` grows forever.
- **Windows:** the code is untested on real Windows.
- **Power loss:** the folder isn't `fsync`ed after an atomic rename, so a power cut can lose the latest write (never corrupt it).

## Open questions

- **Retention policy format** for `instance.toml`. Blocks `save prune` ([#11](https://github.com/chopsticks-user/gsm/issues/11)). `instance.toml` already has a `[retention] keep` stub that nothing enforces.
- **Secrets:** where secrets live locally, and how they're re-entered after `recover`. Blocks remotes ([#14](https://github.com/chopsticks-user/gsm/issues/14)).
- **Encryption scheme** for cloud remotes.
- **Dependencies for remotes:** an SFTP client, an S3 client and an encryption library each need approval ([#14](https://github.com/chopsticks-user/gsm/issues/14)).
