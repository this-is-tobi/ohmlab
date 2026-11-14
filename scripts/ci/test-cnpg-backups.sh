#!/usr/bin/env bash
# The Postgres backups of the CNPG apps. Off by default: no ObjectStore, no
# ScheduledBackup, and the backup Secret reads the app's own path. With the
# values the instance-manager gives the app's pg claim: an ObjectStore at the
# claim's bucket with the store's CA and gzip, synced before the Cluster and
# not as a Helm hook, a daily ScheduledBackup at the app's minute, base backups
# on the primary, and the Secret reading the claim's record. Needs helm, yq and
# jq. No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

STORE=https://rustfs-svc.rustfs-backups.svc:9000
cat >"$work/instance.yaml" <<EOF
instance: {name: lab, env: production}
repoURL: https://git.example/repo.git
storage:
  stores:
    backups: {app: rustfs-backups, namespace: rustfs-backups, endpoint: "$STORE", size: 50Gi}
  backups: backups
EOF
helm template m "$ROOT/argo-cd/apps/instance-manager" -f "$work/instance.yaml" \
  | yq ea -o=json -I=0 'select(.metadata.name == "core-lab") | .spec.generators[0].merge.generators[1].list.elements[]' - >"$work/elements.jsonl"

# The base backups, five minutes apart from 20:00 UTC.
for entry in crowdsec:0 gitea:5 keycloak:10 mattermost:15 prometheus-stack:20 outline:25 harbor:30 sonarqube:35 mlflow:40 vaultwarden:45 coder:50; do
  app="${entry%%:*}" minute="${entry#*:}"
  vf=$(values_file "$app")
  args=()
  if [ -z "$vf" ] || ! app_args "$vf"; then echo "FAIL $app: no values file or manager parameters"; fail=1; continue; fi
  # The catalog's backups: the instance's values (args[1], app_args' -f)
  # without their own cnpg.backups.
  yq 'del(.cnpg.backups)' "$vf" >"$work/values.yaml"
  args[1]="$work/values.yaml"
  # secret <render>: the spec of the VaultStaticSecret that writes the backup Secret.
  secret() { yq -o=json -I=0 "select(.kind == \"VaultStaticSecret\" and .spec.destination.name == \"$app-pg-cluster-backup\") | .spec" "$1"; }

  if ! helm template "$app" "$ROOT/argo-cd/apps/$app" "${args[@]}" >"$work/plain.yaml" 2>"$work/err"; then
    echo "FAIL $app: the plain render fails: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; continue
  fi
  check "$app: no ObjectStore and no ScheduledBackup by default" "" \
    "$(yq ea 'select(.kind == "ObjectStore" or .kind == "ScheduledBackup") | .kind' "$work/plain.yaml")"
  check "$app: the backup Secret reads the app's own path" "true" "$(secret "$work/plain.yaml" | jq --arg a "/$app" '.path | endswith($a)')"
  check "$app: the backup Secret has the four keys the plugin reads" '["ACCESS_KEY_ID","ACCESS_SECRET_KEY","S3_REGION","ca.crt"]' \
    "$(secret "$work/plain.yaml" | jq -c '.destination.transformation.templates | keys | sort')"

  jq -c --arg a "$app" 'select(.app == $a) | .storageValues' "$work/elements.jsonl" >"$work/wiring.json"
  if [ ! -s "$work/wiring.json" ] || ! helm template "$app" "$ROOT/argo-cd/apps/$app" "${args[@]}" -f "$work/wiring.json" >"$work/wired.yaml" 2>"$work/err"; then
    echo "FAIL $app: the render with its claim fails: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; continue
  fi
  store=$(yq -o=json -I=0 'select(.kind == "ObjectStore")' "$work/wired.yaml")
  check "$app: an ObjectStore at the claim's bucket, with the store's CA and gzip" \
    "$(jq -cnS --arg a "$app" --arg s "$STORE" '{name: "\($a)-pg-cluster-backups", endpointURL: $s, destinationPath: "s3://ohmlab-\($a)-pg/", endpointCA: {name: "\($a)-pg-cluster-backup", key: "ca.crt"}, wal: "gzip", data: "gzip"}')" \
    "$(jq -cS '{name: .metadata.name, endpointURL: .spec.configuration.endpointURL, destinationPath: .spec.configuration.destinationPath, endpointCA: .spec.configuration.endpointCA, wal: .spec.configuration.wal.compression, data: .spec.configuration.data.compression}' <<<"$store")"
  check "$app: the ObjectStore syncs before the Cluster and is no Helm hook" '"-1" null' \
    "$(jq -c '.metadata.annotations["argocd.argoproj.io/sync-wave"]' <<<"$store") $(jq -c '.metadata.annotations["helm.sh/hook"]' <<<"$store")"
  check "$app: a daily ScheduledBackup at 20:$(printf '%02d' "$minute") UTC" "0 $minute 20 * * *" \
    "$(yq 'select(.kind == "ScheduledBackup") | .spec.schedule' "$work/wired.yaml")"
  # A standby's backup ends in a WAL segment that a quiet primary may not switch
  # for hours; on the primary, the backup switches it itself.
  check "$app: the base backups run on the primary" "primary" \
    "$(yq 'select(.kind == "Cluster" and .apiVersion == "postgresql.cnpg.io/v1") | .spec.backup.target' "$work/wired.yaml")"
  check "$app: the backup Secret reads the claim's record" "true" "$(secret "$work/wired.yaml" | jq --arg a "/$app/s3" '.path | endswith($a)')"
  # The backup alerts read the series of the cluster's PodMonitor.
  mon=$(yq ea -o=json -I=0 'select(.kind == "PodMonitor" or .kind == "PrometheusRule")' "$work/wired.yaml" \
    | jq -sc --arg p "$app-pg-cluster-" '[.[] | select(.metadata.name | startswith($p))]')
  check "$app: with its claim, the cluster's PodMonitor and alerts, without the HA and zone-spread warnings" \
    "[\"PodMonitor/$app-pg-cluster-cluster-podmonitor\",\"PrometheusRule/$app-pg-cluster-alert-rules\"] 0" \
    "$(jq -c '[.[] | .kind + "/" + .metadata.name] | sort' <<<"$mon") $(jq '[.[] | select(.kind == "PrometheusRule") | .spec.groups[].rules[] | select(.alert == "CNPGClusterHAWarning" or .alert == "CNPGClusterZoneSpreadWarning")] | length' <<<"$mon")"
