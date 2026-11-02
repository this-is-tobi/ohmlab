#!/usr/bin/env bash
# Decision table for scripts/ohmlab-chart.sh against throwaway instance folders.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/ohmlab-chart.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# instance <name> <core.yaml apps entry lines> [<instance.yaml extra lines>]
instance() {
  local dir="$work/$1"
  mkdir -p "$dir"
  printf 'apps:\n%s\n' "$2" >"$dir/core.yaml"
  printf 'instance:\n  name: t\n%s\n' "${3:-}" >"$dir/instance.yaml"
  echo "$dir"
}

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

# run <instance-dir> [<local-chart>] -> "<stdout>|<exit code>"
run() {
  local out rc
  out=$("$SCRIPT" "$@" 2>"$work/err")
  rc=$?
  printf '%s|%s' "$out" "$rc"
}

tab=$'\t'
catalogs='catalogs:
  ohmlab: ghcr.io/example/charts'

d=$(instance plain '- app: ohmlab
  chartPath: charts/ohmlab')
check "no catalog uses the local chart" "charts/ohmlab$tab|0" "$(run "$d")"
check "the local chart path is the second argument" "/some where/chart$tab|0" "$(run "$d" "/some where/chart")"

d=$(instance none '- app: other
  enabled: "true"')
check "no ohmlab entry uses the local chart" "charts/ohmlab$tab|0" "$(run "$d")"

d=$(instance pinned '- app: ohmlab
  catalog: ohmlab
  targetRevision: 0.1.3' "$catalogs")
check "a catalog entry gives the registry chart and its version" "oci://ghcr.io/example/charts/ohmlab${tab}0.1.3|0" "$(run "$d")"

d=$(instance renamed '- app: ohmlab
  chart: ohmlab-bootstrap
  catalog: ohmlab
  targetRevision: 1.2.3-rc.1' "$catalogs")
check "the chart name and a prerelease version are kept" "oci://ghcr.io/example/charts/ohmlab-bootstrap${tab}1.2.3-rc.1|0" "$(run "$d")"

d=$(instance undeclared '- app: ohmlab
  catalog: nope
  targetRevision: 0.1.3' "$catalogs")
check "an undeclared catalog is refused" "|1" "$(run "$d")"

d=$(instance nocatalogs '- app: ohmlab
  catalog: ohmlab
  targetRevision: 0.1.3')
check "a catalog without any catalogs section is refused" "|1" "$(run "$d")"

d=$(instance nover '- app: ohmlab
  catalog: ohmlab' "$catalogs")
check "a catalog entry without a version is refused" "|1" "$(run "$d")"

bundle='catalogs:
  ohmlab:
    repoURL: oci://ghcr.io/example/catalog
    version: 0.2.0'
d=$(instance bundlecat '- app: ohmlab
  catalog: ohmlab
  targetRevision: 0.1.3' "$bundle")
check "a bundle catalog cannot give the bootstrap chart (helm installs a chart)" "|1" "$(run "$d")"
msg=$("$SCRIPT" "$d" 2>&1 >/dev/null || true)
check "the refusal says why" "ok" "$(grep -q 'registry path' <<<"$msg" && echo ok || echo no)"

for v in latest 0.1 0.1.x '^0.1.0' '>=0.1.3' 'v0.1.3' main 0.1.3abc 1.2.3.4; do
  d=$(instance "ver-$RANDOM" "- app: ohmlab
  catalog: ohmlab
  targetRevision: '$v'" "$catalogs")
  check "version '$v' is refused" "|1" "$(run "$d")"
done

exit "$fail"
