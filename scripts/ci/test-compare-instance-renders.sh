#!/usr/bin/env bash
# Tests compare-instance-renders.sh on hand-written renders: what fails (an
# object deleted or renamed, an app gone) and what is only listed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT/scripts/ci/compare-instance-renders.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# render <dir> <app> <lines...>: one app's objects file.
render() { local d="$work/$1/lab/core"; mkdir -p "$d"; shift; local app="$1"; shift; printf '%s\n' "$@" | sort >"$d/$app.objects"; }
expect() { # expect <description> <rc> <text>
  local out rc
  out=$(GITHUB_STEP_SUMMARY="$work/summary" "$SCRIPT" "$work/base" "$work/head" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1 (rc $rc, want $2, looking for '$3')"; sed 's/^/     /' <<<"$out"; fail=1
  fi
  rm -rf "$work/base" "$work/head"
}

render base vault 'Vault/vault/vault aaaa' 'ConfigMap/vault/cm bbbb'
render head vault 'Vault/vault/vault aaaa' 'ConfigMap/vault/cm bbbb'
expect "identical renders pass" 0 "nothing deleted or renamed (0 changed, 0 added)"

render base gw 'Gateway/traefik/homelab-gateway aaaa'
render head gw 'Gateway/traefik/ohmlab-gateway aaaa'
expect "a renamed object fails" 1 "gw: Gateway/traefik/homelab-gateway"

render base app 'Deployment/a/x aaaa' 'Service/a/x bbbb'
render head app 'Deployment/a/x aaaa'
expect "a removed object fails" 1 "app: Service/a/x"

render base app 'Deployment/a/x aaaa'
mkdir -p "$work/head/lab/core"
expect "an app that no longer renders fails" 1 "app: the app no longer renders"

render base vault 'Vault/vault/vault aaaa'
render head vault 'Vault/vault/vault cccc'
expect "a changed object passes and is listed" 0 "vault: Vault/vault/vault"

render base app 'Deployment/a/x aaaa'
render head app 'Deployment/a/x aaaa' 'Service/a/y bbbb'
expect "an added object passes and is listed" 0 "Added (1)"

grep -q '### Deleted or renamed' "$work/summary" && echo "ok   the job summary lists them" || { echo "FAIL no job summary"; fail=1; }

exit "$fail"
