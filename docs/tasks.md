# gsm Task Breakdown

The project goal split into small tasks, in build order. Each task should fit in one focused change and ends with a check that proves it works. Milestones build on each other, but tasks inside a milestone can often be done in any order.

The first useful target is milestone 5: by then gsm can replace `../scripts/palworld.sh` for a single Palworld server on Linux. The code for it exists; what's left is a run against the real Palworld server.

## M0. Open decisions

- [x] **Game definitions:** compile-time Go values (`internal/game`). No `games/` folder, no manifest loading.
- [ ] **Dependency approvals.** Go's standard library has none of these, so each needs a decision:
  - [x] TOML parser: `github.com/BurntSushi/toml`
  - [x] zstd compression: `github.com/klauspost/compress`
  - [x] CLI parsing: stdlib `flag`, with a helper that allows flags after positional arguments
  - [ ] SFTP client (M9)
  - [ ] S3 client (M9)
  - [ ] encryption for cloud remotes (M9)
- [ ] **Retention policy format** for `instance.toml`. Blocks prune (M6). `instance.toml` already has a `[retention] keep` stub that nothing enforces yet.
- [x] **Addon definitions:** built into gsm, like games.
- [ ] **Secrets handling:** where secrets live locally, and how they are re-entered after `recover`. Blocks M9. Note: Palworld's `AdminPassword` lives in `PalWorldSettings.ini`, which the config repo tracks.

## M1. Skeleton

- [x] Go module, `cmd/gsm`, and a build that outputs a `gsm` binary.
- [x] Subcommand dispatch. Planned commands that don't exist yet print "not implemented yet".
- [x] `$GSM_HOME` resolution per OS, plus `--home`. Unit test for each OS branch.
- [x] `gsm doctor`: git, steamcmd, and on NixOS steam-run and steamclient.so; podman and the playit env file when an instance uses the addon.

## M2. Games

- [x] Game model: Steam app ID, launch command and paths per OS, args template, default port, defaults, stop timeout, default config file.
- [ ] Test that checks every built-in game: known platforms, port in range, relative paths that can't escape the install directory, args placeholders that all have defaults or come from the instance.
- [x] Palworld definition (app 2394010).
- [x] Args template rendering, rejecting unknown placeholders.
- [x] `gsm game list` and `gsm game show <game>`.
- [ ] Save-flush method in the game model (candidate for Palworld: REST API `/v1/api/save`).

## M3. Instances

