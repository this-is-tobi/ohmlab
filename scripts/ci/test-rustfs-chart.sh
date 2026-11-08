#!/usr/bin/env bash
# Tests of the rustfs app's TLS: one certificate serves the S3 API and the
# console, and the gateway verifies both once a route reaches the Service.
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
check "the policy's fields with routes to both ports" "rustfs-svc Service/rustfs-svc endpoint,console ConfigMap/ohmlab-internal-ca rustfs-svc.rustfs.svc" \
  "$(q "$policy | .metadata.name + \" \" + .spec.targetRefs[0].kind + \"/\" + .spec.targetRefs[0].name + \" \" + ([.spec.targetRefs[].sectionName] | join(\",\")) + \" \" + .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name + \" \" + .spec.validation.hostname")"
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
check "one route is enough for both targetRefs" "endpoint,console" "$(q "$policy | [.spec.targetRefs[].sectionName] | join(\",\")")"
routes other-svc 9000 9001
render "${tls[@]}" -f "$work/routes.yaml"
check "no policy with the routes on another Service" "0" "$(q "[$policy] | length")"
routes check-rustfs-svc 9000
RELEASE=check render "${tls[@]}" -f "$work/routes.yaml"
check "the names follow another release name" "check-rustfs-svc check-rustfs-svc check-rustfs-svc rustfs-tls" \
  "$(q 'select(.kind == "Service" and (.metadata.name | test("-svc$"))) | .metadata.name') $(q "$cert | .spec.dnsNames[0]") $(q "$policy | .spec.targetRefs[0].name") $(q "$cert | .spec.secretName")"

fails "TLS with the distributed mode fails" "rustfs.mode.distributed" "${tls[@]}" \
  --set rustfs.mode.standalone.enabled=false --set rustfs.mode.distributed.enabled=true
fails "TLS without existingSecret fails" "existingSecret" "${tls[@]}" --set rustfs.tls.existingSecret=

exit "$failures"
