# Roadmap

Where gsm is going, in what order, and why. Each release below is a [GitHub milestone](https://github.com/chopsticks-user/gsm/milestones), which holds its issues; this file holds what the issue lists can't: the theme, the reasoning behind the order, and the exit criteria that say when a release is done. How versions and milestones work is in [development.md](development.md#versioning). What the planned features look like is in [design.md](design.md#planned-features).

Every minor release delivers one capability that changes what gsm is good for. A single feature, like one more game, isn't a release on its own.

## v0.1.0: First public release

[Milestone](https://github.com/chopsticks-user/gsm/milestone/1)

Palworld on Linux and Windows: the full server lifecycle, config history, deduplicated snapshots and the playit addon, as a tested, released open source project.

**Done when:** the end-to-end test and CI guard the lifecycle, the smoke test passes against the real server and a game client connects, release binaries are published for every supported OS and CPU, and the repo meets GitHub's community standards.

## v0.2.0: More than one game

[Milestone](https://github.com/chopsticks-user/gsm/milestone/2)

The game model grows from "Palworld-shaped" to general: servers that only ship for Windows (run through Wine or Proton on Linux), configs that aren't INI files, and per-game save layouts. Enshrouded is the second game and the proof. Because its server is Windows-only, Windows support becomes real: tested in CI and on Windows itself rather than only compiled.

**Why now:** Enshrouded support is wanted right after the first release. It's also what forces the game model to become general, and every later game is cheaper for it.

**Why the format versions come first:** multi-game support is where `instance.toml` and the snapshot records start to change. Before that, every on-disk format needs a version, so data written by v0.1.0 is recognized and data from a newer gsm is refused clearly.

**Done when:**

- every on-disk format carries a version, and v0.1.0 data still works
- every built-in game definition is checked by a test
- `gsm create enshrouded` and the whole lifecycle work on Linux and Windows, checked by a smoke test against the real server
- CI runs on Linux, Windows and macOS

## v0.3.0: Run one server for months unattended

[Milestone](https://github.com/chopsticks-user/gsm/milestone/3)

Everything a long-running server needs so its owner never has to clean up by hand: snapshots pruned by a retention policy, saves flushed before live snapshots, logs rotated, ports checked against other programs, and the remaining instance and config commands (`remove`, `import`, `config edit` and `restore`, `update --all`). `import` is what lets palworld.sh's world move into gsm.

**Why here:** with two games working, the next most valuable thing is that a server can run for months without the disk filling up or snapshots missing progress.

**Done when:** nothing in `$GSM_HOME` grows without bound, a live snapshot is consistent, an existing server's data can be imported, and `remove`, `import`, `config edit`, `config restore` and `update --all` exist (`config remote` comes with the other remotes in v0.4.0).

## v0.4.0: Survive losing the machine

[Milestone](https://github.com/chopsticks-user/gsm/milestone/4)

Off-machine copies of everything gsm keeps (snapshot store, config repos, instance definitions) on a local path, SFTP or S3-compatible storage, encrypted for cloud targets, pushed after every snapshot, and `gsm recover` to rebuild every instance on a fresh machine. Secrets get a home outside the config repos first, since they must never be uploaded.

**Why last before 1.0:** it needs decisions still open in [design.md](design.md#open-questions) (secrets handling, encryption, and approval of SFTP, S3 and encryption libraries), and it builds on stable formats and pruning, since an unpruned store is expensive to copy.

**Done when:** `gsm recover` on a fresh machine brings back every instance from a remote, with no secret ever uploaded.

## v1.0.0: Stable

[Milestone](https://github.com/chopsticks-user/gsm/milestone/5)

The promise release: the CLI and the on-disk formats are frozen, and any later format change comes with an automatic migration.

**Done when:** every known limitation and open question in [design.md](design.md) is resolved or explicitly accepted, the docs match the frozen behavior, and the playit addon has been exercised against a real tunnel.

## Not planned

Issues that don't fit any theme stay without a milestone until one fits, and are listed here:

- [#74](https://github.com/chopsticks-user/gsm/issues/74) Game-specific tools as gsm subcommands, starting with save editing
- [#75](https://github.com/chopsticks-user/gsm/issues/75) Run the Linux smoke tests in a container from CI
- [#77](https://github.com/chopsticks-user/gsm/issues/77) Broaden the test suites and measure coverage, with sub-issues [#100](https://github.com/chopsticks-user/gsm/issues/100) and [#102](https://github.com/chopsticks-user/gsm/issues/102)
- [#78](https://github.com/chopsticks-user/gsm/issues/78) Project agent skills (gsm:*) for developer guidance and chores
- [#79](https://github.com/chopsticks-user/gsm/issues/79) Narrow the Nix package source to the Go files
- [#104](https://github.com/chopsticks-user/gsm/issues/104) A launch whose state file cannot be written hangs 15 s on an unreaped server
- [#106](https://github.com/chopsticks-user/gsm/issues/106) gsm save restore with an empty ID restores the only snapshot
