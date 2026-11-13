#!/usr/bin/env bash
# Decision table for the --reads mode of scripts/ci/check-vault-seeds.sh: the
# keys hand-written VaultStaticSecret templates read, against hand-written
# VaultSeeds of a hub.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-vault-seeds.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

cat >"$work/seeds.json" <<'JSON'
[
  {"path": "dev/platforms/dev/tenant/gitea", "data": {"admin": {"username": "gitea", "password": "<random:32>"}, "token": "", "url": null}},
  {"path": "dev/platforms/dev/core/keycloak", "data": {"clients": {"grafana": {"clientSecret": "<random:32>"}}, "admin": "<random:32>"}},
  {"path": "ohmlab/platforms/dev/tenant/outline", "data": {"secret": "<random:32>"}},
  {"path": "ohmlab/platforms/production/core/argo-cd", "data": {"clusters": {"nonprod": {"bearerToken": "", "caData": ""}}}},
  {"path": "dev/platforms/dev/tenant/mattermost", "data": {"pg": {"accessKey": ""}}}
]
JSON

# reads <label> <secret> <path> [<template text>...] -> one VaultStaticSecret's reads
reads() {
  local label="$1" secret="$2" path="$3"
  shift 3
  jq -cn --arg label "$label" --arg secret "$secret" --arg path "$path" '{$label, $secret, $path, texts: $ARGS.positional}' --args "$@"
}

