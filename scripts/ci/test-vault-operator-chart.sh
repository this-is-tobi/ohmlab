#!/usr/bin/env bash
# Tests of the vault-operator chart's monitors for the two operators it
# installs, bank-vaults and the Vault Secrets Operator: like the chart's Vault
# monitor they render only where the Prometheus Operator API exists (Argo CD
# refuses a whole sync over one unknown kind, and the core apps that bring that
# API need Vault first), and each selects the one rendered Service that carries
# its endpoint's port, so a subchart that renames a label or a port fails here
# rather than as a target that is never scraped. Then the shape of a cluster
# that reads a remote Vault: VSO alone, its default connection on the hub.
#
#   test-vault-operator-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/vault-operator"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/vault-operator.yaml" || { echo "no manager parameters for vault-operator" >&2; exit 1; }
# render <out file> [<helm args>...]: the chart as Argo CD renders the example's vault-operator.
render() {
  local out="$1"
  shift
  helm template vault-operator "$CHART" --namespace vault-operator-system "${args[@]}" "$@" >"$out" 2>"$work/err" \
    || { fail "the chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; : >"$out"; }
}
# selected <render> <monitor>: the rendered Services the monitor's labels select
# that carry one of its endpoint ports, as <Service>:<port>.
selected() {
  yq ea -o=json -I=0 '[select(.kind == "Service" or .kind == "ServiceMonitor")]' "$1" | jq -r --arg m "$2" '
    (map(select(.kind == "ServiceMonitor" and .metadata.name == $m)) | first) as $sm
    | select($sm != null)
    | ($sm.spec.selector.matchLabels // {}) as $want | [$sm.spec.endpoints[].port] as $ports
    | .[] | select(.kind == "Service")
    | select(.metadata.labels as $l | $want | to_entries | all(.value == $l[.key]))
    | .metadata.name as $s | .spec.ports[] | select(.name as $p | $ports | index($p)) | "\($s):\(.name)"' | paste -sd, -
}

render "$work/bare.yaml"
[ -z "$(yq 'select(.apiVersion == "monitoring.coreos.com/v1") | .kind' "$work/bare.yaml")" ] \
  || fail "a monitor renders on a cluster without the Prometheus Operator"

render "$work/monitoring.yaml" --api-versions monitoring.coreos.com/v1
got=$(selected "$work/monitoring.yaml" vault-operator)
[ "$got" = "vault-operator:http-metrics" ] || fail "the bank-vaults monitor must select the operator Service's http-metrics port, got '$got'"
got=$(selected "$work/monitoring.yaml" vault-secrets-operator)
[ "$got" = "vault-operator-vso-metrics-service:https" ] || fail "the VSO monitor must select its metrics Service's https port, got '$got'"

render "$work/novso.yaml" --api-versions monitoring.coreos.com/v1 --set vso.enabled=false
[ -z "$(yq 'select(.kind == "ServiceMonitor" and .metadata.name == "vault-secrets-operator") | .kind' "$work/novso.yaml")" ] \
  || fail "the VSO monitor renders with VSO off"

# Each Argo CD reads only its own tier's path: the core one's holds the
# credentials of the clusters it deploys to, which a tenant must not read.
# argo_paths <policy>: the paths of that policy, comma-separated.
argo_paths() {
  yq -o=json -I=0 "select(.kind == \"Vault\") | .spec.externalConfig.policies[] | select(.name == \"$1\") | .rules" "$work/bare.yaml" \
    | jq -r '[scan("path \"([^\"]+)\"") | .[0]] | join(",")'
}
got=$(argo_paths argo-cd)
[ "$got" = "example/data/platforms/+/tenant/argo-cd" ] || fail "the tenant's Argo CD must read only its tier's path, got '$got'"
got=$(argo_paths argo-cd-core)
[ "$got" = "ohmlab/data/platforms/+/core/argo-cd" ] || fail "the core Argo CD must read only its tier's path, got '$got'"

# A cluster that reads a remote Vault runs VSO alone: no bank-vaults operator,
# no Vault and no seeds; its default connection is the hub's route, with a CA
# Secret only when the values give one.
remote=(--api-versions monitoring.coreos.com/v1 --set vault.enabled=false --set vault-operator.enabled=false
  --set vaultOidcCredentials.enabled=false --set vso.defaultVaultConnection.address=https://vault.example.com
  --set vso.defaultVaultConnection.caCertSecret=)
render "$work/remote.yaml" "${remote[@]}"
bankvaults="vault-operator-$(yq '.dependencies[] | select(.name == "vault-operator") | .version' "$CHART/Chart.yaml")"
[ -z "$(yq "select(.metadata.labels[\"helm.sh/chart\"] == \"$bankvaults\" or (.kind == \"ServiceMonitor\" and .metadata.name == \"vault-operator\")) | .kind" "$work/remote.yaml")" ] \
  || fail "the bank-vaults operator or its monitor renders with vault-operator.enabled=false"
[ -z "$(yq 'select(.kind == "Vault" or .kind == "VaultSeed") | .kind' "$work/remote.yaml")" ] \
  || fail "a Vault or a VaultSeed renders with vault.enabled=false"
[ -n "$(yq 'select(.kind == "Deployment" and .metadata.name == "vault-operator-vso-controller-manager") | .kind' "$work/remote.yaml")" ] \
  || fail "VSO does not render alone"
got=$(yq -o=json -I=0 'select(.kind == "VaultConnection" and .metadata.name == "default") | .spec' "$work/remote.yaml")
[ "$got" = '{"address":"https://vault.example.com","skipTLSVerify":false}' ] \
  || fail "the default VaultConnection must be the hub's address with no CA, got '$got'"
[ -z "$(yq 'select(.kind == "Secret" and .metadata.name == "vault-hub-ca") | .kind' "$work/remote.yaml")" ] \
  || fail "a vault-hub-ca Secret renders without hubCA"
pem=$'-----BEGIN CERTIFICATE-----\nMIIBfake\n-----END CERTIFICATE-----'
render "$work/remote-ca.yaml" "${remote[@]}" --set-string hubCA="$pem"
got=$(yq 'select(.kind == "Secret" and .metadata.name == "vault-hub-ca") | .data["ca.crt"] | @base64d' "$work/remote-ca.yaml")
[ "$got" = "$pem" ] || fail "Secret vault-hub-ca must hold hubCA under ca.crt"

# Vault's routes admit only the given source ranges: a Traefik ipAllowList
# Middleware, first in every rule's filters, before the rule's own.
route='gateway.routes=[{"name":"vault","hostnames":["vault.example.com"],"rules":[
  {"backendService":"vault","backendPort":8200,"filters":[{"type":"RequestHeaderModifier","requestHeaderModifier":{"set":[{"name":"X-A","value":"b"}]}}]},
  {"path":"/ui","backendService":"vault","backendPort":8200}]}]'
given='{"type":"RequestHeaderModifier","requestHeaderModifier":{"set":[{"name":"X-A","value":"b"}]}}'
allow='{"type":"ExtensionRef","extensionRef":{"group":"traefik.io","kind":"Middleware","name":"vault-source-range"}}'
# filters <render>: each rule's filters, one JSON list per line.
filters() { yq -o=json -I=0 'select(.kind == "HTTPRoute") | .spec.rules[] | .filters // []' "$1" | jq -cS .; }
render "$work/allow.yaml" --set gateway.enabled=true --set-json "$route" --set-json 'gateway.sourceRange=["198.51.100.0/24"]'
got=$(yq -o=json -I=0 'select(.kind == "Middleware" and .metadata.name == "vault-source-range") | .spec.ipAllowList.sourceRange' "$work/allow.yaml")
[ "$got" = '["198.51.100.0/24"]' ] || fail "the vault-source-range Middleware must hold gateway.sourceRange, got '$got'"
got=$(filters "$work/allow.yaml" | paste -sd' ' -)
want="$(jq -cS -n "[$allow, $given]") $(jq -cS -n "[$allow]")"
[ "$got" = "$want" ] || fail "with sourceRange, every rule's filters must start with the allowlist, got '$got'"
render "$work/open.yaml" --set gateway.enabled=true --set-json "$route"
[ -z "$(yq 'select(.kind == "Middleware") | .kind' "$work/open.yaml")" ] || fail "a Middleware renders without gateway.sourceRange"
got=$(filters "$work/open.yaml" | paste -sd' ' -)
[ "$got" = "$(jq -cS -n "[$given]") []" ] || fail "without sourceRange, the filters must stay as given, got '$got'"

[ "$failures" -eq 0 ] && echo "ok   vault-operator chart: monitors gated on the Prometheus Operator API, each selecting its Service and port; each Argo CD reads its own tier's path; VSO alone for a remote Vault; routes admit gateway.sourceRange"
exit "$failures"
