#!/usr/bin/env bash
# Tests of the argo-cd app (the tenant Argo CD) whose failures only a running
# cluster would otherwise show: its Redis runs the exporter the ServiceMonitor
# scrapes.
#
#   test-argo-cd-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/argo-cd"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/argo-cd.yaml" || { echo "no manager parameters for argo-cd" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render: the chart with the example instance's values into $work/render.yaml,
# left empty when it does not render.
render() {
  if ! helm template argo-cd "$CHART" --namespace argo-cd "${args[@]}" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

spec='select(.kind == "Deployment" and .metadata.name == "argo-cd-argocd-redis") | .spec.template.spec'
render
check "the metrics Service's target port is a port of the Redis pod" "metrics" \
  "$(q "$spec | .containers[].ports[]? | select(.name == \"metrics\") | .name")"
check "the exporter has limits" "true" \
  "$(q "$spec | .containers[] | select(.name == \"metrics\") | (.resources.limits.cpu != null and .resources.limits.memory != null) | tostring")"

exit "$failures"
