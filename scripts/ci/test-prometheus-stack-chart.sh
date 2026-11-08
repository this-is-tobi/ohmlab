#!/usr/bin/env bash
# Tests of the prometheus-stack chart: Grafana's Postgres cluster comes and goes
# with Grafana, so a cluster that runs Prometheus and Alertmanager alone renders
# no database whose credentials nothing writes.
#
#   test-prometheus-stack-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/prometheus-stack"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/prometheus-stack.yaml" || { echo "no manager parameters for prometheus-stack" >&2; exit 1; }

# clusters [<helm args>...]: the CNPG Clusters the chart renders, by name.
clusters() {
  helm template prometheus-stack "$CHART" --namespace prometheus-stack "${args[@]}" "$@" 2>"$work/err" \
    | yq ea '[select(.kind == "Cluster") | .metadata.name] | join(" ")' - \
    || echo "does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"
}

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

check "with Grafana, its database renders" "prometheus-stack-pg-cluster" "$(clusters)"
check "without Grafana, no database renders" "" "$(clusters --set kube-prometheus-stack.grafana.enabled=false)"

# render [<helm args>...]: the chart with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template "${RELEASE:-prometheus-stack}" "$CHART" --namespace prometheus-stack "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template prometheus-stack "$CHART" --namespace prometheus-stack "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$label" "" ""
  else
    check "$label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

# Grafana over TLS: its chart has no switch, so an instance sets this recipe.
cat >"$work/tls.yaml" <<'EOF'
kube-prometheus-stack:
  grafana:
    grafana.ini:
      server: {protocol: https, cert_file: /etc/grafana/tls/tls.crt, cert_key: /etc/grafana/tls/tls.key, certs_watch_interval: 1h}
    extraSecretMounts: [{name: tls, mountPath: /etc/grafana/tls, secretName: grafana-tls, readOnly: true}]
    livenessProbe: {httpGet: {scheme: HTTPS}}
    readinessProbe: {httpGet: {scheme: HTTPS}}
    serviceMonitor: {scheme: https, tlsConfig: {insecureSkipVerify: true}}
    sidecar:
      dashboards: {reloadURL: "https://localhost:3000/api/admin/provisioning/dashboards/reload", env: {REQ_SKIP_TLS_VERIFY: "true"}}
      datasources: {reloadURL: "https://localhost:3000/api/admin/provisioning/datasources/reload", env: {REQ_SKIP_TLS_VERIFY: "true"}}
EOF
gw=(--set gateway.enabled=true --set 'gateway.routes[0].name=grafana'
  --set 'gateway.routes[0].rules[0].backendService=prometheus-stack-grafana' --set 'gateway.routes[0].rules[0].backendPort=80')
cert='select(.kind == "Certificate" and .metadata.name == "grafana-tls")'
policy='select(.kind == "BackendTLSPolicy")'
grafana='select(.kind == "Deployment" and .metadata.name == "prometheus-stack-grafana") | .spec.template.spec'
waves='.metadata.annotations["argocd.argoproj.io/sync-wave"] + " " + .metadata.annotations["argocd.argoproj.io/sync-options"]'

render
check "by default, no Certificate and no BackendTLSPolicy for Grafana" "0 0" "$(q "[$cert] | length") $(q "[$policy] | length")"

render -f "$work/tls.yaml" "${gw[@]}"
check "the Certificate's fields" "grafana-tls 2160h 360h ECDSA/256 digital signature,server auth ClusterIssuer/ohmlab-internal-ca" \
  "$(q "$cert | .spec.secretName + \" \" + .spec.duration + \" \" + .spec.renewBefore + \" \" + .spec.privateKey.algorithm + \"/\" + (.spec.privateKey.size | tostring) + \" \" + (.spec.usages | join(\",\")) + \" \" + .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
check "the Certificate names the Service" \
  "prometheus-stack-grafana,prometheus-stack-grafana.prometheus-stack,prometheus-stack-grafana.prometheus-stack.svc,prometheus-stack-grafana.prometheus-stack.svc.cluster.local" \
  "$(q "$cert | .spec.dnsNames | join(\",\")")"
