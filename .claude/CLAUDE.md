# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`gsm` is a cross-platform (Linux, Windows, macOS) CLI for managing self-hosted game servers: several named instances per game, git-tracked configs and deduplicated save snapshots, with no daemon. Games so far: Palworld and Enshrouded (through Wine on Linux). `scripts/palworld.sh` is the NixOS-only Bash script gsm grew out of, kept for reference.

## Docs

Read the doc that matches the task before changing code, and update it in the same change:

- `docs/guide.md`: using gsm, the walkthrough and command reference
- `docs/architecture.md`: how it's built: code map, data layout, file formats, every command's algorithm, per-OS behavior
- `docs/design.md`: why: goals, decisions with rejected alternatives, planned features, known limitations, open questions
- `docs/roadmap.md`: the releases ahead (v0.2.0 to v1.0.0): theme, order and exit criteria each; GitHub milestones hold the issues
- `docs/development.md`: dev shell, build, verify, test conventions, adding a game, versioning (patch rule, release branches, milestones), releasing, workflow, labels
- Root files: `README.md` (overview, install), `CHANGELOG.md` (per release), `CONTRIBUTING.md` (for contributors, defers to development.md), `SECURITY.md` (vulnerability reporting), `CODE_OF_CONDUCT.md`, `LICENSE` (MIT)

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

Go is not installed globally; use the flake's dev shell (details in `docs/development.md`). The `justfile` wraps these as recipes (`just` lists them; the table is in `docs/development.md`, Common commands):

- Build (all output goes to `build/`): `nix develop -c go build -o build/ ./cmd/gsm`; install: `just install [dir]`; Nix package: `nix build .#default`
- Release binaries into `build/release/`: `nix develop -c scripts/release-build.sh v1.2.3`
- Test: `nix develop -c go test -race ./...`
- Verify (scripts syntax, gofmt, go vet for Linux/Windows/macOS, race tests including the end-to-end test; CI runs the same): `nix develop -c scripts/verify.sh`
- Smoke tests against the real servers (run before a release): `nix develop -c scripts/smoke-palworld.sh` (5 GB download) and `nix develop -c scripts/smoke-enshrouded.sh` (2 x 7.6 GB, needs wine)
- Vulnerability scan (CI runs it for Linux, Windows and macOS; needs the network): `nix develop -c govulncheck -test ./...`
- After changing Go dependencies, run `scripts/update-vendorhash.sh` (the Nix package's `vendorHash`; CI fixes it on Dependabot PRs)
- A stdlib vulnerability or a Dependabot `go` bump that fails CI is fixed by `nix flake update` (Dependabot doesn't update `flake.lock`)

`git`, `steamcmd`, `steam-run` and `podman` are on the dev host.

## Workflow

GitHub: `chopsticks-user/gsm` (public). Base branch is `main`, protected by a ruleset (PR with passing CI, merge commits only; release tags admin-only); patches go through `release-X.Y` branches. The issue/PR workflow and the versioning policy are in `docs/development.md`. Every open issue belongs to a milestone (see `docs/roadmap.md`) or is listed there as not planned.

Labels in use: `type:bug`, `type:feature`, `type:chore`, `type:docs` (one per issue); `P0` (outage, data loss, security) to `P3` (minor), one per issue; `needs-info`; `ready`.

## taskie

verify: nix develop -c scripts/verify.sh
setup: none
