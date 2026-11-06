#!/usr/bin/env bash
# Decision table for scripts/ci/check-example-clusters.sh: hand-written render
# directories, laid out as render-instance.sh's RENDER_OUT, with a cluster
# layer whose Gateway has a listener per environment.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-example-clusters.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

NONPROD=https://nonprod.example.com:6443
CORE=https://kubernetes.default.svc
GATEWAY='apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: nonprod-gateway, namespace: traefik}
spec:
  listeners:
  - {name: http, protocol: HTTP, port: 80, allowedRoutes: {namespaces: {from: All}}}
  - {name: https-wildcard, protocol: HTTPS, port: 443, hostname: "*.nonprod.example.com", allowedRoutes: {namespaces: {from: All}}}
  - {name: https-dev, protocol: HTTPS, port: 443, hostname: "*.dev.example.com", allowedRoutes: {namespaces: {from: All}}}
  - {name: https-own, protocol: HTTPS, port: 443, hostname: "*.own.example.com"}'

# app <instance/tier/app> <server> <namespace> <objects>: one app of the render.
app() {
  mkdir -p "$out/$(dirname "$1")"
  printf '%s %s\n' "$2" "$3" >"$out/$1.destination"
  printf '%s\n' "$4" >"$out/$1.yaml"
}
# fresh: a render with the nonprod layer and its Gateway alone.
fresh() {
  out=$(mktemp -d "$work/render.XXXX")
  app nonprod/core/traefik "$NONPROD" traefik "$GATEWAY"
}
# route <name> <sectionName, or - for none> <hostname, or - for none> [<gateway>]:
# an HTTPRoute.
route() {
  local section="" hosts=""
  [ "$2" = - ] || section=", sectionName: $2"
  [ "$3" = - ] || hosts="  hostnames: [$3]"
  printf 'apiVersion: gateway.networking.k8s.io/v1\nkind: HTTPRoute\nmetadata: {name: %s}\nspec:\n  parentRefs: [{name: %s, namespace: traefik%s}]\n%s\n' \
    "$1" "${4:-nonprod-gateway}" "$section" "$hosts"
}
# pod <name> <spec>: a Deployment with that pod spec.
pod() {
  printf 'apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: %s}\nspec:\n  template:\n    spec: %s\n' "$1" "$2"
}
# check <label> <expected exit> <text the output must hold>
check() {
  local got rc
  got=$("$SCRIPT" "$out" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$got"; then echo "ok   $1"; else
    echo "FAIL $1 (exit $rc, want $2, looking for '$3')"; printf '%s\n' "$got" | sed 's/^/     /'; fail=1
  fi
}

# Each route attaches to a listener of a Gateway on its cluster.
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route rustfs-api https-dev s3.dev.example.com)"
check "a route whose host matches the listener it names attaches" 0 "ok   dev/tenant/rustfs"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route rustfs-api https-wildcard s3.dev.example.com)"
check "a route whose host does not match the listener it names fails" 1 \
  "FAIL dev/tenant/rustfs: HTTPRoute rustfs-api: host s3.dev.example.com matches no listener it attaches to on traefik/nonprod-gateway (https-wildcard *.nonprod.example.com)"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route rustfs-api https-qa s3.dev.example.com)"
check "a route naming a listener its Gateway does not have fails" 1 "HTTPRoute rustfs-api: traefik/nonprod-gateway has no listener https-qa"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route rustfs-api - s3.dev.example.com)"
check "a route without a listener attaches to the one whose host matches" 0 "ok   dev/tenant/rustfs"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route redirect http s3.dev.example.com)"
check "a listener without a hostname takes every host" 0 "ok   dev/tenant/rustfs"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs "$(route rustfs-api https-own s3.own.example.com)"
check "a listener without allowedRoutes admits only its own namespace's routes" 1 \
  "HTTPRoute rustfs-api: listener https-own of traefik/nonprod-gateway admits no route of namespace dev-rustfs"
fresh; app dev/tenant/rustfs "$CORE" dev-rustfs "$(route rustfs-api https-dev s3.dev.example.com)"
check "a route whose Gateway is on another cluster fails" 1 \
  "HTTPRoute rustfs-api: no app renders the Gateway traefik/nonprod-gateway on $CORE"

# Each https host an app names under a listener of its cluster has a route.
gitea='apiVersion: v1
kind: ConfigMap
metadata: {name: gitea-config}
data: {app.ini: "ROOT_URL = https://gitea.dev.example.com/\nOAUTH = https://keycloak.dev.example.com/realms/dev"}'
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$gitea"
check "a host an app names under a listener, with no route, fails" 2 \
  "FAIL dev/tenant/gitea: names https://gitea.dev.example.com, which listener https-dev of traefik/nonprod-gateway serves, but no route or Ingress on $NONPROD has it"
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$gitea
---
$(route gitea https-dev gitea.dev.example.com)"
app dev/tenant/keycloak "$NONPROD" dev-keycloak 'apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: keycloak}
spec:
  rules: [{host: keycloak.dev.example.com}]'