check "the policy's fields with a route to prometheus-stack-grafana:80" \
  "prometheus-stack-grafana Service/prometheus-stack-grafana/http-web ConfigMap/ohmlab-internal-ca prometheus-stack-grafana.prometheus-stack.svc" \
  "$(q "$policy | .metadata.name + \" \" + .spec.targetRefs[0].kind + \"/\" + .spec.targetRefs[0].name + \"/\" + .spec.targetRefs[0].sectionName + \" \" + .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name + \" \" + .spec.validation.hostname")"
check "both kinds sync in their waves, without a dry run on a fresh cluster" \
  "-1 SkipDryRunOnMissingResource=true|1 SkipDryRunOnMissingResource=true" "$(q "$cert | $waves")|$(q "$policy | $waves")"
check "the probes are HTTPS" "HTTPS HTTPS" \
  "$(q "$grafana | .containers[] | select(.name == \"grafana\") | .livenessProbe.httpGet.scheme + \" \" + .readinessProbe.httpGet.scheme")"
check "the sidecars reload over https without verifying localhost" "2 2" \
  "$(q "[$grafana | .containers[] | select((.env // []) | any_c(.name == \"REQ_URL\"))] | length") $(q "[$grafana | .containers[] | select(((.env // []) | any_c(.name == \"REQ_URL\" and (.value | test(\"^https://\")))) and ((.env // []) | any_c(.name == \"REQ_SKIP_TLS_VERIFY\" and .value == \"true\")))] | length")"

render -f "$work/tls.yaml" "${gw[@]}" --set grafanaCertificate.caConfigMap=
check "with grafanaCertificate.caConfigMap empty, the policy reads the CA of grafana-tls" "Secret/grafana-tls" \
  "$(q "$policy | .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name")"
render -f "$work/tls.yaml"
check "no policy with the gateway off" "1 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
render -f "$work/tls.yaml" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendPort=3000'
check "no policy with the route on another port" "0" "$(q "[$policy] | length")"
render -f "$work/tls.yaml" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendService=prometheus-stack-kube-prom-prometheus'
check "no policy with the route on another Service" "0" "$(q "[$policy] | length")"
RELEASE=check render -f "$work/tls.yaml" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendService=check-grafana'
check "the names follow another release name" "check-grafana check-grafana grafana-tls" \
  "$(q 'select(.kind == "Service" and .metadata.labels["app.kubernetes.io/name"] == "grafana") | .metadata.name') $(q "$policy | .spec.targetRefs[0].name") $(q "$cert | .spec.secretName")"
render -f "$work/tls.yaml" --set kube-prometheus-stack.grafana.enabled=false
check "Grafana off with the recipe renders nothing of it" "0 0" "$(q "[$cert] | length") $(q "[$policy] | length")"

fails "https without certs_watch_interval fails" "certs_watch_interval" -f "$work/tls.yaml" \
  --set 'kube-prometheus-stack.grafana.grafana\.ini.server.certs_watch_interval=null'
fails "https without the tls mount fails" "extraSecretMounts" -f "$work/tls.yaml" \
  --set 'kube-prometheus-stack.grafana.extraSecretMounts=null'
fails "https with a probe left HTTP fails" "livenessProbe" -f "$work/tls.yaml" \
  --set kube-prometheus-stack.grafana.livenessProbe.httpGet.scheme=HTTP
fails "https with a sidecar reloading over http fails" "reloadURL" -f "$work/tls.yaml" \
  --set kube-prometheus-stack.grafana.sidecar.dashboards.reloadURL=http://localhost:3000/api/admin/provisioning/dashboards/reload
fails "https with the scrape on http fails" "serviceMonitor.scheme" -f "$work/tls.yaml" \
  --set kube-prometheus-stack.grafana.serviceMonitor.scheme=http
fails "https with a scrape that neither skips nor verifies the certificate fails" "serviceMonitor.tlsConfig" -f "$work/tls.yaml" \
  --set 'kube-prometheus-stack.grafana.serviceMonitor.tlsConfig=null'
fails "HTTPS probes without https fail" "protocol" \
  --set kube-prometheus-stack.grafana.readinessProbe.httpGet.scheme=HTTPS

exit "$failures"
