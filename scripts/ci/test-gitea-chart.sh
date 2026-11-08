#!/usr/bin/env bash
# Tests of the gitea app whose failures only a running cluster would otherwise
# show: Valkey reloads a renewed certificate and takes it from the issuer the
# values name, Gitea verifies it, and the catalog's Keycloak client allows the
# callback Gitea signs in with.
#
#   test-gitea-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/gitea"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/gitea.yaml" || { echo "no manager parameters for gitea" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: the chart with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template "${RELEASE:-gitea}" "$CHART" --namespace gitea "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template gitea "$CHART" --namespace gitea "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$label" "" ""
  else
    check "$label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

render
check "Valkey reloads a renewed certificate" "1" \
  "$(q '[select(.kind == "ConfigMap" and .metadata.name == "gitea-valkey") | .data["valkey.conf"] | split("\n") | .[] | select(. == "tls-auto-reload-interval 10")] | length')"
check "a change of Valkey's config rolls its pod" "tls-auto-reload" \
  "$(q 'select(.kind == "StatefulSet" and .metadata.name == "gitea-valkey") | .spec.template.metadata.annotations["ohmlab.fr/config-revision"]')"

issuer='select(.kind == "Certificate" and .metadata.name == "gitea-valkey-tls") | .spec.issuerRef.kind + "/" + .spec.issuerRef.name'
pki='[select(.apiVersion == "cert-manager.io/v1") | .kind + "/" + .metadata.name] | sort | join(" ")'
names='select(.kind == "Certificate" and .metadata.name == "gitea-valkey-tls") | (.spec.dnsNames + .spec.ipAddresses + .spec.usages) | join(",")'
check "by default a CA of the namespace issues Valkey's certificate" "Issuer/gitea-valkey-ca" "$(q "$issuer")"
check "by default the namespace CA renders" \
  "Certificate/gitea-valkey-ca Certificate/gitea-valkey-tls Issuer/gitea-valkey-ca Issuer/gitea-valkey-selfsigned" "$(q "$pki")"
check "the namespace CA keeps its key when it renews" "Never" \
  "$(q 'select(.kind == "Certificate" and .metadata.name == "gitea-valkey-ca") | .spec.privateKey.rotationPolicy')"
default_names=$(q "$names")
render --set valkeyCertificate.issuerRef.kind=ClusterIssuer --set valkeyCertificate.issuerRef.name=ohmlab-internal-ca
check "an issuerRef issues Valkey's certificate" "ClusterIssuer/ohmlab-internal-ca" "$(q "$issuer")"
check "with an issuerRef no namespace CA renders" "Certificate/gitea-valkey-tls" "$(q "$pki")"
check "the certificate keeps its names and usages" "$default_names" "$(q "$names")"

