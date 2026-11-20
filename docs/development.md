# Development

How to build, test and change gsm. For how the code is organized, see [architecture.md](architecture.md); for why, see [design.md](design.md).

## Dev environment

The repo ships a Nix flake with the toolchain: Go, gopls, and gcc (the race detector needs cgo).

```
nix develop path:./flake            # a shell with the toolchain
nix develop path:./flake -c <cmd>   # one command inside it
```

`path:` makes Nix copy only `flake/` into the store, not the whole repo, and lets it see the flake files before they're committed.

Entered from inside the repo, the shell keeps Go's module and build caches in a gitignored `.go/` at the repo root instead of `~/go` and `~/.cache/go-build`, and sets `GOFLAGS=-modcacherw` so `.go/` can be deleted normally. Entered from outside a git repo, it warns and leaves Go's defaults.

`nix run path:./flake#install` adds a desktop entry, "VS Code (gsm)", that opens the repo in VS Code inside the shell, so gopls and the Go extension see the toolchain. `nix run path:./flake#uninstall` removes it. The entry is written to `~/.local/share/applications`; on an impermanent home it survives a reboot only if that folder is persisted. Close every VS Code window before using it, since `code` hands the folder to a running instance, which keeps its old environment.

Without Nix, any Go matching the `go` line in `go.mod` works; you'll also need gcc for `-race`.

Tools gsm calls at runtime come from the host, not the flake: `git`, `steamcmd`, and on NixOS `steam-run`; `podman` for the playit addon.

## Build

```
nix develop path:./flake -c go build ./cmd/gsm
nix develop path:./flake -c env GOOS=windows go build ./cmd/gsm
nix develop path:./flake -c env GOOS=darwin GOARCH=arm64 go build ./cmd/gsm
```

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

`cmd/gsm/e2e_test.go` drives the real CLI through a whole lifecycle: create (with a port conflict), start, `status --json`, `config diff`, restart with its auto-commit, a live snapshot, stop, restore over a corrupted save, update to a new build, and `addon add|remove`. No network, Steam or game is involved. The test binary plays every part, chosen by `GSM_E2E_ROLE`: gsm itself, a fake steamcmd that lays out a Palworld-shaped install, and a fake server (started through a `PalServer.sh` that runs it as a child, like the real one) that rewrites a save every 100 ms and writes a final save on SIGTERM. Shell shims named `steamcmd`, `steam-run` and `podman` come first on `PATH`, so gsm runs as in production; `podman` always fails, so no container is ever started. The test sets its own `HOME`, puts its data outside `/tmp` on NixOS, and stops every server it started, even when it fails. It needs a POSIX shell, so it builds only on Unix.

An earlier end-to-end run like this found two bugs unit tests had missed: the early-exiting `steam-run` wrapper, and `steam-run` hiding `/tmp`. Behavior that spans processes deserves a step in this test.

On NixOS, keep test data out of `/tmp` when a test starts a server through `steam-run`, which hides `/tmp` from the server.

### Smoke test against the real server

```
nix develop path:./flake -c scripts/smoke-palworld.sh [--hold]
```

Repeats the lifecycle against the real Palworld dedicated server: it downloads it (about 5 GB) into a throwaway home under `~/.cache`, waits for the server to listen and save, snapshots, stops, restores over a corrupted `Level.sav` and checks the hash, starts again, and updates. The throwaway home is deleted at the end, also on failure. `--hold` pauses while the server runs, so you can connect with a game client. It needs steamcmd (plus `steam-run` and `steamclient.so` on NixOS) and a free UDP port 8211, so it doesn't run in CI; run it before a release.

### CI

`.github/workflows/ci.yml` runs verify through the flake on `ubuntu-latest` for every pull request and every push to `main`, the same command as locally. Linux only for now; Windows and macOS runners are [#15](https://github.com/chopsticks-user/gsm/issues/15).

## Adding a game

1. Add a `game.Game` value in `internal/game/game.go` (like `Palworld`) and append it to the `all` list there: Steam app ID, a launch entry per supported OS (command, config dir and saves dir inside the install, written with `/`), the args template with its defaults, the default port, the stop timeout, and an optional default config file.
2. If the game needs something the model lacks (a different launcher such as Wine, a config format gsm must edit, a save-flush method), extend the model in `internal/game` and the code that uses it in `cmd/gsm`, keeping existing games unchanged.
3. Check the definition with `gsm game show <game>`, then run the whole lifecycle against a real install: `create`, `start`, `status`, `stop`, `save restore`, `update`.
4. Document the game in [guide.md](guide.md) and any new limitation in [design.md](design.md#known-limitations).

## Workflow

Every change that will be committed goes through a GitHub issue and a pull request:

1. **Issue first.** Each task, and each unrelated problem found along the way, gets an issue. Fixes inside a task's scope don't need their own.
2. **Branch** `issue-<n>-<slug>` from `main`. Never commit to `main` directly. Keep commits small, each passing verify.
3. **Verify** before and after the change.
4. **Pull request** against `main`, with the body: `Closes #<n>` lines, then Why, Changes, Verification and Notes.
5. **Review** the PR, fix what it finds, and record the findings in a PR comment.
6. **Merge** with a merge commit after approval, deleting the branch.

### Labels

| Label | Meaning |
|---|---|
| `type:bug` | something is broken |
| `type:feature` | new behavior |
| `type:chore` | maintenance: refactors, tooling, tech debt |
| `type:docs` | documentation only |
| `P0` to `P3` | priority, from outage or data loss (`P0`) to minor (`P3`) |
| `needs-info` | waiting on answers from the reporter |
| `ready` | specified well enough to implement |

### Docs

Keep docs in step with code, in the same PR:

| Doc | Holds |
|---|---|
| [guide.md](guide.md) | using gsm: the walkthrough and command reference |
| [architecture.md](architecture.md) | how gsm is built: packages, data, formats, algorithms, per-OS behavior |
| [design.md](design.md) | why: goals, decisions and rejected alternatives, roadmap, limitations, open questions |
| development.md | this file: dev environment, verify, tests, workflow |

Each fact lives in one doc; the others link to it.
