#!/usr/bin/env bash
# Smoke test against the real Enshrouded dedicated server, run through Wine
# on Linux: the full gsm lifecycle with a real steamcmd download (about
# 7.6 GB per instance, two instances) in a throwaway home, deleted at the
# end, also on failure. Run it before a release:
#
#   nix develop -c scripts/smoke-enshrouded.sh [--hold]
#
# --hold pauses while the server runs, so you can connect with a game client.
# Needs steamcmd (and steam-run plus steamclient.so on NixOS, for steamcmd),
# wine and ss (on Linux), git, and free UDP ports 15637 and 15638. On Windows
# (Git Bash) the server runs natively; .github/workflows/smoke-windows.yml runs it there.
set -euo pipefail
cd "$(dirname "$0")/.."

hold=false
[ "${1:-}" = "--hold" ] && hold=true

cache="${XDG_CACHE_HOME:-$HOME/.cache}"
mkdir -p "$cache"
work=$(mktemp -d "$cache/gsm-smoke-XXXXXX")
home="$work/home"
gsm="$work/gsm$(go env GOEXE)" # gsm.exe on Windows, which needs the extension to start it
inst="$home/instances/smoke"
json="$inst/config/enshrouded_server.json"

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

case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) windows=true ;; *) windows=false ;; esac
# udp_listening <port>: ss on Linux, PowerShell on Windows (Git Bash).
udp_listening() {
  if $windows; then
    powershell -NoProfile -Command "if (Get-NetUDPEndpoint -LocalPort $1 -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
  else
    ss -uln | grep -q ":$1 "
  fi
}

# wait_for <seconds> <description> <command...>
wait_for() {
  local secs=$1 what=$2; shift 2
  for _ in $(seq "$secs"); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  fail "timed out after ${secs}s waiting for $what"
}

# The world save, not the server's rotating copies in backup/.
world() { find "$inst/saves" -type f -not -path '*/backup/*' -print -quit; }
has_save() { [ -n "$(world)" ]; }
# One hash over every world file (the world and its companions), backups excluded.
world_hash() { find "$inst/saves" -type f -not -path '*/backup/*' -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1; }
port_in_json() { grep -Eq "\"queryPort\"[[:space:]]*:[[:space:]]*$1([^0-9]|$)" "$2"; }

$windows || command -v ss >/dev/null || fail "ss not found (iproute2)"
[ "$(uname -s)" != Linux ] || command -v wine >/dev/null || fail "wine not found"

step "build gsm"
go build -o "$gsm" ./cmd/gsm

step "doctor"
g doctor

step "create (downloads the Windows server)"
g create enshrouded smoke
[ -L "$inst/server/savegame" ] || fail "saves are not linked into the install"
[ -L "$inst/server/enshrouded_server.json" ] || fail "the config file is not linked into the install"
port_in_json 15637 "$json" || fail "queryPort isn't 15637 in the created config"

step "start"
g start smoke
wait_for 180 "the server to listen on UDP 15637" udp_listening 15637
wait_for 300 "the first world save" has_save
echo "world save: $(world)"
[ -L "$inst/server/enshrouded_server.json" ] || fail "the server replaced the config link with a file"
port_in_json 15637 "$json" || fail "the server changed queryPort"

if $hold; then
  echo
  read -r -p "Server is up on UDP 15637. Connect with a game client, then press Enter to continue... " _ || true
fi

step "stop (SIGINT, Ctrl+Break on Windows; the server saves on the way out)"
marker="$work/before-stop"
touch "$marker"
sleep 1
g stop smoke
g status | grep 'smoke .* stopped' >/dev/null || fail "status doesn't show smoke stopped"
[ -n "$(find "$inst/saves" -type f -not -path '*/backup/*' -newer "$marker" -print -quit)" ] || fail "no world save written on stop"
level=$(world)
stop_id=$(g save list smoke | awk '$4 == "stop" {id = $1} END {print id}')
[ -n "$stop_id" ] || fail "no stop snapshot"
want=$(world_hash)
g config log smoke

step "restore $stop_id over a corrupted save"
echo corrupted > "$level"
g save restore smoke "$stop_id"
[ "$(world_hash)" = "$want" ] || fail "restored saves differ from the snapshot"

step "start again on the restored world"
g start smoke
wait_for 180 "the server to listen again" udp_listening 15637

step "a second instance gets the next port and runs beside the first"
g create enshrouded smoke2
json2="$home/instances/smoke2/config/enshrouded_server.json"
port_in_json 15638 "$json2" || fail "smoke2's queryPort isn't 15638"
g start smoke2
wait_for 180 "smoke2 to listen on UDP 15638" udp_listening 15638
sleep 20 # a port clash may take the server down only after it starts listening
echo "port keys in the server-written configs:"
grep -i port "$json" "$json2" || true
udp_listening 15637 || fail "smoke stopped listening when smoke2 started"
g status | grep -q "smoke2 .* running" || fail "smoke2 is not running beside smoke"
g status | grep -q "^smoke .* running" || fail "smoke is not running beside smoke2"
g stop smoke smoke2

step "update (expect: already up to date)"
g update smoke
[ -L "$inst/server/savegame" ] || fail "saves link lost after update"
[ -L "$inst/server/enshrouded_server.json" ] || fail "config link lost after update"

step "snapshots"
g save list smoke