# check <label> <expected exit> <reads...> -- [<expected text in the output>]
check() {
  local name="$1" want="$2" out rc
  shift 2
  : >"$work/reads.jsonl"
  while [ $# -gt 0 ] && [ "$1" != -- ]; do printf '%s\n' "$1" >>"$work/reads.jsonl"; shift; done
  shift
  jq -s . "$work/reads.jsonl" >"$work/reads.json"
  out=$("$SCRIPT" --reads "$work/reads.json" "$work/seeds.json" hub 2>&1); rc=$?
  if [ "$rc" != "$want" ]; then
    echo "FAIL $name: exit $rc, expected $want"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${1:-}" ] && ! grep -qF -- "$1" <<<"$out"; then
    echo "FAIL $name: the output does not say: $1"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $name"
  fi
}

gitea=dev/platforms/dev/tenant/gitea
keycloak=dev/platforms/dev/core/keycloak
r=dev/tenant/gitea

check "a seeded key passes" 0 "$(reads $r s $gitea '{{ get .Secrets "token" }}')" -- 'ok   1 key(s) read, all seeded by hub'
check "a key seeded empty is a placeholder and passes" 0 "$(reads $r s $gitea '{{ get .Secrets "token" }}' '{{ get .Secrets "url" }}')" -- '2 key(s) read'
check "a key the seed does not have is refused" 1 "$(reads $r s $gitea '{{ get .Secrets "missing" }}')" -- "FAIL $r: $gitea key missing is not seeded by hub hub"
check "a dig reads the nested key" 0 "$(reads $r s $gitea '{{ get .Secrets "admin" | dig "password" "" }}')"
check "a dig under clients reads both levels" 0 "$(reads dev/core/keycloak s $keycloak '{{ get .Secrets "clients" | dig "grafana" "clientSecret" "" }}')"
check "a nested key the seed does not have is refused" 1 "$(reads dev/core/keycloak s $keycloak '{{ get .Secrets "clients" | dig "argocd" "clientSecret" "" }}')" -- "$keycloak key clients.argocd.clientSecret is not seeded"
check "a dig through a key that holds no map is refused" 1 "$(reads $r s $gitea '{{ get .Secrets "token" | dig "x" "" }}')" -- "key token.x is not seeded"
check "a field reads its key" 0 "$(reads $r s $gitea '{{ .Secrets.token }}')"
check "a missing field is refused" 1 "$(reads $r s $gitea '{{ .Secrets.nope }}')" -- 'key nope is not seeded'
check "every read of a text is checked" 1 "$(reads $r s $gitea '{{ printf "%s:%s" (get .Secrets "admin" | dig "username" "") (get .Secrets "admin" | dig "email" "") }}')" -- 'key admin.email is not seeded'
check "a get piped to default or b64dec reads its key" 0 "$(reads $r s $gitea '{{ get .Secrets "token" | default "" }}' '{{ get .Secrets "url" | b64dec }}')"
check "a form the check cannot follow is refused, naming the secret" 1 "$(reads $r giteaAdmin $gitea '{{ index .Secrets "token" }}')" -- 'VaultStaticSecret giteaAdmin reads .Secrets in a form this check cannot follow'
check "a path seeded under another mount is refused" 1 "$(reads dev/tenant/outline s dev/platforms/dev/tenant/outline '{{ get .Secrets "secret" }}')" -- 'dev/platforms/dev/tenant/outline key secret is not seeded by hub hub'
check "a secret without templates passes when its path is seeded" 0 "$(reads $r s $gitea)"
check "a secret without templates is refused when its path is not seeded" 1 "$(reads dev/tenant/x s dev/platforms/dev/tenant/x)" -- 'FAIL dev/tenant/x: dev/platforms/dev/tenant/x is not seeded by hub hub'
check "a key read twice counts once and fails once" 1 "$(reads $r s $gitea '{{ get .Secrets "gone" }}' '{{ get .Secrets "gone" }}')" -- "FAIL $r: $gitea key gone is not seeded by hub hub"
if [ "$(grep -c '^FAIL' <<<"$("$SCRIPT" --reads "$work/reads.json" "$work/seeds.json" hub 2>&1)")" != 1 ]; then
  echo "FAIL a key read twice is reported twice"; fail=1
else
  echo "ok   a key read twice is reported once"
fi
check "no read passes" 0 -- '0 key(s) read'

# Each action that uses .Secrets is one of the forms above, else the secret
# fails: a form the check cannot follow could read keys it does not see.
cannot='reads .Secrets in a form this check cannot follow'
check "trim markers close an action" 0 "$(reads $r s $gitea '{{- get .Secrets "token" -}}')"
check "a }} inside a string does not close the action" 1 "$(reads $r s $gitea '{{ printf "}}%s" (get .Secrets "missing") }}')" -- 'key missing is not seeded'
check "a dig with a default that is not a string cannot be followed" 1 "$(reads $r s $gitea '{{ get .Secrets "admin" | dig "password" 0 }}')" -- "$cannot"
check "a dig with a dict default cannot be followed" 1 "$(reads $r s $gitea '{{ get .Secrets "admin" | dig "password" (dict) }}')" -- "$cannot"
check "an index of a get cannot be followed" 1 "$(reads $r s $gitea '{{ index (get .Secrets "admin") "password" }}')" -- "$cannot"
check "a get through default dict reads the dug key" 0 "$(reads $r s $gitea '{{ get .Secrets "admin" | default dict | dig "password" "" }}')" -- '1 key(s) read'
check "a key under default dict the seed does not have is refused" 1 "$(reads $r s $gitea '{{ get .Secrets "admin" | default dict | dig "email" "" }}')" -- 'key admin.email is not seeded'
check "trim and toJson read their key" 0 "$(reads $r s $gitea '{{ get .Secrets "token" | trim | toJson }}')" -- '1 key(s) read'
check "a cluster's config reads its two credentials" 0 "$(reads dev/core/ohmlab cluster-nonprod ohmlab/platforms/production/core/argo-cd \
  '{{ printf "{\"bearerToken\":%s,\"tlsClientConfig\":{\"caData\":%s}}" (get .Secrets "clusters" | default dict | dig "nonprod" "bearerToken" "" | trim | toJson) (get .Secrets "clusters" | default dict | dig "nonprod" "caData" "" | trim | toJson) }}')" -- '2 key(s) read'