vss='select(.kind == "VaultStaticSecret" and .metadata.name == "gitea-vso-gitea-valkey-auth")'
spec='select(.kind == "Deployment" and .metadata.name == "gitea") | .spec.template.spec'
main="$spec | .containers[] | select(.name == \"gitea\")"
render
check "Gitea's Valkey URIs verify the server" "0" \
  "$(q "[$vss | .spec.destination.transformation.templates[].text | select(test(\"skipverify\"))] | length")"
check "they stay on TLS" "3" \
  "$(q "[$vss | .spec.destination.transformation.templates[].text | select(test(\"^rediss://\"))] | length")"
check "Gitea adds the CA's directory to the system's" "/etc/ssl/certs:/etc/valkey-ca" \
  "$(q "$main | .env[] | select(.name == \"SSL_CERT_DIR\") | .value")"
check "Gitea mounts that CA read-only" "/etc/valkey-ca true" \
  "$(q "$main | .volumeMounts[] | select(.name == \"valkey-ca\") | .mountPath + \" \" + (.readOnly | tostring)")"
check "only ca.crt of Valkey's Secret is projected" "gitea-valkey-tls ca.crt=ca.crt" \
  "$(q "$spec | .volumes[] | select(.name == \"valkey-ca\") | .secret.secretName + \" \" + ([.secret.items[] | .key + \"=\" + .path] | join(\",\"))")"
check "a change of the URIs or the password restarts Valkey and Gitea" "StatefulSet/gitea-valkey Deployment/gitea" \
  "$(q "[$vss | .spec.rolloutRestartTargets[] | .kind + \"/\" + .name] | join(\" \")")"

# Gitea over TLS: one listener, so PROTOCOL switches it, with the scrape.
tls=(--set gitea.gitea.config.server.PROTOCOL=https --set gitea.gitea.metrics.serviceMonitor.scheme=https)
gw=(--set gateway.enabled=true --set 'gateway.routes[0].name=gitea'
  --set 'gateway.routes[0].rules[0].backendService=gitea-http' --set 'gateway.routes[0].rules[0].backendPort=3000')
cert='select(.kind == "Certificate" and .metadata.name == "gitea-tls")'
policy='select(.kind == "BackendTLSPolicy")'
protocol='select(.kind == "Secret" and .metadata.name == "gitea-inline-config") | .stringData.server | split("\n") | .[] | select(test("^PROTOCOL="))'
waves='.metadata.annotations["argocd.argoproj.io/sync-wave"] + " " + .metadata.annotations["argocd.argoproj.io/sync-options"]'

render
check "by default, no Certificate and no BackendTLSPolicy for Gitea" "0 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
check "by default Gitea serves http" "PROTOCOL=http" "$(q "$protocol")"

render "${tls[@]}" "${gw[@]}"
check "the switch turns app.ini to https" "PROTOCOL=https" "$(q "$protocol")"
check "the Certificate's fields" "gitea-tls 2160h 360h ECDSA/256 digital signature,server auth ClusterIssuer/ohmlab-internal-ca" \
  "$(q "$cert | .spec.secretName + \" \" + .spec.duration + \" \" + .spec.renewBefore + \" \" + .spec.privateKey.algorithm + \"/\" + (.spec.privateKey.size | tostring) + \" \" + (.spec.usages | join(\",\")) + \" \" + .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
check "the Certificate names the Service" "gitea-http,gitea-http.gitea,gitea-http.gitea.svc,gitea-http.gitea.svc.cluster.local" \
  "$(q "$cert | .spec.dnsNames | join(\",\")")"
check "the policy's fields with a route to gitea-http:3000" "gitea-http Service/gitea-http/http ConfigMap/ohmlab-internal-ca gitea-http.gitea.svc" \
  "$(q "$policy | .metadata.name + \" \" + .spec.targetRefs[0].kind + \"/\" + .spec.targetRefs[0].name + \"/\" + .spec.targetRefs[0].sectionName + \" \" + .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name + \" \" + .spec.validation.hostname")"
check "both kinds sync in their waves, without a dry run on a fresh cluster" \
  "-1 SkipDryRunOnMissingResource=true|1 SkipDryRunOnMissingResource=true" "$(q "$cert | $waves")|$(q "$policy | $waves")"
check "the probes stay tcpSocket" "http http" \
  "$(q "$main | .livenessProbe.tcpSocket.port + \" \" + .readinessProbe.tcpSocket.port")"
check "the scrape is https without verification" "https true" \
  "$(q 'select(.kind == "ServiceMonitor" and .metadata.name == "gitea") | .spec.endpoints[0].scheme + " " + (.spec.endpoints[0].tlsConfig.insecureSkipVerify | tostring)')"
check "the certificate reaches Gitea's container only" "/etc/gitea-tls 0" \
  "$(q "$main | .volumeMounts[] | select(.name == \"gitea-tls\") | .mountPath") $(q "[$spec | .initContainers[] | .volumeMounts[]? | select(.name == \"gitea-tls\")] | length")"
check "the volume is the certificate's Secret, optional while it is not issued" "gitea-tls true" \
  "$(q "$spec | .volumes[] | select(.name == \"gitea-tls\") | .secret.secretName + \" \" + (.secret.optional | tostring)")"

render "${tls[@]}" "${gw[@]}" --set certificate.caConfigMap=
check "with certificate.caConfigMap empty, the policy reads the CA of gitea-tls" "Secret/gitea-tls" \
  "$(q "$policy | .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name")"
render "${tls[@]}"
check "no policy with the gateway off" "1 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
render "${tls[@]}" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendPort=2222'
check "no policy with the route on another port" "0" "$(q "[$policy] | length")"
render "${tls[@]}" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendService=gitea-ssh'
check "no policy with the route on another Service" "0" "$(q "[$policy] | length")"
RELEASE=check render "${tls[@]}" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendService=check-gitea-http'
check "the names follow another release name" "check-gitea-http check-gitea-http gitea-tls" \
  "$(q 'select(.kind == "Service" and (.metadata.name | test("-http$"))) | .metadata.name') $(q "$policy | .spec.targetRefs[0].name") $(q "$cert | .spec.secretName")"

fails "PROTOCOL HTTPS fails, Gitea takes lowercase only" "gitea.gitea.config.server.PROTOCOL" \
  --set gitea.gitea.config.server.PROTOCOL=HTTPS
fails "https without the https scrape fails" "gitea.gitea.metrics.serviceMonitor.scheme" \
  --set gitea.gitea.config.server.PROTOCOL=https
fails "the https scrape without https fails" "gitea.gitea.metrics.serviceMonitor.scheme" \
  --set gitea.gitea.metrics.serviceMonitor.scheme=https
printf 'gitea:\n  extraVolumes:\n  - name: other\n    emptyDir: {}\n' >"$work/volumes.yaml"
fails "an instance's extraVolumes without gitea-tls fails with TLS" "gitea.extraVolumes" "${tls[@]}" -f "$work/volumes.yaml"
printf 'gitea:\n  extraContainerVolumeMounts:\n  - name: valkey-ca\n    mountPath: /etc/valkey-ca\n' >"$work/mounts.yaml"
fails "an instance's mounts without the certificate's directory fail with TLS" "gitea.extraContainerVolumeMounts" "${tls[@]}" -f "$work/mounts.yaml"

# Gitea sends <ROOT_URL>/user/oauth2/<its OAuth source's name>/callback, and
# Keycloak matches redirect URIs case-sensitively.
callback="https://git.\$(env:ROOT_DOMAIN)/user/oauth2/$(yq -r '.gitea.gitea.oauth[0].name' "$CHART/values.yaml")/callback"
check "the Keycloak client allows Gitea's sign-in callback" "1" \
  "$(want="$callback" yq '[.realms.instance.clients[] | select(.clientId == "gitea") | .redirectUris[] | select(. == strenv(want))] | length' "$ROOT/argo-cd/apps/keycloak/values.yaml")"

exit "$failures"
