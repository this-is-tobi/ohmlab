#!/usr/bin/env bash
# Every Secret the Vault Secrets Operator writes must hold only the keys its
# consumer needs. A VaultStaticSecret that does not exclude the raw secret
# (`excludeRaw: true`) and the source keys (`excludes: [".*"]`) copies the whole
# Vault path into that Secret, so a pod that mounts one Secret of an app also
# receives the database superuser and backup credentials of the same app.
#
# With --stdin the rendered manifests are read from standard input; without
# arguments every chart is rendered with the values of the real instance (the
# _example one as fallback) and checked. Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"

# check_stream <label>: reads manifests on stdin, prints one line per violation.
check_stream() {
  yq ea -o=json -I=0 '[select(.kind == "VaultStaticSecret")]' - 2>/dev/null \
    | jq -r --arg label "$1" '
        .[]
        | (.spec.destination.transformation // {}) as $t
        | if ($t.excludeRaw // false) != true then
            "\($label): \(.metadata.name): the Secret also carries _raw, the whole Vault path (transformation.excludeRaw is not true)"
          elif (($t.excludes // []) | index(".*")) == null then
            "\($label): \(.metadata.name): the Secret copies every key of the Vault path (transformation.excludes lacks \".*\")"
          else empty end'
}

if [ "${1:-}" = "--stdin" ]; then
  violations=$(check_stream "${2:-stdin}")
  [ -z "$violations" ] || { printf '%s\n' "$violations"; exit 1; }
  exit 0
fi

failures=0
for chart in "$ROOT/utils/helm" "$ROOT"/argo-cd/apps/*/; do
  chart="${chart%/}"
  [ -f "$chart/Chart.yaml" ] || continue
  name="$(basename "$chart")"
  [ "$name" = helm ] && name=ohmlab
  [ "$name" = instance-manager ] && continue

  args=()
  vf=$(values_file "$name")
  [ -n "$vf" ] && args=(-f "$vf")
  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL $name: helm dependency build"; failures=$((failures + 1)); continue; }
  fi

  if ! rendered=$(helm template check "$chart" "${args[@]+"${args[@]}"}" 2>/dev/null); then
    echo "FAIL $name: does not render (see the render step)"
    failures=$((failures + 1))
    continue
  fi
  if violations=$(printf '%s\n' "$rendered" | check_stream "$name") && [ -z "$violations" ]; then
    echo "ok   $name"
  else
    printf '%s\n' "$violations" | sed 's/^/FAIL /'
    failures=$((failures + 1))
  fi
done

exit "$failures"
