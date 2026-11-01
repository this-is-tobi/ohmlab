#!/usr/bin/env bash
# The names the charts give the resources an instance owns: a chart's own
# explicit value first, then the instance's name (global.ohmlab.names.<key>,
# which its manager gives every app), then the ohmlab name. Each case renders
# one chart, with the example values where the chart needs some to render,
# and reads only the name under test: no render is ever printed. Needs helm
# and yq (mikefarah v4).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EXAMPLE="$ROOT/argo-cd/instances/_example/values"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # check <label> <expected> <actual>
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

# render <chart> [<helm args>...]: renders argo-cd/apps/<chart> into
# $work/out.yaml (its dependencies built first when they are not vendored).
render() {
  local chart="$ROOT/argo-cd/apps/$1"
  shift
  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL cannot build the dependencies of $chart"; fail=1; }
  fi
  helm template t "$chart" "$@" >"$work/out.yaml" 2>"$work/err" || { echo "FAIL $chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; }
}
field() { yq ea -r "$1" "$work/out.yaml" | grep -v '^$' | grep -v '^---$' | paste -sd, -; }
# policy_paths <name>: the paths a policy of the Vault CR grants, in order.
policy_paths() {
  yq ea -r "select(.kind == \"Vault\") | .spec.externalConfig.policies[] | select(.name == \"$1\") | .rules" "$work/out.yaml" \
    | grep -o 'path "[^"]*"' | cut -d'"' -f2 | paste -sd, -
}

# Gateways: one name for every implementation.
traefik=(-f "$EXAMPLE/core/traefik.yaml" --set gateway.enabled=true --set gateway.name=)
render traefik "${traefik[@]}"
check "the Traefik Gateway falls back to ohmlab-gateway" ohmlab-gateway "$(field 'select(.kind == "Gateway") | .metadata.name')"
render traefik "${traefik[@]}" --set-string global.ohmlab.names.gateway=lab-gateway
check "the Traefik Gateway takes the instance's gateway name" lab-gateway "$(field 'select(.kind == "Gateway") | .metadata.name')"
render traefik "${traefik[@]}" --set-string global.ohmlab.names.gateway=lab-gateway --set gateway.name=x
check "the Traefik Gateway keeps its own explicit name" x "$(field 'select(.kind == "Gateway") | .metadata.name')"

route=(--set gateway.enabled=true --set-json 'gateway.routes=[{"name":"r","hostnames":["h.example"],"rules":[{"backendService":"s","backendPort":80}]}]')
render homepage "${route[@]}"
check "a route's parent falls back to ohmlab-gateway" ohmlab-gateway "$(field 'select(.kind == "HTTPRoute") | .spec.parentRefs[0].name')"
render homepage "${route[@]}" --set-string global.ohmlab.names.gateway=lab-gateway
check "a route's parent is the instance's gateway" lab-gateway "$(field 'select(.kind == "HTTPRoute") | .spec.parentRefs[0].name')"

# Vault: the core tier's secrets in the system mount (systemMount, ohmlab), the
# tenant tier's in the instance's.
kv='select(.kind == "Vault") | .spec.externalConfig.secrets[] | select(.type == "kv") | .path'
oidc='select(.kind == "VaultStaticSecret" and .metadata.name == "vault-oidc-credentials") | .spec.mount'
vault=(--set vault.enabled=true --set ohmlab.controller.enabled=true)
render vault-operator "${vault[@]}"
check "the Vault KV mount falls back to ohmlab" ohmlab "$(field "$kv")"
check "an app's policy reads both tiers in ohmlab" "ohmlab/data/platforms/+/core/gitea,ohmlab/data/platforms/+/tenant/gitea" "$(policy_paths gitea)"
check "the controller's policy covers the ohmlab mount once" "ohmlab/data/platforms/+/+/*,ohmlab/metadata/platforms/+/+/*" "$(policy_paths ohmlab-controller-vault)"
render vault-operator "${vault[@]}" --set-string global.ohmlab.names.vaultMount=lab
check "the Vault KV mounts are the system's, then the instance's" "ohmlab,lab" "$(field "$kv")"
check "an app's policy reads the core tier in ohmlab and the tenant tier in the instance's mount" "ohmlab/data/platforms/+/core/gitea,lab/data/platforms/+/tenant/gitea" "$(policy_paths gitea)"
check "Vault's OIDC credentials are read from the system mount" ohmlab "$(field "$oidc")"
render vault-operator "${vault[@]}" --set-string global.ohmlab.names.vaultMount=lab --set-string systemMount=lab
check "a system mount equal to the instance's is one KV mount" lab "$(field "$kv")"
check "the controller's policy covers that mount once" "lab/data/platforms/+/+/*,lab/metadata/platforms/+/+/*" "$(policy_paths ohmlab-controller-vault)"

# Keycloak: the instance's realm and the name it shows. Only those two fields
# of the realm import are read.
realm='select(.kind == "Secret" and .metadata.name == "keycloak-config-cli-config-realms") | .data["homelab.json"] | @base64d | from_json | .realm + "|" + .displayName'
render keycloak -f "$EXAMPLE/core/keycloak.yaml"
check "the realm falls back to ohmlab, shown as Ohmlab" "ohmlab|Ohmlab" "$(field "$realm")"
render keycloak -f "$EXAMPLE/core/keycloak.yaml" --set-string global.ohmlab.names.keycloakRealm=lab --set-string global.ohmlab.names.keycloakRealmDisplayName=Lab
check "the realm and the name it shows are the instance's" "lab|Lab" "$(field "$realm")"

exit "$fail"
