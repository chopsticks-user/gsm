#!/usr/bin/env bash
# Prints the CHANGELOG.md section of a version (without its heading), for the
# release notes. Fails if the section is missing or empty, so a release can't
# go out without one.
#
#   scripts/release-notes.sh v0.1.0
set -euo pipefail
cd "$(dirname "$0")/.."

version=${1:?usage: scripts/release-notes.sh <version, e.g. v0.1.0>}
v=${version#v}
notes=$(awk -v v="$v" '
  /^## \[/ { if (found) exit; if (index($0, "## [" v "]") == 1) { found = 1; next } }
  found { print }
' CHANGELOG.md)
if [ -z "$(echo "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no section for $v (add a '## [$v] - YYYY-MM-DD' section)" >&2
  exit 1
fi
echo "$notes"
