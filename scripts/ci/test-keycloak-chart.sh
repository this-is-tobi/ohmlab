#!/usr/bin/env bash
# Tests of the keycloak app's TLS whose failures only a running cluster would
# otherwise show: with TLS on, Keycloak's certificate names its Service and
# comes from the issuer the values name, the gateway verifies the https port
# only, keycloak-config-cli connects over https with the certificate's
# truststore, and half-set values fail the render.
#
#   test-keycloak-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/keycloak"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/keycloak.yaml" || { echo "no manager parameters for keycloak" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: the chart with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template keycloak "$CHART" --namespace keycloak-system "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template keycloak "$CHART" --namespace keycloak-system "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$label" "" ""
  else
    check "$label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

objs='[select((.kind == "Certificate" or .kind == "BackendTLSPolicy") and (.metadata.name | test("^keycloak"))) | .kind + "/" + .metadata.name] | sort | join(" ")'
cert='select(.kind == "Certificate" and .metadata.name == "keycloak-tls")'
policy='select(.kind == "BackendTLSPolicy" and .metadata.name == "keycloak")'
conn='select(.kind == "ConfigMap" and .metadata.name == "keycloak-config-cli-tls") | .data'
job='select(.kind == "Job" and .metadata.name == "keycloak-config-cli") | .spec.template.spec'
kcc="$job | .containers[0]"
sts='select(.kind == "StatefulSet" and .metadata.name == "keycloak") | .spec.template.spec'
monitor='select(.kind == "ServiceMonitor" and .metadata.name == "keycloak") | .spec.endpoints[0]'
names="keycloak,keycloak.keycloak-system,keycloak.keycloak-system.svc,keycloak.keycloak-system.svc.cluster.local"
tls=(--set keycloak.tls.enabled=true --set keycloak.keycloak.httpsEnabled=true --set keycloakConfigCli.env.KEYCLOAK_URL=https://keycloak:8443)
# The example leaves the gateway off.
gw=(--set gateway.enabled=true)
# ports <service>: the named ports of a Service.
ports() { q "select(.kind == \"Service\" and .metadata.name == \"$1\") | [.spec.ports[] | .name + \"/\" + (.port | tostring)] | join(\" \")"; }
# from <variable>: where keycloak-config-cli takes a variable from, once per definition.
from() { q "[$kcc | .env[] | select(.name == \"$1\") | .valueFrom.configMapKeyRef.name + \"/\" + .valueFrom.configMapKeyRef.key] | join(\" \")"; }
# url: the URL keycloak-config-cli reaches Keycloak at, once per definition.
url() { q "[$kcc | .env[] | select(.name == \"KEYCLOAK_URL\") | .value] | join(\" \")"; }

render
check "by default Keycloak has no certificate and the gateway no policy" "" "$(q "$objs")"
check "by default the Service serves http only" "http/8080" "$(ports keycloak)"
check "keycloak-config-cli reaches Keycloak over http by default" "http://keycloak:8080" "$(url)"
check "and loads no truststore" "" "$(q "$conn | keys | join(\" \")")"
check "metrics are scraped over http by default" "http" "$(q "$monitor | .scheme")"

render "${tls[@]}" "${gw[@]}"
check "with TLS Keycloak has a certificate and the gateway a policy" "BackendTLSPolicy/keycloak Certificate/keycloak-tls" "$(q "$objs")"
check "the internal CA issues it" "ClusterIssuer/ohmlab-internal-ca" "$(q "$cert | .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
check "it names the Service" "$names" "$(q "$cert | .spec.dnsNames | join(\",\")")"
check "it lasts 90 days, renews 15 days ahead, ECDSA in PKCS#8, serves only" \
  "2160h 360h ECDSA 256 PKCS8 digital signature,server auth" \
  "$(q "$cert | .spec.duration + \" \" + .spec.renewBefore + \" \" + .spec.privateKey.algorithm + \" \" + (.spec.privateKey.size | tostring) + \" \" + .spec.privateKey.encoding + \" \" + (.spec.usages | join(\",\"))")"
check "it writes Secret keycloak-tls with a PKCS#12 truststore" "keycloak-tls true Modern2023" \
  "$(q "$cert | .spec.secretName + \" \" + (.spec.keystores.pkcs12.create | tostring) + \" \" + .spec.keystores.pkcs12.profile")"
