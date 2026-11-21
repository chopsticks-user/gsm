# Common commands. Run inside the dev shell (`nix develop`), or
# prefix any recipe: `nix develop -c just <recipe>`. Like the
# dev shell, this needs a Unix host.

# Pass recipe arguments to the shell as "$@", unsplit and unexpanded.
set positional-arguments

# List the recipes
default:
    @just --justfile {{justfile()}} --list

# Build gsm and install it into dir (default ~/.local/bin). On NixOS, use the flake's package or overlay instead
install dir="~/.local/bin":
    mkdir -p {{dir}}
    go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" -o {{dir}}/gsm ./cmd/gsm
    {{dir}}/gsm version

# Build gsm into build/
build:
    go build -o build/ ./cmd/gsm

# Run all tests with the race detector, the end-to-end test included
test:
    go test -race ./...

# Formatting, vet for Linux/Windows/macOS and the race tests: what CI runs
verify:
    scripts/verify.sh

# Rewrite Go files to gofmt style (every package, as verify checks)
fmt:
    go fmt ./...

# Scan for known vulnerabilities in code gsm calls, for every OS CI scans (needs the network)
vuln:
    #!/usr/bin/env bash
    set -euo pipefail
    for target in linux/amd64 windows/amd64 darwin/arm64; do
        echo "== $target"
        GOOS=${target%/*} GOARCH=${target#*/} govulncheck -test ./...
    done

# Lifecycle against the real Palworld server (5 GB download); `just smoke --hold` to connect a client
smoke *args:
    scripts/smoke-palworld.sh "$@"

# Lifecycle against the real Enshrouded server under Wine (2 x 7.6 GB download); `--hold` to connect a client
smoke-enshrouded *args:
    scripts/smoke-enshrouded.sh "$@"

# Release binaries for every OS and CPU into build/release/
release-build version:
    scripts/release-build.sh "$1"

# Print a version's CHANGELOG section, as the release notes use it
release-notes version:
    scripts/release-notes.sh "$1"

# Delete build output
clean:
    rm -rf build
