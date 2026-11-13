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
# One document at a time (eval, not eval-all, which pairs documents once an
# expression binds a variable).
field() { yq e -r "$1" "$work/out.yaml" | grep -v '^$' | grep -v '^---$' | paste -sd, -; }
# The example values leave every secret's mount and path to the manager: a
# chart rendered from them needs the manager's Vault parameters.
manager_vault=(--set-string global.ohmlab.vault.mount=ohmlab --set-string global.ohmlab.vault.pathPrefix=platforms/production/core)
# policy_paths <name>: the paths a policy of the Vault CR grants, in order.
policy_paths() {
  yq ea -r "select(.kind == \"Vault\") | .spec.externalConfig.policies[] | select(.name == \"$1\") | .rules" "$work/out.yaml" \
    | grep -o 'path "[^"]*"' | cut -d'"' -f2 | paste -sd, -
}

# Gateways: one name for every implementation.
traefik=(-f "$EXAMPLE/core/traefik.yaml" "${manager_vault[@]}" --set gateway.enabled=true --set gateway.name=)
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
check "an app's policy reads both tiers in ohmlab, its claims' records too" "ohmlab/data/platforms/+/core/gitea,ohmlab/data/platforms/+/tenant/gitea,ohmlab/data/platforms/+/core/gitea/s3,ohmlab/data/platforms/+/tenant/gitea/s3" "$(policy_paths gitea)"
check "the controller's policy covers the ohmlab mount once, and its claims' records" "ohmlab/data/platforms/+/+/*,ohmlab/metadata/platforms/+/+/*,ohmlab/data/platforms/+/+/+/s3,ohmlab/metadata/platforms/+/+/+/s3" "$(policy_paths ohmlab-controller-vault)"
render vault-operator "${vault[@]}" --set-string global.ohmlab.names.vaultMount=lab
check "the Vault KV mounts are the system's, then the instance's" "ohmlab,lab" "$(field "$kv")"
check "an app's policy reads the core tier in ohmlab and the tenant tier in the instance's mount, their claims' records too" "ohmlab/data/platforms/+/core/gitea,lab/data/platforms/+/tenant/gitea,ohmlab/data/platforms/+/core/gitea/s3,lab/data/platforms/+/tenant/gitea/s3" "$(policy_paths gitea)"
check "Vault's OIDC credentials are read from the system mount" ohmlab "$(field "$oidc")"
render vault-operator "${vault[@]}" --set-string global.ohmlab.names.vaultMount=lab --set-string systemMount=lab
check "a system mount equal to the instance's is one KV mount" lab "$(field "$kv")"
check "the controller's policy covers that mount once, and its claims' records" "lab/data/platforms/+/+/*,lab/metadata/platforms/+/+/*,lab/data/platforms/+/+/+/s3,lab/metadata/platforms/+/+/+/s3" "$(policy_paths ohmlab-controller-vault)"