check "Keycloak serves that Secret" "keycloak-tls" "$(q "$sts | .volumes[] | select(.name == \"tls-certs\") | .secret.secretName")"
check "the probes go over HTTPS" "HTTPS HTTPS HTTPS" \
  "$(q "$sts | .containers[] | select(.name == \"keycloak\") | [.livenessProbe.httpGet.scheme, .readinessProbe.httpGet.scheme, .startupProbe.httpGet.scheme] | join(\" \")")"
check "the Service serves https beside http" "http/8080 https/8443" "$(ports keycloak)"
check "keycloak-config-cli reaches Keycloak over https" "https://keycloak:8443" "$(url)"
check "with the truststore it mounts from the certificate's Secret" \
  "/etc/keycloak-tls/truststore.p12 keycloak-tls/truststore.p12 /etc/keycloak-tls" \
  "$(q "$conn | .KEYCLOAK_TLS_TRUSTSTOREPATH") $(q "$job | .volumes[] | select(.name == \"keycloak-tls\") | .secret.secretName + \"/\" + .secret.items[0].key") $(q "$kcc | .volumeMounts[] | select(.name == \"keycloak-tls\") | .mountPath")"
check "keycloak-config-cli takes the truststore settings from the ConfigMap" \
  "keycloak-config-cli-tls/KEYCLOAK_TLS_TRUSTSTOREPATH keycloak-config-cli-tls/KEYCLOAK_TLS_TRUSTSTOREPASSWORD" \
  "$(from KEYCLOAK_TLS_TRUSTSTOREPATH) $(from KEYCLOAK_TLS_TRUSTSTOREPASSWORD)"
check "and opens the truststore with the certificate's password" "changeit changeit" \
  "$(q "$cert | .spec.keystores.pkcs12.password") $(q "$conn | .KEYCLOAK_TLS_TRUSTSTOREPASSWORD")"
check "the gateway verifies the https port only" "Service/keycloak/https" \
  "$(q "$policy | .spec.targetRefs[] | .kind + \"/\" + .name + \"/\" + .sectionName")"
check "against the internal CA's bundle, under a name the certificate holds" \
  "ConfigMap/ohmlab-internal-ca keycloak.keycloak-system.svc" \
  "$(q "$policy | .spec.validation | .caCertificateRefs[0].kind + \"/\" + .caCertificateRefs[0].name + \" \" + .hostname")"
check "metrics are scraped over https without verifying" "https true" \
  "$(q "$monitor | .scheme + \" \" + (.tlsConfig.insecureSkipVerify | tostring)")"

render "${tls[@]}" --set keycloak.keycloak.httpEnabled=false
check "with HTTP closed the Services serve https only" "https/8443 https/8443" "$(ports keycloak) $(ports keycloak-headless)"

render "${tls[@]}" "${gw[@]}" --set certificate.issuerRef.name=other-ca --set certificate.caConfigMap=other-ca
check "an issuer and a CA ConfigMap replace the internal CA's" "ClusterIssuer/other-ca ConfigMap/other-ca" \
  "$(q "$cert | .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name") $(q "$policy | .spec.validation.caCertificateRefs[0] | .kind + \"/\" + .name")"

render "${tls[@]}"
check "without the gateway, no policy" "Certificate/keycloak-tls" "$(q "$objs")"

fails "TLS without the HTTPS port fails" "go together" --set keycloak.tls.enabled=true
fails "the HTTPS port without TLS fails" "go together" --set keycloak.keycloak.httpsEnabled=true
fails "HTTP closed without TLS fails" "no listener" --set keycloak.keycloak.httpEnabled=false
fails "TLS with keycloak-config-cli on http fails" "set keycloakConfigCli.env.KEYCLOAK_URL to https://keycloak:8443" \
  --set keycloak.tls.enabled=true --set keycloak.keycloak.httpsEnabled=true
# With the issuer the chart itself requires, which it checks first.
fails "the chart's own certificate beside the app's fails" "keycloak.tls.certManager" "${tls[@]}" \
  --set keycloak.tls.certManager.enabled=true --set keycloak.tls.certManager.issuerRef.name=other-ca

exit "$failures"
