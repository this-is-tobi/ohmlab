#!/usr/bin/env bash
# The claims of an instance fit its stores: per store, the quotas of the
# S3Buckets that both tiers render add up to at most 80 % of the store's size
# in instance.yaml, so the store stays out of reach of its claims' quotas. It
# reads the S3Buckets render-instance.sh kept in
# <RENDER_OUT>/<instance>/<tier>/storage-claims.yaml.
#
#   check-storage-quotas.sh <RENDER_OUT>
#
# The instances are INSTANCES_DIR's. Exits with the number of stores that
# fail. Needs yq (mikefarah v4) and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
out="${1:?usage: check-storage-quotas.sh <RENDER_OUT>}"

failures=0
while IFS= read -r dir; do
  instance="${dir##*/}"
  stores=$(yq -o=json -I=0 '.storage.stores // {}' "$dir/instance.yaml")
  [ "$(jq length <<<"$stores")" -gt 0 ] || continue
  buckets=$(cat "$out/$instance"/*/storage-claims.yaml 2>/dev/null \
    | yq ea -o=json -I=0 'select(.kind == "S3Bucket") | {"store": .spec.store, "quota": .spec.quota}' - | jq -sc .)
  while IFS=$'\t' read -r verdict line; do
    echo "$line"
    [ "$verdict" = ok ] || failures=$((failures + 1))
  done < <(jq -rn --argjson stores "$stores" --argjson buckets "${buckets:-[]}" --arg instance "$instance" '
    def bytes: (capture("^(?<n>[0-9]+(\\.[0-9]+)?)(?<u>[KMGTPE]i|[kMGTPE])?$") // error("not a quantity: \(.)"))
      | (.n | tonumber) * {"": 1, "k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
          "Ki": 1024, "Mi": 1048576, "Gi": 1073741824, "Ti": 1099511627776, "Pi": 1125899906842624, "Ei": 1152921504606846976}[.u // ""];
    def human: if . % 1073741824 == 0 then "\(. / 1073741824)Gi" elif . % 1048576 == 0 then "\(. / 1048576)Mi" else "\(.)" end;
    $stores | to_entries[] | .key as $store | (.value.size | tostring) as $size
    | ([$buckets[] | select(.store == $store) | .quota | tostring | bytes] | add // 0) as $sum
    | if $sum * 100 <= ($size | bytes) * 80
      then "ok\tok   \($instance)/\($store): \($sum | human) of \($size)"
      else "fail\tFAIL \($instance)/\($store): the claims'"'"' quotas add up to \($sum | human), above 80 % of \($size)" end')
done < <(instance_dirs)

exit "$failures"
