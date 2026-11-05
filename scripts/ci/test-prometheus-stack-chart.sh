#!/usr/bin/env bash
# Tests of the prometheus-stack chart: Grafana's Postgres cluster comes and goes
# with Grafana, so a cluster that runs Prometheus and Alertmanager alone renders
# no database whose credentials nothing writes.
#
#   test-prometheus-stack-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/prometheus-stack"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/prometheus-stack.yaml" || { echo "no manager parameters for prometheus-stack" >&2; exit 1; }

# clusters [<helm args>...]: the CNPG Clusters the chart renders, by name.
clusters() {
  helm template prometheus-stack "$CHART" --namespace prometheus-stack "${args[@]}" "$@" 2>"$work/err" \
    | yq ea '[select(.kind == "Cluster") | .metadata.name] | join(" ")' - \
    || echo "does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"
}

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

check "with Grafana, its database renders" "prometheus-stack-pg-cluster" "$(clusters)"
check "without Grafana, no database renders" "" "$(clusters --set kube-prometheus-stack.grafana.enabled=false)"

exit "$failures"
