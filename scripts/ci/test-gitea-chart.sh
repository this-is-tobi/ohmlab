#!/usr/bin/env bash
# Tests of the gitea chart's Valkey TLS, whose failures only a running cluster
# would otherwise show: Valkey reloads a renewed certificate.
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
  if ! helm template gitea "$CHART" --namespace gitea "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
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

exit "$failures"
