#!/usr/bin/env bash
# Tests of the rustfs app: its TLS, where one certificate serves the S3 API and
# the console and the gateway verifies both once a route reaches the Service,
# the environment RustFS reports, and a store's wait for its wiring.
#
#   test-rustfs-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/rustfs"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/rustfs.yaml" || { echo "no manager parameters for rustfs" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: the chart with the example instance's values, whose
# routes reach both ports, into $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template "${RELEASE:-rustfs}" "$CHART" --namespace rustfs "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template rustfs "$CHART" --namespace rustfs "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$label" "" ""
  else
    check "$label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

# routes <service> <port>...: a values file routing <service> on each port.
routes() {
  local svc="$1" port i=0
  shift
  printf 'gateway:\n  routes:\n' >"$work/routes.yaml"
  for port in "$@"; do
    printf '  - name: r%s\n    rules:\n    - {backendService: %s, backendPort: %s}\n' "$i" "$svc" "$port" >>"$work/routes.yaml"
    i=$((i + 1))
  done
}

tls=(--set rustfs.tls.enabled=true)
cert='select(.kind == "Certificate" and .metadata.name == "rustfs-tls")'
policy='select(.kind == "BackendTLSPolicy")'
waves='.metadata.annotations["argocd.argoproj.io/sync-wave"] + " " + .metadata.annotations["argocd.argoproj.io/sync-options"]'

render
check "by default, no Certificate and no BackendTLSPolicy" "0 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
check "by default RustFS serves plain" "0" "$(q '[select(.kind == "ConfigMap") | .data.RUSTFS_TLS_PATH | select(. != null)] | length')"

render "${tls[@]}"
check "the Certificate's fields" "rustfs-tls 2160h 360h ECDSA/256 digital signature,server auth ClusterIssuer/ohmlab-internal-ca" \
  "$(q "$cert | .spec.secretName + \" \" + .spec.duration + \" \" + .spec.renewBefore + \" \" + .spec.privateKey.algorithm + \"/\" + (.spec.privateKey.size | tostring) + \" \" + (.spec.usages | join(\",\")) + \" \" + .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
check "the Certificate names the Service" "rustfs-svc,rustfs-svc.rustfs,rustfs-svc.rustfs.svc,rustfs-svc.rustfs.svc.cluster.local" \
  "$(q "$cert | .spec.dnsNames | join(\",\")")"
# One targetRef for every port: Traefik lists a policy once per targetRef naming
# the Service, so a second one on the same Service conflicts with the first.
check "the policy's fields with routes to both ports: one targetRef, every port" "rustfs-svc 1 Service/rustfs-svc/none ConfigMap/ohmlab-internal-ca rustfs-svc.rustfs.svc" \
  "$(q "$policy | .metadata.name + \" \" + (.spec.targetRefs | length | tostring) + \" \" + .spec.targetRefs[0].kind + \"/\" + .spec.targetRefs[0].name + \"/\" + (.spec.targetRefs[0].sectionName // \"none\") + \" \" + .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name + \" \" + .spec.validation.hostname")"
check "both kinds sync in their waves, without a dry run on a fresh cluster" \
  "-1 SkipDryRunOnMissingResource=true|1 SkipDryRunOnMissingResource=true" "$(q "$cert | $waves")|$(q "$policy | $waves")"
check "the chart serves both listeners from the certificate" "/opt/tls HTTPS" \
  "$(q 'select(.kind == "ConfigMap") | .data.RUSTFS_TLS_PATH | select(. != null)') $(q 'select(.kind == "Deployment") | .spec.template.spec.containers[0].readinessProbe.httpGet.scheme')"

render "${tls[@]}" --set certificate.caConfigMap=
check "with certificate.caConfigMap empty, the policy reads the CA of rustfs-tls" "Secret/rustfs-tls" \
  "$(q "$policy | .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name")"
render "${tls[@]}" --set gateway.enabled=false
check "no policy with the gateway off" "1 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
routes rustfs-svc 9001
render "${tls[@]}" -f "$work/routes.yaml"
check "one route is enough for the policy" "1" "$(q "[$policy] | length")"
routes other-svc 9000 9001
render "${tls[@]}" -f "$work/routes.yaml"
check "no policy with the routes on another Service" "0" "$(q "[$policy] | length")"
routes check-rustfs-svc 9000
RELEASE=check render "${tls[@]}" -f "$work/routes.yaml"
check "the names follow another release name" "check-rustfs-svc check-rustfs-svc check-rustfs-svc rustfs-tls" \
  "$(q 'select(.kind == "Service" and (.metadata.name | test("-svc$"))) | .metadata.name') $(q "$cert | .spec.dnsNames[0]") $(q "$policy | .spec.targetRefs[0].name") $(q "$cert | .spec.secretName")"

