#!/usr/bin/env bash
# Tests of the vault-operator chart's monitors for the two operators it
# installs, bank-vaults and the Vault Secrets Operator: like the chart's Vault
# monitor they render only where the Prometheus Operator API exists (Argo CD
# refuses a whole sync over one unknown kind, and the core apps that bring that
# API need Vault first), and each selects the one rendered Service that carries
# its endpoint's port, so a subchart that renames a label or a port fails here
# rather than as a target that is never scraped. Then the shape of a cluster
# that reads a remote Vault: VSO alone, its default connection on the hub.
# Last, the catalog's Keycloak client allows only the callback Vault's UI sends.
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

# Vault's UI signs in through <host>/ui/vault/auth/<its oidc mount>/oidc/callback.
# The Keycloak client allows that callback alone: a URI no flow sends only
# widens where Keycloak redirects.
render "$work/oidc.yaml" --set vault.externalConfig.oidcEnabled=true
mount=$(yq 'select(.kind == "Vault") | .spec.externalConfig.auth[] | select(.type == "oidc") | .path' "$work/oidc.yaml")
want="https://vault.core.\$(env:ROOT_DOMAIN)/ui/vault/auth/$mount/oidc/callback"
got=$(yq '[.realms.system.clients[] | select(.clientId == "vault-core") | .redirectUris[]] | join(" ")' "$ROOT/argo-cd/apps/keycloak/values.yaml")
[ "$got" = "$want" ] || fail "the Keycloak vault-core client must allow Vault's UI callback alone ($want), got '$got'"

# Object storage. The vault target leaves the claims' records alone; the
# storage target gets a policy and a role once the instance-manager gives it
# stores; the snapshot job logs in as a ServiceAccount of its own and writes
# to the vault-operator claim.
# policy <render> <name>: the policy's rules, without comments, indentation or repeated spaces.
policy() {
  yq -o=json -I=0 "select(.kind == \"Vault\") | .spec.externalConfig.policies[] | select(.name == \"$2\") | .rules" "$1" | jq -r . \
    | sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/[[:space:]][[:space:]]*/ /g' | sed '/^$/d'
}
role() { yq -o=json -I=0 "select(.kind == \"Vault\") | .spec.externalConfig.auth[] | select(.type == \"kubernetes\") | .roles[] | select(.name == \"$2\")" "$1" | jq -cS .; }

render "$work/nostores.yaml" --set ohmlab.controller.enabled=true
[ -z "$(policy "$work/nostores.yaml" ohmlab-controller-storage)$(role "$work/nostores.yaml" ohmlab-controller-storage)" ] \
  || fail "the storage target's policy or role renders without stores"
got=$(policy "$work/nostores.yaml" ohmlab-controller-vault | jq -Rsc 'split("\n") | map(select(test("deny")))')
want='["capabilities = [\"deny\"]","capabilities = [\"deny\"]","capabilities = [\"deny\"]","capabilities = [\"deny\"]"]'
[ "$got" = "$want" ] || fail "the vault target's policy must deny the claims' records, data and metadata, on both mounts, got $got"
grep -q '^path "example/metadata/platforms/+/+/+/s3" {$' <(policy "$work/nostores.yaml" ohmlab-controller-vault) \
  || fail "the vault target's policy must deny the instance mount's claim metadata"

render "$work/stores.yaml" --set-json 'ohmlab.controller.storage.stores=[{"name":"rustfs-backups","path":"ohmlab/platforms/production/core/rustfs-backups"}]'
want='path "ohmlab/data/platforms/+/+/+/s3" { capabilities = ["create", "update", "read"] }
path "ohmlab/metadata/platforms/+/+/+/s3" { capabilities = ["read", "update"] }
path "example/data/platforms/+/+/+/s3" { capabilities = ["create", "update", "read"] }
path "example/metadata/platforms/+/+/+/s3" { capabilities = ["read", "update"] }
path "ohmlab/data/platforms/production/core/rustfs-backups" { capabilities = ["read"] }'
got=$(policy "$work/stores.yaml" ohmlab-controller-storage)
[ "$got" = "$want" ] || fail "the storage target's policy must write the records on both mounts and read the store's root keys, got: $got"
got=$(role "$work/stores.yaml" ohmlab-controller-storage)
want='{"audience":"vault","bound_service_account_names":["ohmlab-controller-storage"],"bound_service_account_namespaces":["ohmlab-system"],"name":"ohmlab-controller-storage","policies":["ohmlab-controller-storage"],"ttl":"15m"}'
[ "$got" = "$want" ] || fail "the storage target's role must be bound to ohmlab-controller-storage, got $got"

render "$work/backup.yaml" --set backup-utils.enabled=true --set global.ohmlab.storage.vault.enabled=true
got=$(yq -o=json -I=0 'select(.kind == "ServiceAccount" and .metadata.name == "vault-backup") | .automountServiceAccountToken' "$work/backup.yaml")
[ "$got" = false ] || fail "the ServiceAccount vault-backup must not mount its token, got '$got'"
got=$(policy "$work/backup.yaml" vault-backup)
[ "$got" = 'path "sys/storage/raft/snapshot" { capabilities = ["read"] }' ] || fail "the vault-backup policy must read the raft snapshot alone, got: $got"
got=$(role "$work/backup.yaml" vault-backup)
want='{"audience":"vault","bound_service_account_names":["vault-backup"],"bound_service_account_namespaces":["vault-operator-system"],"name":"vault-backup","policies":["vault-backup"],"ttl":"15m"}'
[ "$got" = "$want" ] || fail "the vault-backup role must be bound to vault-backup in the release's namespace, got $got"
got=$(yq -o=json -I=0 'select(.kind == "VaultStaticSecret" and .metadata.name == "vault-backup") | .spec' "$work/backup.yaml" \
  | jq -c '{path, secret: .destination.name, keys: (.destination.transformation.templates | keys | sort)}')
