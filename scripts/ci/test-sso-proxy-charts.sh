#!/usr/bin/env bash
# Tests of the oauth2-proxies' TLS in the cilium (Hubble) and longhorn apps,
# which copy the same templates: sso.httpScheme https serves the proxy over
# TLS only, from a certificate the app issues, and the gateway verifies it
# once a route reaches the https port.
#
#   test-sso-proxy-charts.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: $CHART with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template "${RELEASE:-$app}" "$CHART" --namespace "$ns" "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "$app: the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# fails <label> <message> [<helm args>...]: the chart must not render, and its
# error must name <message>.
fails() {
  local label="$1" message="$2"
  shift 2
  if helm template "$app" "$CHART" --namespace "$ns" "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    check "$app: $label" "an error naming: $message" "it renders"
  elif grep -qF -- "$message" "$work/err"; then
    check "$app: $label" "" ""
  else
    check "$app: $label" "an error naming: $message" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

waves='(.metadata.annotations["argocd.argoproj.io/sync-wave"] // "none") + " " + .metadata.annotations["argocd.argoproj.io/sync-options"]'
tls=(--set sso.httpScheme=https --set sso.service.portNumber=443 --set sso.service.appProtocol=https)

for entry in cilium:kube-system:hubble-oauth2-proxy longhorn:longhorn-system:longhorn-oauth2-proxy; do
  IFS=: read -r app ns svc <<<"$entry"
  CHART="$ROOT/argo-cd/apps/$app"
  if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
    helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
  fi
  args=()
  app_args "$ROOT/argo-cd/instances/_example/values/core/$app.yaml" || { echo "no manager parameters for $app" >&2; exit 1; }
  gw=(--set gateway.enabled=true --set 'lanOnly.sourceRange={192.168.0.0/16}' --set 'gateway.routes[0].name=sso'
    --set "gateway.routes[0].rules[0].backendService=$svc" --set 'gateway.routes[0].rules[0].backendPort=443')
  cert="select(.kind == \"Certificate\" and .metadata.name == \"$svc-tls\")"
  policy='select(.kind == "BackendTLSPolicy")'
  proxy="select(.kind == \"Deployment\" and .metadata.name == \"$svc\") | .spec.template.spec"
  container="$proxy | .containers[] | select(.name == \"sso\")"
  service="select(.kind == \"Service\" and .metadata.name == \"$svc\")"
  tlsargs="[$container | .args[] | select(test(\"^--tls-(cert|key)-file=.+\"))] | length"

  render
  check "$app: by default, no Certificate and no BackendTLSPolicy" "0 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
  check "$app: by default the proxy serves http" "http/80 HTTP HTTP 0" \
    "$(q "$service | .spec.ports[0].name + \"/\" + (.spec.ports[0].port | tostring)") $(q "$container | .livenessProbe.httpGet.scheme + \" \" + .readinessProbe.httpGet.scheme") $(q "$tlsargs")"

  render "${tls[@]}" "${gw[@]}"
  check "$app: the Certificate's fields" "$svc-tls 2160h 360h ECDSA/256 digital signature,server auth ClusterIssuer/ohmlab-internal-ca" \
    "$(q "$cert | .spec.secretName + \" \" + .spec.duration + \" \" + .spec.renewBefore + \" \" + .spec.privateKey.algorithm + \"/\" + (.spec.privateKey.size | tostring) + \" \" + (.spec.usages | join(\",\")) + \" \" + .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
  check "$app: the Certificate names the Service" "$svc,$svc.$ns,$svc.$ns.svc,$svc.$ns.svc.cluster.local" \
    "$(q "$cert | .spec.dnsNames | join(\",\")")"
  check "$app: the policy's fields with a route to $svc:443" "$svc Service/$svc/https ConfigMap/ohmlab-internal-ca $svc.$ns.svc" \
    "$(q "$policy | .metadata.name + \" \" + .spec.targetRefs[0].kind + \"/\" + .spec.targetRefs[0].name + \"/\" + .spec.targetRefs[0].sectionName + \" \" + .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name + \" \" + .spec.validation.hostname")"
  # These apps sync before cert-manager: a wave would hold all of them on it.
  check "$app: the Certificate syncs with the pods, the policy after, neither dry-run on a fresh cluster" \
    "none SkipDryRunOnMissingResource=true|1 SkipDryRunOnMissingResource=true" "$(q "$cert | $waves")|$(q "$policy | $waves")"
  check "$app: with TLS the proxy serves https only" "2 4443 HTTPS HTTPS https/443,metrics/44180 $svc-tls/true" \
    "$(q "$tlsargs") $(q "$container | .ports[] | select(.name == \"https\") | .containerPort") $(q "$container | .livenessProbe.httpGet.scheme + \" \" + .readinessProbe.httpGet.scheme") $(q "$service | [.spec.ports[] | .name + \"/\" + (.port | tostring)] | join(\",\")") $(q "$proxy | .volumes[] | select(.name == \"tls\") | .secret.secretName + \"/\" + (.secret.optional | tostring)")"

  render "${tls[@]}" "${gw[@]}" --set ssoCertificate.caConfigMap=
  check "$app: with ssoCertificate.caConfigMap empty, the policy reads the CA of $svc-tls" "Secret/$svc-tls" \
    "$(q "$policy | .spec.validation.caCertificateRefs[0].kind + \"/\" + .spec.validation.caCertificateRefs[0].name")"
  render "${tls[@]}"
  check "$app: no policy with the gateway off" "1 0" "$(q "[$cert] | length") $(q "[$policy] | length")"
  render "${tls[@]}" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendPort=80'
  check "$app: a route on 80 gives no policy" "0" "$(q "[$policy] | length")"
  render "${tls[@]}" "${gw[@]}" --set 'gateway.routes[0].rules[0].backendService=other'
  check "$app: no policy with the route on another Service" "0" "$(q "[$policy] | length")"
  RELEASE=check render "${tls[@]}" "${gw[@]}"
  check "$app: the names follow another release name" "$svc-tls $svc" "$(q "$cert | .spec.secretName") $(q "$policy | .spec.targetRefs[0].name")"

  fails "httpScheme other than http or https fails" "sso.httpScheme" --set sso.httpScheme=HTTPS
  fails "https with the alpha config fails" "sso.alphaConfig" "${tls[@]}" --set sso.alphaConfig.enabled=true
  printf 'sso:\n  extraArgs: [--skip-provider-button]\n' >"$work/args.yaml"
  fails "extraArgs other than a map fails with TLS" "sso.extraArgs" "${tls[@]}" -f "$work/args.yaml"
  printf 'sso:\n  extraVolumes:\n  - name: other\n    emptyDir: {}\n' >"$work/volumes.yaml"
  fails "an instance's extraVolumes without tls fails with TLS" "sso.extraVolumes" "${tls[@]}" -f "$work/volumes.yaml"
done

app=cilium ns=kube-system CHART="$ROOT/argo-cd/apps/cilium"
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/cilium.yaml" || exit 1
render --set sso.enabled=false "${tls[@]}"
check "cilium: with sso.enabled=false no proxy TLS renders" "0 0" \
  "$(q '[select(.kind == "Certificate")] | length') $(q '[select(.kind == "BackendTLSPolicy")] | length')"

exit "$failures"