done

# The Postgres restore runbook of docs/10, as written, on gitea with its claim:
# the new cluster recovers from the claim's ObjectStore, with no credentials of
# its own, and archives there under a server name of its own.
awk '/^\*\*Postgres\.\*\* A cluster restores/ {on = 1} on && /^```yaml$/ {y = 1; next} y && /^```$/ {exit} y' \
  "$ROOT/docs/10-object-storage.md" | sed 's/<app>/gitea/g' >"$work/runbook.yaml"
vf=$(values_file gitea)
args=()
app_args "$vf"
yq 'del(.cnpg.backups)' "$vf" >"$work/values.yaml"
args[1]="$work/values.yaml"
jq -c 'select(.app == "gitea") | .storageValues' "$work/elements.jsonl" >"$work/wiring.json"
if [ -s "$work/runbook.yaml" ] && helm template gitea "$ROOT/argo-cd/apps/gitea" "${args[@]}" -f "$work/wiring.json" -f "$work/runbook.yaml" >"$work/restore.yaml" 2>"$work/err"; then
  cluster=$(yq -o=json -I=0 'select(.kind == "Cluster")' "$work/restore.yaml")
  check "the restore runbook: the cluster recovers from the claim's ObjectStore, under the server name of the backups" \
    '{"barmanObjectName":"gitea-pg-cluster-backups","serverName":"gitea-pg-cluster","source":"pluginRecoveryCluster"}' \
    "$(jq -cS '{source: .spec.bootstrap.recovery.source, barmanObjectName: .spec.externalClusters[0].plugin.parameters.barmanObjectName, serverName: .spec.externalClusters[0].plugin.parameters.serverName}' <<<"$cluster")"
  check "the restore runbook: the restored cluster archives to the claim's ObjectStore under a new server name" \
    '{"barmanObjectName":"gitea-pg-cluster-backups","isWALArchiver":true,"serverName":"gitea-pg-cluster-2"}' \
    "$(jq -cS '.spec.plugins[] | select(.name == "barman-cloud.cloudnative-pg.io") | {isWALArchiver, barmanObjectName: .parameters.barmanObjectName, serverName: .parameters.serverName}' <<<"$cluster")"
  check "the restore runbook: no ObjectStore or recovery Secret beyond the claim's" '["ObjectStore/gitea-pg-cluster-backups"]' \
    "$(yq ea -o=json -I=0 'select(.kind == "ObjectStore" or (.kind == "Secret" and (.metadata.name | test("recovery"))))' "$work/restore.yaml" | jq -sc '[.[] | .kind + "/" + .metadata.name]')"
else
  echo "FAIL the restore runbook renders for gitea: $(grep -m1 -i error "$work/err" | cut -c1-200)"; fail=1
fi

exit "$fail"
