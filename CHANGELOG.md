# Changelog

All notable changes to gsm are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before 1.0.0, minor versions may change the CLI or data formats.

## [Unreleased]

### Added

- On Windows, a server and every process it starts run in a Job Object, so a force stop also ends orphaned child processes. From another login session gsm falls back to `taskkill /T` as before.

- Enshrouded dedicated servers (`gsm create enshrouded`): through Wine on Linux, natively on Windows (untested there), with the port set in `enshrouded_server.json`.
- Game servers that ship only for Windows can run on Linux through Wine: gsm installs the Windows build and runs it with `wine` and a Wine prefix per instance. A game can also set the signal that stops its server (for example SIGINT).
- Games whose server keeps its config in one file in the install: gsm links it to `config/<file>` and, for a JSON file with a port key, sets only that key to the instance's port before each start. On Windows this needs file symlinks (Developer Mode or admin).
- Every on-disk format records its version: `instance.toml`, `state.json`, snapshot records and the store (`store/format`). Data from v0.1.0, which has none, reads as version 1; data from a newer gsm is refused with a message to upgrade gsm.

## [0.1.0] - 2026-11-19

The first release: Palworld dedicated servers on Linux and Windows.

### Added

- Palworld dedicated servers as named instances: several per machine, each with its own config, saves and port (`gsm create`, `list`, `game list|show`).
- Server lifecycle without a daemon: `start`, `stop`, `restart`, `run` (foreground), `status [--json]`, `logs [-f]`, and `update` through steamcmd. Servers are found by PID, start time and instance, and stopped as a whole process group, with a force stop as the fallback.
- Config history in a git repo per instance; `start` commits uncommitted edits (`config diff|log|commit`).
- Deduplicated, compressed save snapshots, taken automatically on stop, before update and before restore (`save snapshot|list|restore`). Restore is verified and non-destructive, and every write to the store is crash-safe.
- The playit.gg tunnel as an addon (`addon add|remove`).
- `gsm doctor` checks the machine; `gsm version` prints the version, commit and Go version.
- Release binaries for Linux, Windows and macOS on amd64 and arm64, with SHA-256 checksums.
- Documentation: a user guide, architecture and design docs, a development guide, and contributing, security and conduct policies. MIT license.

[Unreleased]: https://github.com/chopsticks-user/gsm/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/chopsticks-user/gsm/releases/tag/v0.1.0
