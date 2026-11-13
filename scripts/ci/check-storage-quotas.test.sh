#!/usr/bin/env bash
# Decision table for scripts/ci/check-storage-quotas.sh: the quotas of the
# S3Buckets an instance's tiers render, against 80 % of each store's size.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-storage-quotas.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# instance <name> <storage.stores as JSON>: a fresh instance folder.
instance() {
  rm -rf "$work/instances/$1" "$work/render/$1"
  mkdir -p "$work/instances/$1"
  jq -n --argjson s "$2" '{instance: {name: "x"}, storage: {stores: $s}}' >"$work/instances/$1/instance.yaml"
}
# buckets <instance> <tier> <store>=<quota>...: the S3Buckets the tier renders.
buckets() {
  local inst="$1" tier="$2" i=0 pair
  shift 2
  mkdir -p "$work/render/$inst/$tier"
  for pair in "$@"; do
    i=$((i + 1))
    printf -- '---\napiVersion: ohmlab.fr/v1alpha1\nkind: S3Bucket\nmetadata: {name: b%s}\nspec: {store: %s, quota: %s}\n' "$i" "${pair%%=*}" "${pair#*=}"
  done >"$work/render/$inst/$tier/storage-claims.yaml"
}
# check <label> <expected exit> [<expected text in the output>]...
check() {
  local name="$1" want="$2" out rc t
  shift 2
  out=$(INSTANCES_DIR="$work/instances" "$SCRIPT" "$work/render" 2>&1); rc=$?
  if [ "$rc" != "$want" ]; then
    echo "FAIL $name: exit $rc, expected $want"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1; return
  fi
  for t in "$@"; do
    if ! grep -qxF -- "$t" <<<"$out"; then
      echo "FAIL $name: the output has no line: $t"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1; return
    fi
  done
  echo "ok   $name"
}

instance lab '{"backups": {"size": "50Gi"}}'
buckets lab core backups=5Gi backups=2Gi backups=2Gi backups=2Gi backups=3Gi
buckets lab tenant backups=2Gi backups=2Gi
check "18Gi of claims on a 50Gi store passes" 0 'ok   lab/backups: 18Gi of 50Gi'

buckets lab core backups=30Gi
buckets lab tenant backups=11Gi
check "41Gi of claims on a 50Gi store fails" 1 "FAIL lab/backups: the claims' quotas add up to 41Gi, above 80 % of 50Gi"

buckets lab core backups=40Gi
rm -f "$work/render/lab/tenant/storage-claims.yaml"
check "exactly 80 % passes, and a tier without claims counts nothing" 0 'ok   lab/backups: 40Gi of 50Gi'

buckets lab core backups=512Mi backups=512Mi backups=1536Mi
check "quotas in Mi add up" 0 'ok   lab/backups: 2560Mi of 50Gi'

instance lab '{"backups": {"size": "50Gi"}, "data": {"size": "10Gi"}}'
buckets lab core backups=30Gi data=10Gi
buckets lab tenant data=1Gi
check "each store is counted on its own" 1 'ok   lab/backups: 30Gi of 50Gi' "FAIL lab/data: the claims' quotas add up to 11Gi, above 80 % of 10Gi"

instance other '{"backups": {"size": "1Gi"}}'
buckets other core backups=1Gi
check "the exit code is the number of failing stores" 2 "FAIL lab/data: the claims' quotas add up to 11Gi, above 80 % of 10Gi" "FAIL other/backups: the claims' quotas add up to 1Gi, above 80 % of 1Gi"

# A quantity the check cannot read fails its store, rather than leaving it
# unchecked (the S3Bucket CRD takes any Kubernetes quantity).
rm -rf "$work/instances" "$work/render"
instance lab '{"backups": {"size": "50Gi"}}'
buckets lab core backups=45Gi backups=3GB
check "a quota the check cannot read fails its store" 1 "FAIL lab/backups: the quota 3GB is not a quantity this check reads: write it as a number and a unit, such as 2Gi"
instance lab '{"backups": {"size": "5e10"}}'
buckets lab core backups=2Gi
check "a size the check cannot read fails its store" 1 "FAIL lab/backups: the size 5e10 is not a quantity this check reads: write it as a number and a unit, such as 2Gi"

rm -rf "$work/instances" "$work/render"
instance plain '{}'
check "an instance without stores passes, silently" 0
if [ -n "$(INSTANCES_DIR="$work/instances" "$SCRIPT" "$work/render" 2>&1)" ]; then echo "FAIL an instance without stores prints nothing"; fail=1; else
  echo "ok   an instance without stores prints nothing"
fi

exit "$fail"
