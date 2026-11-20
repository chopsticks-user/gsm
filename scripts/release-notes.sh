#!/usr/bin/env bash
# Prints the CHANGELOG.md section of a version (without its heading), for the
# release notes. A pre-release (v1.2.3-rc.1) without a section of its own
# uses the Unreleased section. Fails if the section is missing or empty, so a
# release can't go out without notes.
#
#   scripts/release-notes.sh v0.1.0
set -euo pipefail
cd "$(dirname "$0")/.."

version=${1:?usage: scripts/release-notes.sh <version, e.g. v0.1.0>}
v=${version#v}

# section NAME prints the body of "## [NAME]", up to the next section or the
# link definitions at the bottom.
section() {
  awk -v name="$1" '
    /^## \[/ { if (found) exit; if (index($0, "## [" name "]") == 1) { found = 1; next } }
    /^\[[^]]+\]: / { if (found) exit }
    found { print }
  ' CHANGELOG.md
}
empty() { [ -z "$(echo "$1" | tr -d '[:space:]')" ]; }

notes=$(section "$v")
if empty "$notes" && [[ $v == *-* ]]; then
  notes=$(section Unreleased)
fi
if empty "$notes"; then
  echo "CHANGELOG.md has no section for $v (add a '## [$v] - YYYY-MM-DD' section)" >&2
  exit 1
fi
echo "$notes"
