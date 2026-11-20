# Changelog

All notable changes to gsm are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before 1.0.0, minor versions may change the CLI or data formats.

## [Unreleased]

The first release, v0.1.0, is in progress ([milestone](https://github.com/chopsticks-user/gsm/milestone/1)).

### Added

- Palworld dedicated servers on Linux and Windows, as named instances: several per machine, each with its own config, saves and port (`gsm create`, `list`, `game list|show`).
- Server lifecycle without a daemon: `start`, `stop`, `restart`, `run` (foreground), `status [--json]`, `logs [-f]`, and `update` through steamcmd. Servers are found by PID, start time and instance, and stopped as a whole process group.
- Config history in a git repo per instance; `start` commits uncommitted edits (`config diff|log|commit`).
- Deduplicated, compressed save snapshots, taken automatically on stop, before update and before restore (`save snapshot|list|restore`). Restore is verified and non-destructive.
- The playit.gg tunnel as an addon (`addon add|remove`).
- `gsm doctor` checks the machine; `gsm version` prints the version, commit and Go version.
- Release binaries for Linux, Windows and macOS on amd64 and arm64, with SHA-256 checksums.

### Fixed

- An empty or corrupt store lock left by a crash no longer blocks every snapshot ([#22](https://github.com/chopsticks-user/gsm/issues/22)).
- Stop falls back to a force stop when the stop request can't be sent ([#23](https://github.com/chopsticks-user/gsm/issues/23)).
- `game show` no longer lists `port` as a `--set` setting ([#24](https://github.com/chopsticks-user/gsm/issues/24)).
- Copies of the seeded config and `steamclient.so` are written atomically ([#25](https://github.com/chopsticks-user/gsm/issues/25)).

[Unreleased]: https://github.com/chopsticks-user/gsm/commits/main