# RustFSMetricsAbsent selects a RustFS by its environment, which is its
# namespace; its stdout logs stay on whatever that environment (RustFS turns
# them off in one named production).
render
env='select(.kind == "Deployment") | .spec.template.spec.containers[0].env[]'
check "the pod reports its namespace as RustFS's environment, with stdout logs on" "metadata.namespace true" \
  "$(q "$env | select(.name == \"RUSTFS_OBS_ENVIRONMENT\") | .valueFrom.fieldRef.fieldPath") $(q "$env | select(.name == \"RUSTFS_OBS_LOG_STDOUT_ENABLED\") | .value")"

fails "TLS with the distributed mode fails" "rustfs.mode.distributed" "${tls[@]}" \
  --set rustfs.mode.standalone.enabled=false --set rustfs.mode.distributed.enabled=true
fails "TLS without existingSecret fails" "existingSecret" "${tls[@]}" --set rustfs.tls.existingSecret=

# A store of instance.yaml's storage block: its Application can sync once
# before its ApplicationSet carries the instance-manager's wiring.
fails "a store does not render before its wiring" "waiting for the instance-manager's wiring" --set store.enabled=true
render --set store.enabled=true --set store.wired=true
check "a wired store renders" "1" "$(q '[select(.kind == "Deployment")] | length')"

# A store's cloud replica: an hourly rclone job, its secrets, its egress and its alert.
REPLICA=(--set store.enabled=true --set store.wired=true --set replica.enabled=true --set networkPolicy.enabled=true
  --set replica.endpoint=https://s3.fr-par.scw.cloud --set replica.region=fr-par --set replica.bucket=cloud-replica
  --set replica.provider=Scaleway --set replica.prefix=homelab --set replica.store=rustfs-backups
  --set replica.vault.mount=ohmlab --set replica.vault.record=platforms/production/core/rustfs-backups/s3
  --set replica.vault.target=platforms/production/core/rustfs-backups/replica
  --set replica.vault.crypt=platforms/production/core/rustfs-backups/replica-crypt)
objs='select(.metadata.name | test("-replica"))'
render --set store.enabled=true --set store.wired=true
check "without a replica, no replica object and no replica alert" "0 0" \
  "$(q "[$objs] | length") $(q '[select(.kind == "PrometheusRule") | .spec.groups[].rules[] | select(.alert == "OhmlabReplicaStale")] | length')"
render "${REPLICA[@]}"
check "a replica renders its objects" \
  "ConfigMap/rustfs-replica CronJob/rustfs-replica NetworkPolicy/rustfs-replica-egress VaultStaticSecret/rustfs-replica-crypt VaultStaticSecret/rustfs-replica-source VaultStaticSecret/rustfs-replica-target" \
  "$(q "[$objs | .kind + \"/\" + .metadata.name] | sort | join(\" \")")"
check "a replica renders its alert" "OhmlabReplicaStale" \
  "$(q 'select(.kind == "PrometheusRule") | .spec.groups[].rules[] | select(.alert == "OhmlabReplicaStale") | .alert')"
cj='select(.kind == "CronJob")'
pod="$cj | .spec.jobTemplate.spec.template.spec"
check "the schedule skips 00:40-04:40 UTC, one run at a time, bounded" "40 5-23 * * * Etc/UTC Forbid 600 3000 0" \
  "$(q "$cj | .spec.schedule + \" \" + .spec.timeZone + \" \" + .spec.concurrencyPolicy + \" \" + (.spec.startingDeadlineSeconds | tostring) + \" \" + (.spec.jobTemplate.spec.activeDeadlineSeconds | tostring) + \" \" + (.spec.jobTemplate.spec.backoffLimit | tostring)")"
