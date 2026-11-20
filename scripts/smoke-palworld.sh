#!/usr/bin/env bash
# Smoke test against the real Palworld dedicated server: the full gsm
# lifecycle with a real steamcmd download (about 5 GB) in a throwaway home,
# deleted at the end, also on failure. Run it before a release:
#
#   nix develop path:./flake -c scripts/smoke-palworld.sh [--hold]
#
# --hold pauses while the server runs, so you can connect with a game client.
# Needs steamcmd (and steam-run plus steamclient.so on NixOS), git, and a free
# UDP port 8211.
set -euo pipefail
cd "$(dirname "$0")/.."

hold=false
[ "${1:-}" = "--hold" ] && hold=true

# Not under /tmp: on NixOS, steam-run gives the server a fresh /tmp.
cache="${XDG_CACHE_HOME:-$HOME/.cache}"
mkdir -p "$cache"
work=$(mktemp -d "$cache/gsm-smoke-XXXXXX")
home="$work/home"
gsm="$work/gsm"
inst="$home/instances/smoke"

cleanup() {
  status=$?
  [ -x "$gsm" ] && "$gsm" --home "$home" stop --all >/dev/null 2>&1 || true
  rm -rf "$work"
  if [ $status -eq 0 ]; then echo "PASS: smoke test"; else echo "FAIL: smoke test (exit $status)" >&2; fi
}
trap cleanup EXIT

step() { echo; echo "== $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
g() { "$gsm" --home "$home" "$@"; }

# wait_for <seconds> <description> <command...>
wait_for() {
  local secs=$1 what=$2; shift 2
  for _ in $(seq "$secs"); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  fail "timed out after ${secs}s waiting for $what"
}

listening() { grep -q "Running Palworld dedicated server" "$inst/logs/server.log"; }
has_save() { [ -n "$(find "$inst/saves" -name Level.sav -not -path '*/backup/*' -print -quit)" ]; }

step "build gsm"
go build -o "$gsm" ./cmd/gsm

step "doctor"
g doctor

step "create (downloads the server)"
g create palworld smoke
[ -L "$inst/server/Pal/Saved/SaveGames" ] || fail "saves are not linked into the install"
settings="$inst/config/PalWorldSettings.ini"
sed -i 's/ServerName="[^"]*"/ServerName="gsm smoke"/' "$settings"
grep -q 'ServerName="gsm smoke"' "$settings" || fail "couldn't edit ServerName in the seeded settings"

step "start"
g start smoke
wait_for 120 "the server to listen" listening
if command -v ss >/dev/null; then
  ss -uln | grep ':8211 ' >/dev/null || fail "nothing listens on UDP 8211"
fi
wait_for 300 "the first world save" has_save
level=$(find "$inst/saves" -name Level.sav -not -path '*/backup/*' -print -quit)
echo "world save: ${level#"$inst/"}"

if $hold; then
  echo
  read -r -p "Server is up on UDP 8211. Connect with a game client, then press Enter to continue... " _ || true
fi

step "snapshot while running (expect a save-flush warning)"
# The server may rewrite a save mid-snapshot; gsm then says "try again".
for try in 1 2 3; do g save snapshot smoke -m "smoke: running" && break; [ $try = 3 ] && fail "live snapshot failed 3 times"; sleep 2; done

step "stop"
g stop smoke
g status | grep 'smoke .* stopped' >/dev/null || fail "status doesn't show smoke stopped"
# Palworld keeps settings that differ from its defaults across the first start.
grep -q 'ServerName="gsm smoke"' "$settings" || fail "the ServerName edit didn't survive the first start"
stop_id=$(g save list smoke | awk '$4 == "stop" {id = $1} END {print id}')
[ -n "$stop_id" ] || fail "no stop snapshot"
want=$(sha256sum "$level" | cut -d' ' -f1)

step "restore $stop_id over a corrupted save"
echo corrupted > "$level"
g save restore smoke "$stop_id"
[ "$(sha256sum "$level" | cut -d' ' -f1)" = "$want" ] || fail "restored Level.sav differs from the snapshot"

step "start again on the restored world"
: > "$inst/logs/server.log"
g start smoke
wait_for 120 "the server to listen again" listening
g stop smoke
grep -q 'ServerName="gsm smoke"' "$settings" || fail "the ServerName edit didn't survive the second start"

step "update (expect: already up to date)"
g update smoke
[ -L "$inst/server/Pal/Saved/SaveGames" ] || fail "saves link lost after update"

step "snapshots"
g save list smoke
