#!/usr/bin/env bash
# Tests of the settings trivy-operator builds its scan pods from. The operator
# creates those pods at runtime from its ConfigMaps, so no render check sees
# them: a key the chart renames would leave every scan pod refused by
# require-non-root, or downloading the whole vulnerability database, and only a
# running cluster would show it.
#
#   test-trivy-operator-chart.sh
#
# Needs helm, yq (mikefarah v4) and jq. No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/trivy-operator"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/trivy-operator.yaml" || { echo "no manager parameters for trivy-operator" >&2; exit 1; }
helm template trivy-operator "$CHART" --namespace trivy-operator "${args[@]}" >"$work/render.yaml" 2>"$work/err" \
  || { echo "FAIL: the chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)" >&2; exit 1; }

# setting <ConfigMap> <key>: one key of a rendered ConfigMap, empty when absent.
setting() {
  yq -o=json -I=0 "select(.kind == \"ConfigMap\" and .metadata.name == \"$1\") | .data" "$work/render.yaml" \
    | jq -r --arg k "$2" '.[$k] // empty'
}

pod=$(setting trivy-operator scanJob.podTemplatePodSecurityContext)
jq -e '.runAsNonRoot == true and (.runAsUser // 0) > 0' <<<"${pod:-null}" >/dev/null 2>&1 \
  || fail "scan pods must run as a declared non-root user (scanJob.podTemplatePodSecurityContext: '${pod}')"
mode=$(setting trivy-operator-trivy-config trivy.mode)
[ "$mode" = ClientServer ] \
  || fail "scans must use the built-in Trivy server, not a database download per scan pod (trivy.mode: '$mode')"
infra=$(setting trivy-operator-config OPERATOR_INFRA_ASSESSMENT_SCANNER_ENABLED)
[ "$infra" = false ] \
  || fail "the infra-assessment scanner must stay off: its node-collector Job needs the host PID namespace and host paths, which pod-security-baseline refuses (OPERATOR_INFRA_ASSESSMENT_SCANNER_ENABLED: '$infra')"
cache=$(setting trivy-operator-config OPERATOR_CLUSTER_SBOM_CACHE_ENABLED)
[ "$cache" = true ] \
  || fail "rescans must read each image's cached SBOM rather than pull the image again (OPERATOR_CLUSTER_SBOM_CACHE_ENABLED: '$cache')"

if [ "$failures" -gt 0 ]; then
  echo "test-trivy-operator-chart: $failures failure(s)" >&2
  exit 1
fi
echo "ok   trivy-operator: non-root scan pods, client-server scans, no node-collector, SBOM cache"
