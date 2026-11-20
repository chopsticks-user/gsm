# Development

How to build, test and change gsm. For how the code is organized, see [architecture.md](architecture.md); for why, see [design.md](design.md).

## Dev environment

The repo ships a Nix flake with the toolchain: Go, gopls, gcc (the race detector needs cgo), govulncheck and just.

```
nix develop path:./flake            # a shell with the toolchain
nix develop path:./flake -c <cmd>   # one command inside it
```

`path:` makes Nix copy only `flake/` into the store, not the whole repo, and lets it see the flake files before they're committed.

Entered from inside the repo, the shell keeps Go's module and build caches in a gitignored `.go/` at the repo root instead of `~/go` and `~/.cache/go-build`, and sets `GOFLAGS=-modcacherw` so `.go/` can be deleted normally. Entered from outside a git repo, it warns and leaves Go's defaults.

`nix run path:./flake#install` adds a desktop entry, "VS Code (gsm)", that opens the repo in VS Code inside the shell, so gopls and the Go extension see the toolchain. `nix run path:./flake#uninstall` removes it. The entry is written to `~/.local/share/applications`; on an impermanent home it survives a reboot only if that folder is persisted. Close every VS Code window before using it, since `code` hands the folder to a running instance, which keeps its old environment.

Without Nix, any Go matching the `go` line in `go.mod` works; you'll also need gcc for `-race`.

Tools gsm calls at runtime come from the host, not the flake: `git`, `steamcmd`, and on NixOS `steam-run`; `podman` for the playit addon.

## Common commands

The `justfile` has recipes for the commands below; inside the dev shell run `just <recipe>`, or from outside it `nix develop path:./flake -c just <recipe>`. `just` alone lists them.

| Recipe | Runs |
|---|---|
| `just build` | `go build -o build/ ./cmd/gsm` |
| `just test` | `go test -race ./...` |
| `just verify` | `scripts/verify.sh` (what CI runs) |
| `just fmt` | `go fmt ./...` |
| `just vuln` | `govulncheck -test ./...` for Linux, Windows and macOS, as CI does |
| `just smoke [--hold]` | `scripts/smoke-palworld.sh` |
| `just release-build <version>` | `scripts/release-build.sh` |
| `just release-notes <version>` | `scripts/release-notes.sh` |
| `just clean` | removes `build/` |

The sections below give the full commands and what each one does.

## Build

```
nix develop path:./flake -c go build -o build/ ./cmd/gsm
nix develop path:./flake -c env GOOS=windows go build -o build/ ./cmd/gsm
nix develop path:./flake -c env GOOS=darwin GOARCH=arm64 go build -o build/gsm-darwin-arm64 ./cmd/gsm
```

