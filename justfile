# Common commands. Run inside the dev shell (`nix develop path:./flake`), or
# prefix any recipe: `nix develop path:./flake -c just <recipe>`.

# List the recipes
default:
    @just --list

# Build gsm into build/
build:
    go build -o build/ ./cmd/gsm

# Run all tests with the race detector, the end-to-end test included
test:
    go test -race ./...

# Formatting, vet for Linux/Windows/macOS and the race tests: what CI runs
verify:
    scripts/verify.sh

# Rewrite Go files to gofmt style
fmt:
    gofmt -w cmd internal

# Check that gsm builds for Windows and macOS too
cross:
    GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/gsm
    GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/gsm

# Scan for known vulnerabilities in code gsm calls (needs the network)
vuln:
    govulncheck -test ./...

# Lifecycle against the real Palworld server (5 GB download); `just smoke --hold` to connect a client
smoke *args:
    scripts/smoke-palworld.sh {{args}}

# Release binaries for every OS and CPU into build/release/
release-build version:
    scripts/release-build.sh {{version}}

# Print a version's CHANGELOG section, as the release notes use it
release-notes version:
    scripts/release-notes.sh {{version}}

# Delete build output
clean:
    rm -rf build