check "the job runs unprivileged" "65534 true RuntimeDefault false true false ALL" \
  "$(q "$pod | (.securityContext.runAsUser | tostring) + \" \" + (.securityContext.runAsNonRoot | tostring) + \" \" + .securityContext.seccompProfile.type + \" \" + (.automountServiceAccountToken | tostring) + \" \" + (.containers[0].securityContext.readOnlyRootFilesystem | tostring) + \" \" + (.containers[0].securityContext.allowPrivilegeEscalation | tostring) + \" \" + (.containers[0].securityContext.capabilities.drop | join(\",\"))")"
check "the image is pinned by digest" "yes" "$(q "$pod | .containers[0].image" | grep -qE '^docker\.io/rclone/rclone:[0-9.]+@sha256:[a-f0-9]{64}$' && echo yes || echo no)"
envs="$pod | .containers[0].env[]"
check "the job's secrets come from the three Secrets" \
  "rustfs-replica-crypt/PASSWORD rustfs-replica-crypt/PASSWORD2 rustfs-replica-source/ACCESS_KEY rustfs-replica-source/BUCKETS rustfs-replica-source/ENDPOINT rustfs-replica-source/REGION rustfs-replica-source/SECRET_KEY rustfs-replica-target/ACCESS_KEY rustfs-replica-target/SECRET_KEY" \
  "$(q "[$envs | select(.valueFrom.secretKeyRef) | .valueFrom.secretKeyRef.name + \"/\" + .valueFrom.secretKeyRef.key] | sort | join(\" \")")"
check "the job mounts its script, the store's CA and a writable /tmp" "/etc/ohmlab/store-ca /scripts /tmp" \
  "$(q "[$pod | .containers[0].volumeMounts[].mountPath] | sort | join(\" \")")"
check "the job is not a dry run and refuses a shrink by default" "false false" "$(q "$envs | select(.name == \"DRY_RUN\") | .value") $(q "$envs | select(.name == \"ALLOW_SHRINK\") | .value")"
cm='select(.kind == "ConfigMap" and .metadata.name == "rustfs-replica")'
check "the ConfigMap holds the settings the bundle reads, and its label" \
  "rustfs-backups Scaleway https://s3.fr-par.scw.cloud fr-par cloud-replica homelab standard true rustfs-replica-target rustfs-replica-crypt true" \
  "$(q "$cm | .data.store + \" \" + .data.provider + \" \" + .data.endpoint + \" \" + .data.region + \" \" + .data.bucket + \" \" + .data.prefix + \" \" + .data.filenameEncryption + \" \" + .data.directoryNameEncryption + \" \" + .data.targetSecret + \" \" + .data.cryptSecret + \" \" + .metadata.labels[\"ohmlab.fr/replica\"]")"
check "the ConfigMap holds the script" "yes" "$(q "$cm | .data[\"replicate.sh\"]" | grep -q 'refusing to delete more than half of the replica' && echo yes || echo no)"
vss='select(.kind == "VaultStaticSecret" and .metadata.name == "rustfs-replica-source") | .spec'
check "the source secret reads the store's record with the store's auth" "ohmlab platforms/production/core/rustfs-backups/s3 rustfs-vso-default rustfs-replica-source" \
  "$(q "$vss | .mount + \" \" + .path + \" \" + .vaultAuthRef + \" \" + .destination.name")"
check "the egress policy selects the job only, on 443" "replica 443" \
  "$(q 'select(.kind == "NetworkPolicy" and .metadata.name == "rustfs-replica-egress") | .spec.podSelector.matchLabels["app.kubernetes.io/component"] + " " + (.spec.egress[0].ports[0].port | tostring)')"
render "${REPLICA[@]}" --set replica.dryRun=true --set 'replica.extraArgs={--size-only}'
check "dryRun and extraArgs reach the job" "true --size-only" \
  "$(q "$envs | select(.name == \"DRY_RUN\") | .value") $(q "$envs | select(.name == \"EXTRA_ARGS\") | .value")"
render "${REPLICA[@]}" --set networkPolicy.enabled=false
check "no egress policy where the namespace does not restrict egress" "0" \
  "$(q '[select(.kind == "NetworkPolicy" and .metadata.name == "rustfs-replica-egress")] | length')"
for key in endpoint region bucket prefix store vault.mount vault.record vault.target vault.crypt; do
  fails "a replica without $key fails" "replica.$key" "${REPLICA[@]}" --set "replica.$key="
done
fails "a replica needs a store" "replica.enabled needs store.enabled" "${REPLICA[@]}" --set store.enabled=false

exit "$failures"