check "hosts a route or an Ingress has pass" 0 "ok   dev/tenant/gitea"
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$gitea
---
$(route gitea https-wildcard gitea.dev.example.com)"
check "a route that does not attach serves no host" 3 "names https://gitea.dev.example.com, which listener https-dev"
fresh; app nonprod/core/traefik "$NONPROD" traefik "$GATEWAY
---
$(route redirect http -)"
app dev/tenant/gitea "$NONPROD" dev-gitea "$gitea"
check "a redirect on the plain http listener serves no https host" 2 "names https://gitea.dev.example.com, which listener https-dev"
fresh; app dev/core/vault "$NONPROD" vault-operator-system 'apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultConnection
metadata: {name: default}
spec: {address: "https://vault.platform.example.com"}'
check "a host no listener of the cluster serves is not checked" 0 "ok   dev/core/vault"
fresh; app platform/core/ohmlab "$CORE" argocd-system 'apiVersion: v1
kind: Secret
metadata: {name: cluster-nonprod}
stringData: {server: "https://api.nonprod.example.com:6443"}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: clusters}
data: {nonprod: "https://api.nonprod.example.com:6443"}'
check "a URL on another port than a listener's is not checked" 0 "ok   platform/core/ohmlab"
# A host belongs to the cluster of the most specific listener that covers it,
# as its DNS name points there: prod's *.example.com does not take the hub's.
PROD=https://prod.example.com:6443
fresh
app prod/core/traefik "$PROD" traefik 'apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: prod-gateway, namespace: traefik}
spec:
  listeners: [{name: https-wildcard, protocol: HTTPS, port: 443, hostname: "*.example.com", allowedRoutes: {namespaces: {from: All}}}]'
app prod/core/vault "$PROD" vault-operator-system 'apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultConnection
metadata: {name: default}
spec: {address: "https://vault.platform.example.com"}'
app platform/core/traefik "$CORE" traefik 'apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: platform-gateway, namespace: traefik}
spec:
  listeners: [{name: https-wildcard, protocol: HTTPS, port: 443, hostname: "*.platform.example.com", allowedRoutes: {namespaces: {from: All}}}]'
app platform/core/vault-operator "$CORE" vault-operator-system "$(route vault https-wildcard vault.platform.example.com platform-gateway)"
check "a host is checked on the cluster whose listener covers it most closely" 0 "ok   prod/core/vault"

# Nothing a managed cluster lacks: node labels, StorageClasses.
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$(pod gitea '{nodeSelector: {node-type: worker}}')"
check "a node selector on another label fails" 1 \
  "FAIL dev/tenant/gitea: Deployment/gitea selects nodes by node-type, which a managed cluster's nodes do not carry"
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$(pod gitea '{nodeSelector: {kubernetes.io/os: linux}, affinity: {nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: kubernetes.io/arch, operator: In, values: [amd64]}]}]}}}}')"
check "the os and arch labels pass" 0 "ok   dev/tenant/gitea"
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea "$(pod gitea '{affinity: {nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: node-type, operator: In, values: [worker]}]}]}}}}')"
check "a node affinity on another label fails" 1 "Deployment/gitea selects nodes by node-type"
fresh; app nonprod/core/exporter "$NONPROD" monitoring "$(pod exporter '{affinity: {nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: eks.amazonaws.com/compute-type, operator: NotIn, values: [fargate]}, {key: type, operator: DoesNotExist}]}]}}}}')"
check "a node affinity that only keeps off a label passes" 0 "ok   nonprod/core/exporter"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs 'apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data}
spec: {storageClassName: longhorn}'
check "a StorageClass name fails" 1 \
  "FAIL dev/tenant/rustfs: PersistentVolumeClaim/data names the StorageClass longhorn: leave it unset for the cluster's default"
fresh; app dev/tenant/rustfs "$NONPROD" dev-rustfs 'apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data}
spec: {storageClassName: ""}'
check "an empty StorageClass name fails, as it asks for none" 1 'PersistentVolumeClaim/data asks for no StorageClass (""), which only a volume made by hand binds'
fresh; app dev/tenant/gitea "$NONPROD" dev-gitea 'apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata: {name: gitea-pg}
spec: {storage: {size: 1Gi, storageClass: ""}}'
check "an empty storageClass, a field that falls back to the default, passes" 0 "ok   dev/tenant/gitea"
fresh; app dev/core/cnpg "$NONPROD" cnpg-system 'apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: clusters.postgresql.cnpg.io}
spec:
  versions: [{name: v1, schema: {openAPIV3Schema: {properties: {nodeSelector: {type: object}, storageClassName: {type: string}}}}}]'
check "a CRD's schema is not read as a selector or a StorageClass" 0 "ok   dev/core/cnpg"

# An exit status is taken modulo 256: 256 failures must not read as success.
fresh
for i in $(seq 1 256); do app "dev/tenant/a$i" "$NONPROD" "dev-a$i" "$(pod "a$i" '{nodeSelector: {node-type: worker}}')"; done
check "256 failures exit 255" 255 "FAIL dev/tenant/a256"

exit "$fail"
