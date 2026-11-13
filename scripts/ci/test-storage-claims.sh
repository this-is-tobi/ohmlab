#!/usr/bin/env bash
# The storage-claims chart: from the claims the instance-manager gives a tier
# and the tier file's apps, the S3Buckets of the enabled apps, the store's
# NetworkPolicy admitting their clients and, with networkPolicies, their
# clients' egress to it.
# The claims come from the instance-manager itself, in the homelab's shape.
# Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/storage-claims"
MANAGER="$ROOT/argo-cd/apps/instance-manager"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

cat >"$work/instance.yaml" <<'EOF'
instance: {name: homelab, env: production}
repoURL: https://git.example/repo.git
storage:
  stores:
    rustfs-backups: {app: rustfs-backups, namespace: rustfs-backups, endpoint: "https://rustfs-svc.rustfs-backups.svc:9000", size: 50Gi}
  backups: rustfs-backups
  networkPolicies: true
EOF
for tier in core tenant; do
  helm template m "$MANAGER" -f "$work/instance.yaml" \
    | N="storage-$tier-homelab" yq ea -o=json -I=0 'select(.metadata.name == strenv(N)) | .spec.generators[0].matrix.generators[1].list.elements[0].claimsValues' - >"$work/$tier.json"
done
cat >"$work/core-apps.yaml" <<'EOF'
apps:
- {app: keycloak, enabled: "true"}
- {app: crowdsec, enabled: "true"}
- {app: vault-operator, enabled: "true", namespace: vault-operator-system}
- {app: homepage, enabled: "true"}
EOF
printf 'apps:\n- {app: gitea, enabled: true}\n' >"$work/tenant-apps.yaml"

# render <tier> <apps file> [<helm args>...]: the chart's objects, one JSON per line.
render() {
  local tier="$1" apps="$2"
  shift 2
  helm template "storage-$tier" "$CHART" -f "$work/$tier.json" -f "$apps" "$@" | yq ea -o=json -I=0 'select(. != null and .kind != null)' -
}
# names <objects> <kind>: the names of that kind, sorted, as JSON.
names() { jq -sc --arg k "$2" '[.[] | select(.kind == $k) | .metadata.namespace + "/" + .metadata.name] | sort' <<<"$1"; }
# admitted <objects> <policy>: whom the store's policy admits, sorted, as JSON.
admitted() {
  jq -sc --arg n "$2" '[.[] | select(.kind == "NetworkPolicy" and .metadata.name == $n) | .spec.ingress[0].from[]
    | .namespaceSelector.matchLabels["kubernetes.io/metadata.name"] + " " + (.podSelector.matchLabels | to_entries | sort_by(.key) | map(.key + "=" + .value) | join(","))] | sort' <<<"$1"
}

core=$(render core "$work/core-apps.yaml")
check "core: an S3Bucket per claim of the enabled apps, in the controller's namespace" \
  '["ohmlab-system/ohmlab-crowdsec-pg","ohmlab-system/ohmlab-keycloak-pg","ohmlab-system/ohmlab-vault-operator-vault"]' "$(names "$core" S3Bucket)"
check "core: keycloak's S3Bucket holds its claim, kept on deletion" \
  '{"labels":{"ohmlab.fr/app-name":"keycloak","ohmlab.fr/claim":"pg"},"spec":{"access":"readWrite","bucket":"ohmlab-keycloak-pg","credentials":{"key":"pg","path":"ohmlab/platforms/production/core/keycloak/s3"},"deletionPolicy":"Retain","lifecycle":{"expireDays":0,"noncurrentDays":7},"quota":"2Gi","replicate":true,"store":"rustfs-backups","versioning":true}}' \
  "$(jq -cS 'select(.kind == "S3Bucket" and .metadata.name == "ohmlab-keycloak-pg") | {labels: .metadata.labels, spec}' <<<"$core")"
check "core: crowdsec's quota and vault-operator's record, key and quota" \
  '["5Gi","ohmlab/platforms/production/core/vault-operator/s3 vault 3Gi"]' \
  "$(jq -sc '[(.[] | select(.metadata.name == "ohmlab-crowdsec-pg") | .spec.quota), (.[] | select(.metadata.name == "ohmlab-vault-operator-vault") | .spec | "\(.credentials.path) \(.credentials.key) \(.quota)")]' <<<"$core")"
check "core: a NetworkPolicy per claim in its app's namespace, and one for the store" \
  '["crowdsec/ohmlab-storage-pg","keycloak/ohmlab-storage-pg","rustfs-backups/ohmlab-storage-core","vault-operator-system/ohmlab-storage-vault"]' "$(names "$core" NetworkPolicy)"
