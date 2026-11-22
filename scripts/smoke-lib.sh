#!/usr/bin/env bash
# Shared by the smoke-*.sh scripts, which source it after setting
# server_procs (a Get-Process name pattern for the Windows diagnostics). It
# makes a throwaway home, deleted at the end (also on failure, after printing
# what helps to diagnose it), and the helpers below. Runs on Linux and on
# Windows (Git Bash).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

hold=false
[ "${1:-}" = "--hold" ] && hold=true

case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) windows=true ;; *) windows=false ;; esac
$windows || command -v ss >/dev/null || { echo "FAIL: ss not found (iproute2)" >&2; exit 1; }

# Not under /tmp: on NixOS, steam-run gives the server a fresh /tmp.
cache="${XDG_CACHE_HOME:-$HOME/.cache}"
mkdir -p "$cache"
work=$(mktemp -d "$cache/gsm-smoke-XXXXXX")
home="$work/home"
gsm="$work/gsm$(go env GOEXE)" # gsm.exe on Windows, which needs the extension to start it

cleanup() {
  status=$?
  if [ $status -ne 0 ] && [ -d "$home/instances" ]; then
    [ -x "$gsm" ] && "$gsm" --home "$home" status || true
    if $windows; then
      powershell -NoProfile -Command "Get-Process | Where-Object Name -like '$server_procs' | Format-Table Id,Name,StartTime; Get-NetUDPEndpoint | Where-Object LocalPort -gt 1024 | Format-Table LocalAddress,LocalPort,OwningProcess" || true
    fi
    for log in "$home"/instances/*/logs/server.log; do
      [ -f "$log" ] && { echo "== last lines of $log"; tail -n 60 "$log"; }
    done
  fi
  [ -x "$gsm" ] && "$gsm" --home "$home" stop --all >/dev/null 2>&1 || true
  rm -rf "$work"
  if [ $status -eq 0 ]; then echo "PASS: smoke test"; else echo "FAIL: smoke test (exit $status)" >&2; fi
}
trap cleanup EXIT

step() { echo; echo "== $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
g() { "$gsm" --home "$home" "$@"; }

# udp_listening <port>: ss on Linux, PowerShell on Windows.
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

step "build gsm"
go build -o "$gsm" ./cmd/gsm
