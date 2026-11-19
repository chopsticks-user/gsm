#!/usr/bin/env bash
# Formatting, vet and all tests. Run inside the dev shell: nix develop path:./flake -c scripts/verify.sh
set -euo pipefail
cd "$(dirname "$0")/.."

for f in scripts/*.sh; do bash -n "$f"; done
# go list skips .go/ (the module cache), which gofmt -l . would scan
unformatted=$(gofmt -l $(go list -f '{{.Dir}}' ./...))
if [ -n "$unformatted" ]; then
  echo "gofmt needed:" >&2
  echo "$unformatted" >&2
  exit 1
fi
go vet ./...
GOOS=windows go vet ./...
GOOS=darwin GOARCH=arm64 go vet ./...
go test -race ./...
