#!/usr/bin/env bash
# Tests of the trust-manager app: each Bundle reaches the namespaces its
# values list, and only namespaces trust-manager may write to.
#
#   test-trust-manager-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/trust-manager"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/trust-manager.yaml" || { echo "no manager parameters for trust-manager" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: the chart with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template "${RELEASE:-trust-manager}" "$CHART" --namespace trust-manager "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template trust-manager "$CHART" --namespace trust-manager "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$label" "" ""
  else
    check "$label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

# selector <Bundle>: its namespace selector, on one line.
selector() { q "select(.kind == \"Bundle\" and .metadata.name == \"$1\") | .spec.target.namespaceSelector | @json"; }

render
check "by default the internal CA's Bundle selects no namespace" "null" "$(selector ohmlab-internal-ca)"
check "by default Vault's Bundle selects no namespace" "null" "$(selector vault-ca-bundle)"

render --set 'trust-manager.app.targetNamespaces={vault-operator-system,keycloak-system}' --set 'internalCaBundle.namespaces={keycloak-system}'
check "a list selects its namespaces" \
  '{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["keycloak-system"]}]}' \
  "$(selector ohmlab-internal-ca)"
check "the other Bundle keeps every target namespace" "null" "$(selector vault-ca-bundle)"

fails "a listed namespace outside the targets fails" "trust-manager.app.targetNamespaces" \
  --set 'trust-manager.app.targetNamespaces={vault-operator-system}' --set 'vaultCaBundle.namespaces={keycloak-system}'

# trust-manager writes anywhere without a target list, which only a values file
# can give: --set turns {} into [""].
printf 'trust-manager:\n  app:\n    targetNamespaces: []\n' >"$work/any.yaml"
render -f "$work/any.yaml" --set 'vaultCaBundle.namespaces={keycloak-system}'
check "an empty target list accepts any namespace" \
  '{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["keycloak-system"]}]}' \
  "$(selector vault-ca-bundle)"

exit "$failures"