# A chart's VaultStaticSecrets read the mount the manager gives its tier
# (global.ohmlab.vault.mount), under the manager's path prefix and the app's own
# segment (global.ohmlab.vault.pathPrefix), unless an entry sets its own.
vss='select(.kind == "VaultStaticSecret") | .spec.mount + " " + .spec.path'
vault_params=(--set-string global.ohmlab.vault.mount=m)
render gitea -f "$EXAMPLE/tenant/gitea.yaml" "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/tenant
check "gitea's secrets take the manager's mount and path" "m platforms/p/tenant/gitea" "$(yq ea -r "$vss" "$work/out.yaml" | grep -v '^---$' | sort -u | paste -sd, -)"
render cilium -f "$EXAMPLE/core/cilium.yaml" "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/core
check "cilium's secrets are under hubble" "m platforms/p/core/hubble" "$(yq ea -r "$vss" "$work/out.yaml" | grep -v '^---$' | sort -u | paste -sd, -)"
render coder -f "$EXAMPLE/tenant/coder.yaml" "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/tenant
check "coder's secrets take the manager's mount and path" "m platforms/p/tenant/coder" "$(yq ea -r "$vss" "$work/out.yaml" | grep -v '^---$' | sort -u | paste -sd, -)"
# What coder reads must be what its VaultStaticSecrets write: the Secret keys
# its pods read, and the Secrets its database cluster and backups read.
lines() { yq e -r "$1" "$work/out.yaml" | grep -v -e '^---$' -e '^$' -e '^null$' | sort -u; }
lines 'select(.kind == "VaultStaticSecret") | .spec.destination.name as $n | .spec.destination.transformation.templates | keys | .[] | $n + "/" + .' >"$work/written"
lines '.. | select(tag == "!!map" and has("secretKeyRef")) | .secretKeyRef.name + "/" + .secretKeyRef.key' >"$work/read"
check "every Secret key coder's pods read is one its VaultStaticSecrets write" "" "$(comm -23 "$work/read" "$work/written" | paste -sd, -)"
lines 'select(.kind == "VaultStaticSecret") | .spec.destination.name' >"$work/written"
lines 'select(.kind == "Cluster" or .kind == "ObjectStore") | .. | select(tag == "!!map") | (.superuserSecret.name, .secret.name, .accessKeyId.name, .secretAccessKey.name)' >"$work/read"
check "every Secret coder's database reads is one its VaultStaticSecrets write" "" "$(comm -23 "$work/read" "$work/written" | paste -sd, -)"
if helm template t "$ROOT/argo-cd/apps/gitea" -f "$EXAMPLE/tenant/gitea.yaml" "${vault_params[@]}" >/dev/null 2>"$work/err"; then
  echo "FAIL gitea renders its secrets without a path"; fail=1
elif grep -q 'path is required' "$work/err"; then echo "ok   a secret without a path prefix nor a path of its own does not render"; else
  echo "FAIL a secret without a path: expected 'path is required', got: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1
