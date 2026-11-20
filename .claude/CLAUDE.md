# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`gsm` is a cross-platform (Linux, Windows, macOS) CLI for managing self-hosted game servers: several named instances per game, git-tracked configs and deduplicated save snapshots, with no daemon. Palworld is the only game so far. `scripts/palworld.sh` is the NixOS-only Bash script gsm grew out of, kept for reference.

## Docs

Read the doc that matches the task before changing code, and update it in the same change:

- `docs/guide.md`: using gsm, the walkthrough and command reference
- `docs/architecture.md`: how it's built: code map, data layout, file formats, every command's algorithm, per-OS behavior
- `docs/design.md`: why: goals, decisions with rejected alternatives, roadmap, known limitations, open questions
- `docs/development.md`: dev shell, verify, test conventions, adding a game, workflow, labels

Each fact lives in one doc. The rules below are short forms of decisions in those docs, kept here because agents must follow them; change the doc first, then the rule.

## Rules

- gsm is one Go binary, and that binary is the only public interface.
- Games and addons are compile-time Go values (`internal/game`, `internal/addon`). No runtime manifests, no `games/` folder.
- No daemon. A server is identified by its state file's PID, process start time and instance name, plus its process group. Never identify a server by binary name (two instances share a binary), and always signal the whole process group.
- User data (`config/`, `saves/`) never lives inside `server/`; it's linked in with `fsutil.LinkTo`. Never merge or pick between two copies of user data: stop with an error.
- Configs go in the instance's git repo; saves go to the snapshot store, never git. State, metadata and snapshot files are written through a temp file and an atomic rename (exceptions in `docs/architecture.md`, Atomic writes); a snapshot record is written last.
- Only `cmd/gsm` prints or parses flags; `internal` packages return errors.
- Approved Go modules: `github.com/BurntSushi/toml`, `github.com/klauspost/compress`. Ask before adding any other.
- Never use `%APPDATA%` on Windows (roaming profile). On NixOS, `$GSM_HOME` can't be under `/tmp` when servers run through `steam-run`.
- Tests use the real filesystem and fake processes by re-running the test binary from `TestMain` (see `internal/proc/proc_test.go`), never mocks.

## Tooling

Go is not installed globally; use the flake's dev shell (details in `docs/development.md`):

- Build: `nix develop path:./flake -c go build ./cmd/gsm`
- Test: `nix develop path:./flake -c go test -race ./...`
- Verify (scripts syntax, gofmt, go vet for Linux/Windows/macOS, race tests including the end-to-end test; CI runs the same): `nix develop path:./flake -c scripts/verify.sh`
- Smoke test against the real Palworld server (5 GB download, run before a release): `nix develop path:./flake -c scripts/smoke-palworld.sh`

`git`, `steamcmd`, `steam-run` and `podman` are on the dev host.

## Workflow

GitHub: `chopsticks-user/gsm` (private until v0.1.0). Base branch is `main`. The issue/PR workflow is in `docs/development.md`.

Labels in use: `type:bug`, `type:feature`, `type:chore`, `type:docs` (one per issue); `P0` (outage, data loss, security) to `P3` (minor), one per issue; `needs-info`; `ready`.

## taskie

verify: nix develop path:./flake -c scripts/verify.sh
setup: none
