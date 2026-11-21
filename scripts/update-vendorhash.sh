#!/usr/bin/env bash
# Recomputes vendorHash in flake.nix after go.mod or go.sum change: blanks
# it, lets Nix report the hash it got, and writes that in. CI runs it on
# Dependabot Go module PRs (.github/workflows/vendorhash.yml).
#
#   scripts/update-vendorhash.sh
set -euo pipefail
cd "$(dirname "$0")/.."

pattern='vendorHash = "[^"]*";'
grep -q "$pattern" flake.nix || { echo "no vendorHash line in flake.nix" >&2; exit 1; }
old=$(grep -o "$pattern" flake.nix)
sed -i "s|$pattern|vendorHash = \"\";|" flake.nix
# The blank hash always fails the module fetch; Nix prints the real one.
out=$(nix build --no-link .#default.goModules 2>&1 || true)
hash=$(grep -oE 'got: +sha256-[A-Za-z0-9+/=]+' <<<"$out" | awk '{print $2}' | tail -1)
if [ -z "$hash" ]; then
  sed -i "s|vendorHash = \"\";|$old|" flake.nix
  echo "$out" >&2
  echo "couldn't read the vendor hash from nix build" >&2
  exit 1
fi
sed -i "s|vendorHash = \"\";|vendorHash = \"$hash\";|" flake.nix
echo "vendorHash = $hash"