fi
# Every app's VaultAuth logs in the way its instance says: the jwt method at
# jwt-<instance> for an instance that reads another instance's Vault
# (global.ohmlab.vault.auth), else kubernetes; only that method's block renders.
auth_spec() { yq e -o=json -I=0 'select(.kind == "VaultAuth") | .spec' "$work/out.yaml" | jq -cS .; }
jwt=(--set-string global.ohmlab.vault.auth.method=jwt --set-string global.ohmlab.vault.auth.mount=jwt-dev)
for app in tenant/gitea core/cert-manager; do
  name=${app#*/}
  render "$name" -f "$EXAMPLE/$app.yaml" "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/${app%/*} "${jwt[@]}"
  check "$name: on another instance's Vault, its VaultAuth logs in with jwt, and only jwt" \
    "{\"jwt\":{\"audiences\":[\"vault\"],\"role\":\"$name\",\"serviceAccount\":\"vso\"},\"method\":\"jwt\",\"mount\":\"jwt-dev\"}" "$(auth_spec)"
  render "$name" -f "$EXAMPLE/$app.yaml" "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/${app%/*}
  check "$name: on its own cluster's Vault, its VaultAuth logs in with kubernetes" \
    "{\"kubernetes\":{\"audiences\":[\"vault\"],\"role\":\"$name\",\"serviceAccount\":\"vso\"},\"method\":\"kubernetes\",\"mount\":\"kubernetes\"}" "$(auth_spec)"
done
helm template t "$ROOT/charts/ohmlab" --set vso.enabled=true --set vso.vaultAuth.default.kubernetes.role=argo-cd-core >"$work/out.yaml" 2>"$work/err" \
  || { echo "FAIL charts/ohmlab does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; }
check "the ohmlab chart: an instance that sets only kubernetes.role keeps the service account and audiences" \
  '{"kubernetes":{"audiences":["vault"],"role":"argo-cd-core","serviceAccount":"vso"},"method":"kubernetes","mount":"kubernetes"}' "$(auth_spec)"
# No app renders a VaultConnection of its own: every VaultAuth leaves
# vaultConnectionRef out, so VSO uses its operator-wide default connection,
# vault-operator's `default`, the only one.
connections=""
for chart in "$ROOT/charts/ohmlab" $(grep -l 'name: vso-utils' "$ROOT"/argo-cd/apps/*/Chart.yaml | xargs -n1 dirname) "$ROOT/argo-cd/apps/vault-operator"; do
  name=$(basename "$chart")
  example=()
  for scope in core tenant; do
    [ -f "$EXAMPLE/$scope/$name.yaml" ] && { example=(-f "$EXAMPLE/$scope/$name.yaml"); break; }
  done
  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL cannot build the dependencies of $chart"; fail=1; }
  fi
  helm template t "$chart" "${example[@]+"${example[@]}"}" --set vso.enabled=true "${vault_params[@]}" \
    --set-string global.ohmlab.vault.pathPrefix=platforms/p/core >"$work/out.yaml" 2>"$work/err" \
    || { echo "FAIL $name does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; }
  connections+=$(yq e -r "select(.kind == \"VaultConnection\") | \"$name:\" + .metadata.name, select(.kind == \"VaultAuth\" and .spec.vaultConnectionRef) | \"$name:ref=\" + .spec.vaultConnectionRef" "$work/out.yaml" | grep -v '^---$')$'\n'
done
check "the only VaultConnection is vault-operator's default, the only reference to it vault-operator's own" \
  "vault-operator:default,vault-operator:ref=default" "$(grep -v '^$' <<<"$connections" | sort | paste -sd, -)"
oidc_at='select(.kind == "VaultStaticSecret" and .metadata.name == "vault-oidc-credentials") | .spec.mount + " " + .spec.path'
render vault-operator --set vault.enabled=true "${vault_params[@]}" --set-string global.ohmlab.vault.pathPrefix=platforms/p/core
check "Vault's OIDC credentials take the manager's mount and path" "m platforms/p/core/keycloak" "$(field "$oidc_at")"
render vault-operator --set vault.enabled=true
check "without the manager, Vault's OIDC credentials are at ohmlab, platforms/production/core/keycloak" "ohmlab platforms/production/core/keycloak" "$(field "$oidc_at")"

# Keycloak: the system realm, ohmlab, holds the core apps' clients; the
# instance's realm the tenant apps'. Only realm names, display names, client IDs,
# group paths and user counts of the realm import are read.
realms() { # realms <jq on {<file>: <realm>}>
  yq ea -o=json -I0 'select(.kind == "Secret" and .metadata.name == "keycloak-config-cli-config-realms") | .data' "$work/out.yaml" \
    | jq -r "map_values(@base64d | fromjson) | $1"
}
ids='[.clients[].clientId] | sort | join(",")'
shape='([.groups[].path] | join(",")) + " " + (.users | length | tostring)'
core=argo-cd-core,grafana-core,hubble-core,longhorn-core,vault-core
tenant=argo-cd,gitea,rustfs
all=argo-cd,argo-cd-core,gitea,grafana-core,hubble-core,longhorn-core,rustfs,vault-core
render keycloak -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}" --set-string global.ohmlab.names.keycloakRealm=lab --set-string global.ohmlab.names.keycloakRealmDisplayName=Lab
check "one realm file each, named after the realm" lab.json,ohmlab.json "$(realms 'keys | join(",")')"
check "the system realm is ohmlab, shown as Ohmlab" "ohmlab|Ohmlab" "$(realms '.["ohmlab.json"] | .realm + "|" + .displayName')"
check "the system realm holds the core clients" "$core" "$(realms ".[\"ohmlab.json\"] | $ids")"
check "the instance's realm and the name it shows are the instance's" "lab|Lab" "$(realms '.["lab.json"] | .realm + "|" + .displayName')"
check "the instance's realm holds the tenant clients" "$tenant" "$(realms ".[\"lab.json\"] | $ids")"
check "the instance's admin group carries the tenant client roles" rustfs "$(realms '.["lab.json"] | [.groups[].clientRoles | keys[]] | join(",")')"
check "the instance's client roles are the tenant's" rustfs "$(realms '.["lab.json"] | .roles.client | keys | join(",")')"
check "both realms have the admin group and one user" "/admin 1|/admin 1" "$(realms ".[\"ohmlab.json\"] | $shape")|$(realms ".[\"lab.json\"] | $shape")"
check "the system realm's admin group carries the core client roles" grafana-core "$(realms '.["ohmlab.json"] | [.groups[].clientRoles | keys[]] | join(",")')"
check "the config-cli Job reads that Secret" keycloak-config-cli-config-realms "$(field 'select(.kind == "Job" and .metadata.name == "keycloak-config-cli") | .spec.template.spec.volumes[] | select(.name == "config") | .secret.secretName')"
check "no KEYCLOAK_REALM in the config Secret nor in the config-cli env" "" "$(field 'select(.kind == "VaultStaticSecret" and .spec.destination.name == "keycloak-config") | .spec.destination.transformation.templates | keys | .[] | select(. == "KEYCLOAK_REALM")')$(field 'select(.kind == "Job" and .metadata.name == "keycloak-config-cli") | .spec.template.spec.containers[].env[]?.name | select(. == "KEYCLOAK_REALM")')"
render keycloak -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}"
check "without names, the two realms are one: ohmlab" ohmlab.json "$(realms 'keys | join(",")')"
check "that realm is shown as Ohmlab" "ohmlab|Ohmlab" "$(realms '.["ohmlab.json"] | .realm + "|" + .displayName')"
check "that realm holds both tiers' clients" "$all" "$(realms ".[\"ohmlab.json\"] | $ids")"
check "its admin group carries both tiers' client roles" grafana-core,rustfs "$(realms '.["ohmlab.json"] | [.groups[] | select(.path == "/admin") | .clientRoles | keys[]] | sort | join(",")')"
check "its client roles are both tiers'" grafana-core,rustfs "$(realms '.["ohmlab.json"] | .roles.client | keys | sort | join(",")')"
check "it has one admin group and one user" "/admin 1" "$(realms ".[\"ohmlab.json\"] | $shape")"
# A remote instance's own Keycloak leaves the system realm out (system: null)
# and imports its own realm alone.
render keycloak -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}" --set-string global.ohmlab.names.keycloakRealm=lab --set-string global.ohmlab.names.keycloakRealmDisplayName=Lab --set realms.system=null
check "without the system realm, only the instance's realm file" lab.json "$(realms 'keys | join(",")')"
check "that realm holds the tenant clients" "$tenant" "$(realms ".[\"lab.json\"] | $ids")"
# An instance that reads another instance's Vault (global.ohmlab.vault.hub)
# has no system realm either, nor the core clients' secrets, which only the
# hub's Vault holds.
core_vss='select(.kind == "VaultStaticSecret" and (.spec.destination.name | test("^keycloak-clients-.*-core$"))) | .spec.destination.name'
core_env='select(.kind == "Job" and .metadata.name == "keycloak-config-cli") | .spec.template.spec.containers[].env[] | select(.name | test("_CORE_CLIENT_SECRET$")) | .valueFrom.secretKeyRef.optional'
render keycloak -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}" --set-string global.ohmlab.names.keycloakRealm=lab --set-string global.ohmlab.vault.hub=platform
check "a remote instance's Keycloak imports its own realm alone" lab.json "$(realms 'keys | join(",")')"
check "it renders none of the core clients' secrets" "" "$(field "$core_vss")"
check "its config-cli Job takes the core clients' secrets as optional" true,true,true,true,true "$(field "$core_env")"
render keycloak -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}"
check "without a hub, the core clients' five secrets render" 5 "$(field "$core_vss" | tr ',' '\n' | grep -c .)"
if helm template t "$ROOT/argo-cd/apps/keycloak" -f "$EXAMPLE/core/keycloak.yaml" "${manager_vault[@]}" --set realms.system=null --set realms.instance=null >/dev/null 2>"$work/err"; then
  echo "FAIL a Keycloak with neither realm rendered"; fail=1
elif grep -q 'neither' "$work/err"; then echo "ok   a Keycloak with neither realm fails"
else echo "FAIL a Keycloak with neither realm fails for another reason: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1
fi

exit "$fail"