want='{"path":"platforms/production/core/vault-operator/s3","secret":"vault-backup","keys":["RCLONE_S3_REGION","S3_ACCESS_KEY","S3_BUCKET_NAME","S3_ENDPOINT","S3_PATH_STYLE","S3_SECRET_KEY","ca.crt"]}'
[ "$(jq -cS . <<<"$got")" = "$(jq -cS . <<<"$want")" ] || fail "the VaultStaticSecret vault-backup must read the claim's record into Secret vault-backup's seven keys, got $got"
# The image configures rclone without a region; its environment overrides that.
got=$(yq -o=json -I=0 'select(.kind == "VaultStaticSecret" and .metadata.name == "vault-backup") | .spec.destination.transformation.templates.RCLONE_S3_REGION.text' "$work/backup.yaml")
[ "$got" = '"{{ get .Secrets \"vault\" | dig \"region\" \"\" }}"' ] || fail "rclone must sign with the record's region, got $got"
render "$work/backup-off.yaml" --set backup-utils.enabled=true
[ -z "$(yq 'select(.kind == "VaultStaticSecret" and .metadata.name == "vault-backup") | .kind' "$work/backup-off.yaml")" ] \
  || fail "the VaultStaticSecret vault-backup renders without the vault claim"
cron=$(yq -o=json -I=0 'select(.kind == "CronJob" and .metadata.name == "backup-vault-snapshot") | .spec' "$work/backup.yaml")
got=$(jq -c '{schedule, sa: .jobTemplate.spec.template.spec.serviceAccountName, token: .jobTemplate.spec.template.spec.automountServiceAccountToken,
  client: .jobTemplate.spec.template.metadata.labels["ohmlab.fr/storage-client"], command: .jobTemplate.spec.template.spec.containers[0].command}' <<<"$cron")
want='{"schedule":"0 22 * * *","sa":"vault-backup","token":false,"client":"vault","command":["/bin/bash","-c","export VAULT_TOKEN=\"$(vault write -field=token auth/kubernetes/login role=vault-backup jwt=@/var/run/secrets/ohmlab/vault/token)\" && exec ./scripts/vault-backup.sh"]}'
[ "$got" = "$want" ] || fail "the snapshot CronJob must run at 22:00 as vault-backup, labelled a storage client, logging in first, got $got"
got=$(jq -c '[.jobTemplate.spec.template.spec as $p | $p.containers[0].volumeMounts[] | .name as $n | {path: .mountPath, readOnly, from: ($p.volumes[] | select(.name == $n) | (.secret // .projected) | if .secretName then "\(.secretName)/\(.items | map(.key) | join(","))" else (.sources[0].serviceAccountToken | "token:\(.audience)/\(.path)") end)}] | sort_by(.path)' <<<"$cron")
want='[{"path":"/etc/ohmlab/store-ca","readOnly":true,"from":"vault-backup/ca.crt"},{"path":"/etc/ohmlab/vault-ca","readOnly":true,"from":"vault-tls/ca.crt"},{"path":"/var/run/secrets/ohmlab/vault","readOnly":true,"from":"token:vault/token"}]'
[ "$got" = "$want" ] || fail "the snapshot CronJob must mount Vault's CA, the store's CA and a vault-audience token, got $got"
# The kyverno chart's require-non-root (Enforce) admits a pod with a non-zero
# user, and the image's user is a name the kubelet cannot check.
got=$(jq -cS '.jobTemplate.spec.template.spec | {pod: .securityContext, container: .containers[0].securityContext}' <<<"$cron")
want='{"container":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}},"pod":{"fsGroup":1000,"runAsGroup":1000,"runAsNonRoot":true,"runAsUser":1000,"seccompProfile":{"type":"RuntimeDefault"}}}'
[ "$got" = "$want" ] || fail "the snapshot pod must run as the image's user 1000, without privilege escalation, got $got"
got=$(yq -o=json -I=0 'select(.kind == "ConfigMap" and .metadata.name == "backup-vault-snapshot") | .data | keys' "$work/backup.yaml")
[ "$got" = '["RCLONE_CA_CERT","RETENTION","VAULT_ADDR","VAULT_CACERT"]' ] || fail "the snapshot job's settings must name both CAs and drop VAULT_SKIP_VERIFY, got $got"
got=$(yq -o=json -I=0 'select(.kind == "ConfigMap" and .metadata.name == "backup-vault-snapshot") | .data' "$work/backup.yaml" | jq -c '[.VAULT_CACERT, .RCLONE_CA_CERT, .RETENTION]')
[ "$got" = '["/etc/ohmlab/vault-ca/ca.crt","/etc/ohmlab/store-ca/ca.crt","30d"]' ] || fail "the snapshot job's CA paths and retention, got $got"

[ "$failures" -eq 0 ] && echo "ok   vault-operator chart: monitors gated on the Prometheus Operator API, each selecting its Service and port; each Argo CD reads its own tier's path; VSO alone for a remote Vault; routes admit gateway.sourceRange; Keycloak allows Vault's UI callback alone; the storage target's policy and role, the vault target kept off the claims' records, the snapshot job's identity, record and CA mounts"
exit "$failures"