check "core: keycloak's Postgres pods reach the store on its port" \
  '{"egress":[{"ports":[{"port":9000,"protocol":"TCP"}],"to":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"rustfs-backups"}},"podSelector":{"matchLabels":{"app.kubernetes.io/name":"rustfs"}}}]}],"podSelector":{"matchLabels":{"cnpg.io/cluster":"keycloak-pg-cluster"}},"policyTypes":["Egress"]}' \
  "$(jq -cS 'select(.kind == "NetworkPolicy" and .metadata.namespace == "keycloak") | .spec' <<<"$core")"
check "core: the store admits the three clients and the controller's storage target" \
  '["crowdsec cnpg.io/cluster=crowdsec-pg-cluster","keycloak cnpg.io/cluster=keycloak-pg-cluster","ohmlab-system app.kubernetes.io/component=storage,app.kubernetes.io/name=ohmlab-controller","vault-operator-system ohmlab.fr/storage-client=vault"]' \
  "$(admitted "$core" ohmlab-storage-core)"
check "core: the store's policy is an ingress rule on the RustFS pods, on the port" \
  '{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"rustfs"}},"policyTypes":["Ingress"],"ports":[{"port":9000,"protocol":"TCP"}]}' \
  "$(jq -cS 'select(.metadata.name == "ohmlab-storage-core") | .spec | {podSelector, policyTypes, ports: .ingress[0].ports}' <<<"$core")"

yq '.apps[0].enabled = "false"' "$work/core-apps.yaml" >"$work/off.yaml"
off=$(render core "$work/off.yaml")
check "a disabled app gets no bucket" '["ohmlab-system/ohmlab-crowdsec-pg","ohmlab-system/ohmlab-vault-operator-vault"]' "$(names "$off" S3Bucket)"
check "a disabled app gets no NetworkPolicy and no rule at the store" \
  '["crowdsec/ohmlab-storage-pg","rustfs-backups/ohmlab-storage-core","vault-operator-system/ohmlab-storage-vault"] 3' \
  "$(names "$off" NetworkPolicy) $(admitted "$off" ohmlab-storage-core | jq length)"
yq '.apps[0].namespace = "keycloak-system"' "$work/core-apps.yaml" >"$work/moved.yaml"
moved=$(render core "$work/moved.yaml")
check "an entry's own namespace takes its NetworkPolicy and its rule at the store" \
  'keycloak-system/ohmlab-storage-pg true' \
  "$(names "$moved" NetworkPolicy | jq -r 'map(select(startswith("keycloak"))) | .[0]') $(admitted "$moved" ohmlab-storage-core | jq 'any(startswith("keycloak-system "))')"
long=$(printf 'a%.0s' $(seq 1 64))
if err=$(helm template s "$CHART" -f "$work/core.json" -f "$work/core-apps.yaml" --set "claims.keycloak.pg.bucket=$long" 2>&1 >/dev/null); then
  echo "FAIL a 64-character bucket is refused: the chart rendered"; fail=1
else
  check "a 64-character bucket is refused" "yes" \
    "$(grep -qF "storage claim keycloak.pg: bucket \"$long\" is not a valid bucket name (3 to 63 lowercase letters, digits or hyphens): shorten the instance name or set names.vaultMount" <<<"$err" && echo yes)"
fi

tenant=$(render tenant "$work/tenant-apps.yaml")
check "tenant: gitea's bucket, on the instance's mount" '["ohmlab-system/homelab-gitea-pg"]' "$(names "$tenant" S3Bucket)"
check "tenant: the store admits gitea and not the controller" '["gitea cnpg.io/cluster=gitea-pg-cluster"]' "$(admitted "$tenant" ohmlab-storage-tenant)"

printf 'apps:\n- {app: homepage, enabled: "true"}\n' >"$work/no-claims.yaml"
check "core admits the controller into the store with no claim to render" \
  '["ohmlab-system app.kubernetes.io/component=storage,app.kubernetes.io/name=ohmlab-controller"]' \
  "$(admitted "$(render core "$work/no-claims.yaml")" ohmlab-storage-core)"
check "tenant renders nothing with no claim" "[]" "$(render tenant "$work/no-claims.yaml" | jq -sc 'map(.kind)')"
nonp=$(render core "$work/core-apps.yaml" --set networkPolicies=false)
check "without networkPolicies, no client policy, and the store still admits the clients and the controller" \
  '["rustfs-backups/ohmlab-storage-core"] 4' "$(names "$nonp" NetworkPolicy) $(admitted "$nonp" ohmlab-storage-core | jq length)"
check "the chart's defaults render nothing" "" "$(helm template s "$CHART" | yq ea 'select(.kind != null) | .kind' -)"

exit "$fail"
