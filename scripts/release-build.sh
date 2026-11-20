#!/usr/bin/env bash
# Builds release binaries for every supported OS and CPU into build/release/,
# with the version baked in, plus a SHA256SUMS file.
#
#   nix develop path:./flake -c scripts/release-build.sh v0.1.0
set -euo pipefail
cd "$(dirname "$0")/.."

version=${1:?usage: scripts/release-build.sh <version, e.g. v0.1.0>}
if ! [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "version must look like v1.2.3 or v1.2.3-rc.1, got: $version" >&2
  exit 1
fi

out=build/release
rm -rf "$out"
mkdir -p "$out"
for target in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*} arch=${target#*/}
  name=gsm-$os-$arch
  [ "$os" = windows ] && name=$name.exe
  echo "building $name"
  # Pure Go, so cross-compiling needs no C toolchain; -trimpath drops local paths.
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$out/$name" ./cmd/gsm
done
(cd "$out" && sha256sum gsm-* > SHA256SUMS)
echo "built $version into $out:"
ls -l "$out"
