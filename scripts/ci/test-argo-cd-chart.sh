#!/usr/bin/env bash
# Tests of the argo-cd app (the tenant Argo CD) whose failures only a running
# cluster would otherwise show: its Redis runs the exporter the ServiceMonitor
# scrapes, and with TLS on, the default, it serves a certificate that names the
# Service the components dial and verify, from the issuer the values name, and
# reloads it when it renews.
#
#   test-argo-cd-chart.sh
#
# Needs helm and yq (mikefarah v4). No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/argo-cd"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/argo-cd.yaml" || { echo "no manager parameters for argo-cd" >&2; exit 1; }

# check <label> <expected> <actual>
check() {
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# render [<helm args>...]: the chart with the example instance's values into
# $work/render.yaml, left empty when it does not render.
render() {
  if ! helm template argo-cd "$CHART" --namespace argo-cd "${args[@]}" "$@" >"$work/render.yaml" 2>"$work/err"; then
    : >"$work/render.yaml"
    check "the chart renders${*:+ with $*}" "" "$(grep -m1 Error "$work/err" | cut -c1-200)"
  fi
}

# q <expression>: yq over every document of the last render.
q() { yq ea "$1" "$work/render.yaml"; }

spec='select(.kind == "Deployment" and .metadata.name == "argo-cd-argocd-redis") | .spec.template.spec'
render
check "the metrics Service's target port is a port of the Redis pod" "metrics" \
  "$(q "$spec | .containers[].ports[]? | select(.name == \"metrics\") | .name")"
check "the exporter has limits" "true" \
  "$(q "$spec | .containers[] | select(.name == \"metrics\") | (.resources.limits.cpu != null and .resources.limits.memory != null) | tostring")"
# Its /scrape?target= would dial any target with Redis's password.
check "the exporter scrapes only its own Redis" "true" \
  "$(q "$spec | .containers[] | select(.name == \"metrics\") | .env[] | select(.name == \"REDIS_EXPORTER_DISABLE_SCRAPE_ENDPOINT\") | .value")"
check "the Redis pod mounts no service account token" "false" "$(q "$spec | .automountServiceAccountToken")"

cert='select(.kind == "Certificate" and .metadata.name == "argocd-redis-tls")'
# The Redis objects only: the server has a certificate of its own.
pki='[select(.apiVersion == "cert-manager.io/v1" and (.metadata.name | test("^argocd-redis"))) | .kind + "/" + .metadata.name] | sort | join(" ")'
reload="$spec | .containers[] | select(.name == \"tls-reload\")"
names="argo-cd-argocd-redis,argo-cd-argocd-redis.argo-cd,argo-cd-argocd-redis.argo-cd.svc,argo-cd-argocd-redis.argo-cd.svc.cluster.local"
# The Secret and key of Redis's password, in the redis container then the sidecar.
pw='[.containers[] | select(.name == "redis") | .env[] | select(.name == "REDIS_PASSWORD") | .valueFrom.secretKeyRef | .name + "/" + .key] + [.containers[] | select(.name == "tls-reload") | .env[] | select(.name == "REDISCLI_AUTH") | .valueFrom.secretKeyRef | .name + "/" + .key] | join(" ")'
# redis_args <kind> <name> <container>: the --redis-* arguments of one container.
redis_args() { q "select(.kind == \"$1\" and .metadata.name == \"$2\") | .spec.template.spec.containers[] | select(.name == \"$3\") | [.args[]? | select(test(\"^--redis-\"))] | join(\" \")"; }
check "by default a CA of the namespace issues Redis's certificate" \
  "Certificate/argocd-redis-ca Certificate/argocd-redis-tls Issuer/argocd-redis-ca Issuer/argocd-redis-selfsigned" "$(q "$pki")"
check "the namespace CA keeps its key when it renews" "Never" \
  "$(q 'select(.kind == "Certificate" and .metadata.name == "argocd-redis-ca") | .spec.privateKey.rotationPolicy')"
check "the certificate names the Service the components dial" "$names" "$(q "$cert | .spec.dnsNames | join(\",\")")"
check "that Service is the rendered one" "argo-cd-argocd-redis" "$(q 'select(.kind == "Service" and .metadata.name == "argo-cd-argocd-redis") | .metadata.name')"
check "it lasts 90 days, renews 15 days ahead, serves only" "2160h 360h server auth" "$(q "$cert | .spec.duration + \" \" + .spec.renewBefore + \" \" + (.spec.usages | join(\",\"))")"
check "Redis serves that Secret" "argocd-redis-tls" "$(q "$spec | .volumes[] | select(.name == \"redis-tls\") | .secret.secretName")"
for c in "Deployment argo-cd-argocd-server server" "Deployment argo-cd-argocd-repo-server repo-server" "StatefulSet argo-cd-argocd-application-controller application-controller"; do
  # shellcheck disable=SC2086
  check "${c#* } verifies Redis" "--redis-use-tls --redis-ca-certificate=/app/config/redis-tls/ca.crt" "$(redis_args $c)"