check "a get kept in a variable cannot be followed" 1 "$(reads $r s $gitea '{{ $a := get .Secrets "admin" }}{{ $a.password }}')" -- "$cannot"
check "a field piped to dig cannot be followed" 1 "$(reads $r s $gitea '{{ .Secrets.admin | dig "password" "" }}')" -- "$cannot"
check "an action left open cannot be followed" 1 "$(reads $r s $gitea '{{ get .Secrets "token"')" -- "$cannot"
check "a printf of strings and gets reads each get" 0 "$(reads $r s $gitea '{{ printf "%s:%s/%s" "u" (get .Secrets "admin" | dig "username" "") (get .Secrets "token") }}')" -- '2 key(s) read'

# A storage claim's key: the claim's record when the app's path has the claim
# key, else a fallback, which the seed must have. A path that ends in /s3 is a
# claim's record, which the controller's storage target writes, not a seed: a
# read there is <claim>.<one of the record's keys>.
claim='{{ with get .Secrets "pg" }}{{ dig "accessKey" "" . }}{{ else }}{{ get .Secrets "admin" | dig "password" "" }}{{ end }}'
check "a claim read takes its fallback while the path has no claim key" 0 "$(reads $r s $gitea "$claim")" -- '1 key(s) read'
check "a claim read whose fallback is not seeded is refused" 1 \
  "$(reads $r s $gitea '{{ with get .Secrets "pg" }}{{ dig "accessKey" "" . }}{{ else }}{{ get .Secrets "missing" }}{{ end }}')" -- "$gitea key missing is not seeded"
check "a claim read without a fallback reads nothing while the path has no claim key" 0 \
  "$(reads $r s $gitea '{{ with get .Secrets "pg" }}{{ dig "ca" "" . }}{{ end }}')" -- '0 key(s) read'
mm=dev/platforms/dev/tenant/mattermost
check "a claim read takes the claim key when the path has it" 0 "$(reads dev/tenant/mattermost s $mm "$claim")" -- '1 key(s) read'
check "a claim key's field the seed does not have is refused" 1 \
  "$(reads dev/tenant/mattermost s $mm '{{ with get .Secrets "pg" }}{{ dig "secretKey" "" . }}{{ end }}')" -- "$mm key pg.secretKey is not seeded"
record=dev/platforms/dev/tenant/gitea/s3
check "a claim's record serves the claim's keys" 0 \
  "$(reads $r s $record "$claim" '{{ get .Secrets "vault" | dig "endpoint" "" }}' '{{ with get .Secrets "pg" }}{{ dig "ca" "" . }}{{ end }}')" -- '3 key(s) read'
check "a key a claim's record does not have is refused" 1 \
  "$(reads $r s $record '{{ get .Secrets "pg" | dig "password" "" }}')" -- "$record is a claim's record: pg.password is not one of its keys"
check "a claim's record read without templates passes" 0 "$(reads $r s $record)"

# The loop reports what it cannot read rather than passing.
check "a secret that did not render fails, and no ok line follows" 1 "$(jq -cn '{label: "dev/tenant/x", fail: "does not render: boom"}')" -- 'FAIL dev/tenant/x: does not render: boom'
if grep -q '^ok' <<<"$("$SCRIPT" --reads "$work/reads.json" "$work/seeds.json" hub 2>&1)"; then
  echo "FAIL an ok line follows a FAIL"; fail=1
else
  echo "ok   no ok line follows a FAIL"
fi
: >"$work/empty.json"
out=$("$SCRIPT" --reads "$work/reads.json" "$work/empty.json" hub 2>&1); rc=$?
if [ "$rc" = 2 ] && grep -qF 'FAIL cannot read' <<<"$out"; then echo "ok   seeds that are not JSON fail"; else
  echo "FAIL seeds that are not JSON (exit $rc)"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

exit "$fail"