All build output goes to the gitignored `build/`. Release binaries for every OS and CPU go to `build/release/` (see [Releasing](#releasing)). `gsm version` prints the version, the commit and the Go version; a build without a release version says `dev`, and a build from uncommitted changes marks the commit `-dirty`.

## Verify

One command runs everything a change must pass:

```
nix develop path:./flake -c scripts/verify.sh
```

It syntax-checks the shell scripts in `scripts/`, checks `gofmt` on every package, runs `go vet` for Linux, Windows and macOS, and runs `go test -race ./...`, which includes the end-to-end test. CI runs the same command. Run it before and after a change, and before merging.

## Tests

- **Real filesystem, no mocks.** Tests work in `t.TempDir()` folders.
- **Fake processes by re-running the test binary.** A package's `TestMain` checks an environment variable and, when set, acts as a fake instead of running tests: an obedient server (exits on SIGTERM), a stubborn one (ignores it), a wrapper that starts a child like `PalServer.sh` does, optionally exiting early like `steam-run`, or a fake steamcmd. See `internal/proc/proc_test.go`. This works the same on every OS.
- **OS branches as parameters.** Code like `$GSM_HOME` resolution takes the OS name and environment as arguments, so one machine can test every branch.
- **Per-OS tests** go in `*_unix_test.go` or `*_windows_test.go`, next to the code.

### End-to-end test

`cmd/gsm/e2e_test.go` drives the real CLI through a whole lifecycle: create (with a port conflict), start, `status --json`, `config diff`, restart with its auto-commit, a live snapshot, stop, restore over a corrupted save, update to a new build, and `addon add|remove`. No network, Steam or game is involved. The test binary plays every part, chosen by `GSM_E2E_ROLE`: gsm itself, a fake steamcmd that lays out a Palworld-shaped install, and a fake server (started through a `PalServer.sh` that runs it as a child, like the real one) that rewrites a save every 100 ms and writes a final save on SIGTERM. Shell shims named `steamcmd`, `steam-run` and `podman` come first on `PATH`, so gsm runs as in production; `podman` always fails, so no container is ever started. The test sets its own `HOME`, puts its data outside `/tmp` on NixOS, and stops every server it started, even when it fails; a fake server also exits on its own after two minutes, in case the test dies without its cleanup. It needs a POSIX shell, so it builds only on Unix.

An earlier end-to-end run like this found two bugs unit tests had missed: the early-exiting `steam-run` wrapper, and `steam-run` hiding `/tmp`. Behavior that spans processes deserves a step in this test.

On NixOS, keep test data out of `/tmp` when a test starts a server through `steam-run`, which hides `/tmp` from the server.

### Smoke test against the real server

```
nix develop path:./flake -c scripts/smoke-palworld.sh [--hold]
```

Repeats the lifecycle against the real Palworld dedicated server: it downloads it (about 5 GB) into a throwaway home under `~/.cache`, edits `ServerName` in the seeded settings, waits for the server to listen and save, checks the edit survived, snapshots, stops, restores over a corrupted `Level.sav` and checks the hash, starts again, and updates. The throwaway home is deleted at the end, also on failure. `--hold` pauses while the server runs, so you can connect with a game client. It needs steamcmd (plus `steam-run` and `steamclient.so` on NixOS) and a free UDP port 8211, so it doesn't run in CI; run it before a release.

### CI

`.github/workflows/ci.yml` runs verify through the flake on `ubuntu-latest` for every pull request and every push to `main`, the same command as locally. Linux only for now; Windows and macOS runners are [#15](https://github.com/chopsticks-user/gsm/issues/15).

CI then runs `govulncheck -test ./...` for Linux, Windows and macOS, which fails on a known vulnerability in code gsm actually calls, tests included (one in a dependency function gsm never calls is reported but doesn't fail). It needs the network for the vulnerability database, so it's a CI step rather than part of verify. CI also runs weekly on a schedule, so a vulnerability published against unchanged code shows up without a pull request. Run it locally with `nix develop path:./flake -c govulncheck -test ./...`.

Dependabot (`.github/dependabot.yml`) opens weekly pull requests for Go module and GitHub Actions updates, one grouped PR per ecosystem, which CI checks like any other.

GitHub also checks the repo on its own, outside the workflows: Dependabot alerts and security-fix pull requests for vulnerable dependencies, secret scanning with push protection (a push containing a token or key is rejected), and CodeQL code scanning of the Go code and the workflows. Findings show under the repo's Security tab; vulnerabilities reported privately ([SECURITY.md](../SECURITY.md)) land there too.

Two things nothing updates automatically, and both have the same fix:

- **A vulnerability in Go's standard library.** govulncheck checks the standard library of the Go the flake provides, so a stdlib advisory fails CI on every PR, even ones that touch no Go code, until a newer Go is in the flake.
- **A Dependabot PR that raises `go.mod`'s `go` line** past the flake's Go fails, since the flake's Go doesn't download newer toolchains.

The fix is updating the flake inputs (`flake/flake.lock`), which Dependabot doesn't support: `nix flake update --flake path:./flake`, then verify and open a PR.

## Adding a game

1. Add a `game.Game` value in `internal/game/game.go` (like `Palworld`) and append it to the `all` list there: Steam app ID, a launch entry per supported OS (command, config dir and saves dir inside the install, written with `/`), the args template with its defaults, the default port, the stop timeout, and an optional default config file.
2. If the game needs something the model lacks (a different launcher such as Wine, a config format gsm must edit, a save-flush method), extend the model in `internal/game` and the code that uses it in `cmd/gsm`, keeping existing games unchanged. Extend `validate` in `internal/game/game_test.go` to check the new fields.
3. Run the tests: `TestBuiltinGamesAreValid` checks every game in `all` (names, app ID, platforms, paths that stay inside the install, config and saves dirs that don't overlap, port, stop timeout, every placeholder has a default and every default is used). Check the result with `gsm game show <game>`, then run the whole lifecycle against a real install: `create`, `start`, `status`, `stop`, `save restore`, `update`.
4. Document the game in [guide.md](guide.md) and any new limitation in [design.md](design.md#known-limitations).

## Versioning

gsm follows [Semantic Versioning](https://semver.org) with a `v` prefix: `v0.1.0`, `v0.1.1`, `v1.0.0`; `v0.2.0-rc.1` for a pre-release.

**Minor versions are planned; patch versions aren't.** Each minor release is a GitHub milestone with one theme and exit criteria in [roadmap.md](roadmap.md). A patch release fixes what's already released, when a fix can't wait for the next minor.

| | Before 1.0 | From 1.0 |
|---|---|---|
| Major `X.0.0` | (stays 0) | a breaking CLI change, or an on-disk format change that needs a migration |
| Minor `0.X.0` / `1.X.0` | one milestone's capability; may change the CLI or on-disk formats, noted in the CHANGELOG | new features, backwards compatible |
| Patch `0.x.Y` / `1.x.Y` | fixes only | fixes only |

**Patch rule.** A patch fixes a regression or a bug in a released feature. It never adds a feature or changes the CLI or an on-disk format. Cut one when a fix for a `P0` or `P1` issue lands (see [Labels](#labels)); `P2` and `P3` fixes wait for the next minor. Patches have no milestones.

Patches come from a release branch, not `main`, because `main` already carries unfinished work for the next minor:

1. The fix goes to `main` first, through the normal issue and pull request.
2. The first patch of a minor creates its branch, `release-X.Y`, from that minor's last tag, for example `git switch -c release-0.1 v0.1.0`, and pushes it. Later patches of the same minor reuse it.
3. Cherry-pick the fix onto the release branch (`git cherry-pick -x <commit>`) in a pull request against `release-X.Y`, and add the patch's CHANGELOG section there.
4. Tag the release branch's merge commit and push the tag, as in [Releasing](#releasing). The release workflow accepts a patch tag `vX.Y.Z` on its own `release-X.Y` branch, and doesn't mark it as the latest release unless it's also on `main`.
5. Bring the CHANGELOG section to `main` by cherry-picking that one commit, so `main` keeps the full history and the section is written once.

**Milestones.**

- There's one milestone per minor version, plus v1.0.0. Its description is a one-line theme; the full theme, order and exit criteria live in [roadmap.md](roadmap.md), and the milestone holds the issues.
- An issue joins a milestone when it's needed for that milestone's exit criteria, not just because it would be nice. Issues that fit no theme stay without a milestone and are listed under Not planned in roadmap.md.
- An issue that won't make its release moves to the next milestone; if that changes what the release delivers, roadmap.md changes in the same step.
- A release is cut when its milestone has no open issues and its exit criteria hold.

## Releasing

What a version number means is in [Versioning](#versioning). To cut a release:

1. For a minor release: every issue in its [milestone](https://github.com/chopsticks-user/gsm/milestones) is closed, its exit criteria in [roadmap.md](roadmap.md) hold, and CI is green on `main`. For a patch: the fixes are cherry-picked onto `release-X.Y` (see [Versioning](#versioning)).
2. Run the smoke test with a game client: `nix develop path:./flake -c scripts/smoke-palworld.sh --hold`, and connect to the server on UDP 8211 while it holds.
3. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, start a new empty `## [Unreleased]` above it, update the links at the bottom, and merge that through a pull request. For a patch, the section goes on the release branch first (steps in [Versioning](#versioning)). A pre-release (`vX.Y.Z-rc.N`) can skip this: without a section of its own, its notes come from `[Unreleased]`.
4. Tag the merge commit on `main` (or on `release-X.Y` for a patch) and push the tag:
   ```
   git tag -a vX.Y.Z -m "gsm vX.Y.Z"
   git push origin vX.Y.Z
   ```
5. `.github/workflows/release.yml` then verifies the tagged commit, builds the binaries (`scripts/release-build.sh`), takes the notes from the version's CHANGELOG section (`scripts/release-notes.sh`, which fails if the section is missing) and publishes the GitHub release with every binary and `SHA256SUMS`. A tag with a `-` suffix becomes a pre-release. The release is created only after everything is built, and never overwrites an existing one.
6. Check the release page, download one binary, verify it against `SHA256SUMS`, and run `gsm version`.

If the workflow fails, fix the problem on `main` (and, for a patch, cherry-pick the fix onto `release-X.Y`), delete any release it left behind (`gh release delete vX.Y.Z`, in case an asset upload failed after the release was created), delete the tag (`git push --delete origin vX.Y.Z`, `git tag -d vX.Y.Z`), and tag again. The workflow also refuses a tag that's neither on `main` nor a patch on its own `release-X.Y` branch.

To try a release build locally: `nix develop path:./flake -c scripts/release-build.sh v0.0.0-test`.

## Workflow

Every change that will be committed goes through a GitHub issue and a pull request:

1. **Issue first.** Each task, and each unrelated problem found along the way, gets an issue. Fixes inside a task's scope don't need their own.
2. **Branch** `issue-<n>-<slug>` from `main`. Never commit to `main` directly. Keep commits small, each passing verify.
3. **Verify** before and after the change.
4. **Pull request** against `main`, with the body: `Closes #<n>` lines, then Why, Changes, Verification and Notes.
5. **Review** the PR, fix what it finds, and record the findings in a PR comment.
6. **Merge** with a merge commit after approval, deleting the branch.

GitHub enforces the parts that matter:

- **`main` and `release-*`** can only change through a pull request whose `verify (Linux)` check passes, and can't be deleted or force-pushed. Merges are merge commits only (squash and rebase are off), and merged branches are deleted automatically. Admins can override a failing check when merging a pull request, but can't push to these branches directly.
- **Release tags (`v*`)** can only be created by admins, and nobody can move or delete one, since pushing a tag publishes a release.
- **Pull requests from outside contributors** run CI only after a maintainer approves the run.

### Labels

| Label | Meaning |
|---|---|
| `type:bug` | something is broken |
| `type:feature` | new behavior |
| `type:chore` | maintenance: refactors, tooling, tech debt |
| `type:docs` | documentation only |
| `P0` | outage, data loss or security hole: drop everything |
| `P1` | a major feature broken with no workaround |
| `P2` | degraded, but a workaround exists |
| `P3` | minor or cosmetic |
| `needs-info` | waiting on answers from the reporter |
| `ready` | specified well enough to implement |

### Docs

Keep docs in step with code, in the same PR:

| Doc | Holds |
|---|---|
| [guide.md](guide.md) | using gsm: the walkthrough and command reference |
| [architecture.md](architecture.md) | how gsm is built: packages, data, formats, algorithms, per-OS behavior |
| [design.md](design.md) | why: goals, decisions and rejected alternatives, planned features, limitations, open questions |
| [roadmap.md](roadmap.md) | the releases ahead: theme, order and exit criteria per milestone |
| development.md | this file: dev environment, verify, tests, releasing, workflow |
| [README.md](../README.md) | what gsm is, install, links into these docs |
| [CHANGELOG.md](../CHANGELOG.md) | what changed in each release |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | how to report bugs, propose features and open pull requests (defers to this file) |
| [SECURITY.md](../SECURITY.md) | supported versions and private vulnerability reporting |
| [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) | Contributor Covenant 2.1 and how to report conduct issues |

Each fact lives in one doc; the others link to it.