done
check "the ApplicationSet controller, which does not use Redis, is unchanged" "applicationset-controller:" \
  "$(q 'select(.kind == "Deployment" and .metadata.name == "argo-cd-argocd-applicationset-controller") | .spec.template.spec.containers[] | select(.name == "applicationset-controller") | .name + ":" + ([.args[]? | select(test("^--redis-"))] | join(" "))')"
check "the sidecar reloads the certificate Redis serves" "1" \
  "$(q "[$reload | .args[] | select(test(\"redis-cli --tls --insecure -p 6379 CONFIG SET tls-cert-file /app/config/redis-tls/tls.crt tls-key-file /app/config/redis-tls/tls.key\"))] | length")"
check "the reload script parses" "0" "$(sh -n -c "$(q "$reload | .args[0]")" 2>/dev/null; echo $?)"
# redis-cli exits 0 on an error reply: only the reply tells a refused reload.
check "a reload Redis refuses is reported" "1" "$(q "[$reload | .args[] | select(contains(\"= OK ]\"))] | length")"
check "the sidecar runs Redis's image" "true" \
  "$(q "[$spec | .containers[] | select(.name == \"redis\" or .name == \"tls-reload\") | .image] | (length == 2 and .[0] == .[1]) | tostring")"
# The sidecar copies the subchart's choice of Secret: a divergence would keep
# the whole Redis pod from starting.
check "the sidecar takes Redis's password from its environment" "argocd-redis/auth argocd-redis/auth" "$(q "$spec | $pw")"
check "the sidecar has limits and runs restricted" "true true ALL" \
  "$(q "$reload | ((.resources.limits.cpu != null and .resources.limits.memory != null) | tostring) + \" \" + (.securityContext.readOnlyRootFilesystem | tostring) + \" \" + (.securityContext.capabilities.drop | join(\",\"))")"
render --set argo-cd.redis.tls.enabled=false
check "with TLS off Redis has no certificate" "" "$(q "$pki")"
check "with TLS off the reload sidecar idles" "tls-reload 0" \
  "$(q "$reload | .name + \" \" + ([.args[] | select(test(\"CONFIG SET\"))] | length | tostring)")"
check "the idle script parses" "0" "$(sh -n -c "$(q "$reload | .args[0]")" 2>/dev/null; echo $?)"
render --set argo-cd.redis.existingSecret=my-redis --set argo-cd.redisSecretInit.enabled=false
check "with the instance's own Secret, the sidecar takes the same password" "my-redis/auth my-redis/auth" "$(q "$spec | $pw")"
render --set argo-cd.redis.containerPorts.redis=6380
check "the sidecar reloads on Redis's port" "1" \
  "$(q "[$reload | .args[] | select(test(\"redis-cli --tls --insecure -p 6380 CONFIG SET\"))] | length")"
render --set argo-cd.redis.tls.enabled=true --set redisCertificate.issuerRef.kind=ClusterIssuer --set redisCertificate.issuerRef.name=ohmlab-internal-ca
check "an issuerRef issues it" "ClusterIssuer/ohmlab-internal-ca" "$(q "$cert | .spec.issuerRef.kind + \"/\" + .spec.issuerRef.name")"
check "with an issuerRef no namespace CA renders" "Certificate/argocd-redis-tls" "$(q "$pki")"
check "the names do not depend on the issuer" "$names" "$(q "$cert | .spec.dnsNames | join(\",\")")"
render --set argo-cd.redis.tls.enabled=true --set argo-cd.redis-ha.enabled=true
check "redis-ha gets no certificate (TLS covers the single node Redis)" "" "$(q "$pki")"
render --set argo-cd.redis.tls.enabled=true --set argo-cd.redis.enabled=false
check "without Redis, no certificate" "" "$(q "$pki")"

exit "$failures"