- [x] Instance model and `instance.toml` read/write.
- [x] Name rules: unique across all games, and safe as a directory name.
- [x] Port assignment that skips ports used by other instances.
- [x] Platform check: `create` refuses a game that doesn't list the current OS.
- [x] Directory layout, with the instance directory as a git repo limited to `config/` and `instance.toml`.
- [x] steamcmd install into `server/`, with output streamed, and a check that the install happened (steamcmd's exit code is unreliable).
- [x] NixOS setup: copy `steamclient.so` into `~/.steam/sdk64`, skipping a copy onto itself.
- [x] `LinkTo` ported from `../scripts/palworld.sh`, with tests.
- [x] Link `config/` and `saves/` into the server's paths, re-checked on every start and after update.
- [x] `gsm create` with `--port` and `--set k=v`. A failed create removes the half-made instance.
- [x] `gsm list`.
- [ ] `gsm remove <name>`: refuse while running, confirm first, keep snapshots in the store.
- [ ] `gsm import <game> <name> --config DIR --saves DIR`.
  Check: import a copy of the current `../scripts/palworld.sh` data and start it.

## M4. Process supervision

- [x] State file: PID, process start time, instance name, written atomically.
- [x] Start time lookup: `/proc/<pid>/stat` on Linux, `ps` on macOS, `GetProcessTimes` on Windows.
- [x] Identity check, with a test for PID reuse.
- [x] The server counts as running while its process group lives, even if the launched process exited (found with steam-run). Test included.
- [x] Detached launch: `setsid` on Unix, wrapped in `steam-run` on NixOS. Refuses a `$GSM_HOME` under `/tmp` on NixOS, since steam-run hides it.
- [x] `gsm start <name...>` and `--all`, with a check that catches servers that die right after start.
- [x] `gsm stop`: signal the process group, wait, then kill. Tests for an obedient server, a stubborn one, and a wrapper whose child ignores SIGTERM.
- [x] `gsm status` and `--json`.
- [x] `gsm logs <name>` and `-f` (polling, so it works on Windows).
- [x] `gsm restart` and `gsm run` (foreground; Ctrl+C runs the stop sequence).

## M5. Config versioning

- [x] Git wrapper over the `git` CLI, with a fallback author when none is configured.
- [x] `git init` and first commit during `create`.
- [x] Auto-commit on `start`.
- [x] `gsm config diff`, `commit`, `log`.
- [ ] `gsm config edit`.
- [ ] `gsm config restore <rev>`.
- [ ] `gsm config remote`.
- [ ] Real Palworld end-to-end run: create (full download), start, connect from the game, edit config, restart, snapshot, stop, restore.

Milestone check: `../scripts/palworld.sh` can be retired for the existing Palworld server, once the real run passes and `import` exists.

## M6. Snapshot store

- [x] Store lock (O_EXCL file with PID and start time), stale locks taken over. Test included.
- [x] Object write: hash, skip if present, else zstd to a temp file and rename. A file that changes while it's being stored is rejected.
- [x] Snapshot record, written last and atomically. Test that a recordless snapshot doesn't exist.
- [x] `gsm save snapshot [-m msg]`, warning when the server is running.
- [x] Automatic snapshot on `stop` and before `update` and `restore`.
- [x] `gsm save list`.
- [ ] `gsm save show`.
- [ ] `gsm save diff`.
- [ ] `gsm save verify`.
- [x] `gsm save restore`: safety snapshot, stage into `saves.restore/`, verify hashes, swap; an interrupted swap is repaired on the next restore. Tests for corruption and interruption.
- [ ] `gsm save restore --into <new-instance>`.
- [ ] `gsm save pin`, `tag`, `annotate`.
- [ ] `gsm save prune` (needs the retention format from M0).

## M7. Updates

- [x] Read the build ID from the app manifest, recorded as `server_build` in snapshots.
- [x] `gsm update <name>`: require stopped, snapshot, steamcmd with validate, re-link.
- [ ] `gsm update --all`.

## M8. Addons

- [x] Addon list in `instance.toml`, started and stopped with the instance; a failing addon warns without stopping the server.
- [x] `playit` addon: podman container `gsm-<instance>-playit`, `--network=host`, `--rm`, env file `~/.config/playit/env`.
- [x] `gsm addon add` and `remove`.
- [ ] Run the playit addon for real (not exercised in tests so far).

## M9. Remotes and recovery

- [ ] Remote interface: list, upload, download, and atomic writes.
- [ ] Local path remote.
- [ ] `gsm remote add`, `remove`, `push`: upload only the objects the remote lacks, then records, config repos and instance definitions. Exclude secrets.
  Check: a second push with no changes uploads nothing.
- [ ] Automatic push after each snapshot. A failed push warns but doesn't fail the snapshot.
- [ ] Encryption for cloud targets.
- [ ] SFTP remote.
- [ ] S3-compatible remote.
- [ ] `gsm recover`: fetch definitions, clone config repos, reinstall servers, restore the latest snapshots, recreate links, and list the secrets to re-enter.
  Check: recover onto an empty `$GSM_HOME` from a local-path remote and start the server.

## M10. Cross-platform

- [x] Windows: directory junctions (written, untested on Windows).
- [x] Windows: new process group with a hidden console, start time lookup, Ctrl+Break through a helper process, `taskkill /T /F` fallback (written, untested on Windows).
- [ ] Windows: replace the `taskkill` fallback with a Job Object.
- [x] macOS: start time lookup via `ps`, and `create` refuses unsupported games.
- [ ] CI that builds and runs tests on Linux, Windows and macOS. This is the only way to check the Windows code above.
