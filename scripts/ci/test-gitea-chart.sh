#!/usr/bin/env bash
# Tests of the gitea chart's Valkey TLS, whose failures only a running cluster
# would otherwise show: Valkey reloads a renewed certificate.
#
#   test-gitea-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/gitea"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/gitea.yaml" || { echo "no manager parameters for gitea" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render: the chart with the example instance's values into $work/render.yaml,
# left empty when it does not render.
render() {
  if ! helm template gitea "$CHART" --namespace gitea "${args[@]}" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

render
check "Valkey reloads a renewed certificate" "1" \
  "$(q '[select(.kind == "ConfigMap" and .metadata.name == "gitea-valkey") | .data["valkey.conf"] | split("\n") | .[] | select(. == "tls-auto-reload-interval 10")] | length')"
check "a change of Valkey's config rolls its pod" "tls-auto-reload" \
  "$(q 'select(.kind == "StatefulSet" and .metadata.name == "gitea-valkey") | .spec.template.metadata.annotations["ohmlab.fr/config-revision"]')"

exit "$failures"
