#!/usr/bin/env bash
# Decision table for scripts/ci/check-example-pod-security.sh on hand-written
# pods, against the policies the kyverno chart renders. Needs what the check
# needs (helm, yq, jq and the kyverno CLI).
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-example-pod-security.sh"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# check <label> <expected exit> <pods> [<tier>/<app> [<namespace> [<expected text>]]]
check() {
  local out rc
  out=$(printf '%s\n' "$3" | "$SCRIPT" --stdin "${4:-tenant/app}" ${5:+"$5"} 2>&1); rc=$?
  if [ "$rc" != "$2" ]; then
    echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${6:-}" ] && ! grep -qF -- "$6" <<<"$out"; then
    echo "FAIL $1: no '$6' in the output"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $1"
  fi
}

# pod [<field>=<value>...]: a Deployment every policy admits, with the fields
# given changed: name, image, seccomp (empty: none), nonroot (no: neither
# runAsNonRoot nor runAsUser), privileged, limits (no: none), init (null: an
# initContainers key with no value), volume (bare: a volume with no source),
# hook (a helm.sh/hook annotation).
pod() {
  local name=app image=docker.io/library/busybox:1.37 seccomp=RuntimeDefault nonroot=yes privileged=false limits=yes init='' volume='' hook=''
  local kv
  for kv in "$@"; do local "${kv?}"; done
  printf -- '---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: %s\n' "$name"
  [ -n "$hook" ] && printf '  annotations:\n    helm.sh/hook: %s\n' "$hook"
  printf 'spec:\n  selector:\n    matchLabels: {app: %s}\n  template:\n    metadata:\n      labels: {app: %s}\n    spec:\n      securityContext:\n' "$name" "$name"
  [ "$nonroot" = yes ] && printf '        runAsNonRoot: true\n        runAsUser: 1000\n'
  [ -n "$seccomp" ] && printf '        seccompProfile:\n          type: %s\n' "$seccomp"
  [ "$init" = null ] && printf '      initContainers:\n'
  [ "$volume" = bare ] && printf '      volumes:\n      - name: scratch\n        emptyDir:\n'
  printf '      containers:\n      - name: c\n        image: %s\n        securityContext:\n' "$image"
  printf '          allowPrivilegeEscalation: false\n          privileged: %s\n          capabilities:\n            drop: [ALL]\n' "$privileged"
  [ "$limits" = yes ] && printf '        resources:\n          limits: {cpu: 100m, memory: 64Mi}\n'
  return 0
}

check "a pod every policy admits passes" 0 "$(pod)"
check "a tenant pod without a seccomp profile is refused" 1 "$(pod seccomp=)" tenant/app app "Deployment/app pod-security-restricted"
check "the same pod passes in core, where the restricted profile does not apply" 0 "$(pod seccomp=)" core/app
check "a pod that may run as root is refused" 1 "$(pod nonroot=no)" core/app app "Deployment/app require-non-root"
check "it passes in a namespace require-non-root leaves out" 0 "$(pod nonroot=no)" core/app kube-system
check "a privileged container is refused" 1 "$(pod privileged=true)" core/app app "Deployment/app pod-security-baseline"
check "a latest tag is refused" 1 "$(pod image=docker.io/library/busybox:latest)" core/app app "Deployment/app disallow-latest-tag"
check "a container without limits is refused" 1 "$(pod limits=no)" core/app app "Deployment/app require-resource-limits"
check "an image from a registry off the allowlist is refused" 1 "$(pod image=registry.example.com/busybox:1.37)" core/app app "Deployment/app restrict-image-registries"
check "an initContainers key with no value is read as the API server drops it" 0 "$(pod init=null)"
check "a volume with no source is read as the emptyDir the API server makes it" 0 "$(pod volume=bare)"
check "a Helm test hook is left out: Argo CD never runs it" 0 "$(pod privileged=true hook=test)"
check "a pod a PolicyException of the chart covers passes" 0 "$(pod name=rustfs limits=no)" tenant/rustfs rustfs
POD_SECURITY_KNOWN="tenant/app Deployment/app pod-security-restricted" \
  check "a failure listed as known passes" 0 "$(pod seccomp=)"
POD_SECURITY_KNOWN="tenant/app Deployment/app pod-security-restricted" \
  check "a listed failure that no longer fails is refused until it is removed" 1 "$(pod)" tenant/app app "no longer fails"

# A CLI that evaluates nothing must not pass the pods it was given.
mkdir -p "$work/bin"
printf '#!/bin/sh\nexit 0\n' >"$work/bin/kyverno"
chmod +x "$work/bin/kyverno"
PATH="$work/bin:$PATH" check "a CLI that reports no result is refused" 1 "$(pod)" tenant/app app "no result"

# --instances: each enabled entry of every instance of a directory, its chart
# from APPS_DIR; the known failures are the product example's only.
mkdir -p "$work/apps/good/templates" "$work/apps/bad/templates"
printf 'apiVersion: v2\nname: good\nversion: 0.1.0\n' >"$work/apps/good/Chart.yaml"
pod name=good >"$work/apps/good/templates/good.yaml"
printf 'apiVersion: v2\nname: bad\nversion: 0.1.0\n' >"$work/apps/bad/Chart.yaml"
pod name=bad privileged=true >"$work/apps/bad/templates/bad.yaml"
d="$work/repo/argo-cd/instances/lab"
mkdir -p "$d/values/core"
printf 'instance: {name: lab}\nrepoURL: https://git.example/x.git\n' >"$d/instance.yaml"
printf 'apps:\n- {app: good, enabled: "true"}\n- {app: bad, enabled: "true"}\n' >"$d/core.yaml"
printf 'apps: []\n' >"$d/tenant.yaml"
for a in good bad; do printf '{}\n' >"$d/values/core/$a.yaml"; done
out=$(APPS_DIR="$work/apps" "$SCRIPT" --instances "$work/repo/argo-cd/instances" 2>&1); rc=$?
if [ "$rc" = 1 ] && grep -qF 'ok   lab/core/good' <<<"$out" && grep -qF 'FAIL lab/core/bad' <<<"$out" && ! grep -qF 'no such app' <<<"$out"; then
  echo "ok   --instances checks each enabled app of every instance, without the example's known failures"
else
  echo "FAIL --instances: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

# --instances: an app is checked against the policies of the instance that
# runs kyverno on its cluster (its values), else the example's.
mkdir -p "$work/apps/rustfs/templates"
printf 'apiVersion: v2\nname: rustfs\nversion: 0.1.0\n' >"$work/apps/rustfs/Chart.yaml"
pod name=rustfs limits=no >"$work/apps/rustfs/templates/rustfs.yaml"
ln -s "$ROOT/argo-cd/apps/kyverno" "$work/apps/kyverno"
r="$work/clusters/argo-cd/instances"
mkdir -p "$r/layer/values/core" "$r/dev/values/tenant" "$r/away/values/tenant"
printf 'instance: {name: layer}\nrepoURL: https://git.example/x.git\ndestination: {server: "https://s.example.com:6443"}\n' >"$r/layer/instance.yaml"
printf 'apps:\n- {app: kyverno, enabled: "true"}\n' >"$r/layer/core.yaml"
printf 'exceptionNamespaces:\n  rustfs-init-step-limits: [dev-rustfs]\n' >"$r/layer/values/core/kyverno.yaml"
for i in dev away; do
  server=https://s.example.com:6443
  [ "$i" = away ] && server=https://other.example.com:6443
  printf 'instance: {name: %s}\nrepoURL: https://git.example/x.git\ndestination: {server: "%s"}\nnamespacePrefix: dev-\n' "$i" "$server" >"$r/$i/instance.yaml"
  printf 'apps:\n- {app: rustfs, enabled: "true"}\n' >"$r/$i/tenant.yaml"
  printf '{}\n' >"$r/$i/values/tenant/rustfs.yaml"
done
out=$(APPS_DIR="$work/apps" "$SCRIPT" --instances "$r" 2>&1); rc=$?
if [ "$rc" = 1 ] && grep -qx 'ok   dev/tenant/rustfs' <<<"$out" && grep -qF 'FAIL away/tenant/rustfs Deployment/rustfs require-resource-limits' <<<"$out"; then
  echo "ok   --instances checks an app against the policies of its own cluster's kyverno"
else
  echo "FAIL --instances with a kyverno per cluster: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

# mkcase <dir>: an instances directory where layer runs kyverno, its rustfs
# exception extended to dev-rustfs, on the cluster dev runs rustfs on.
S1=https://s.example.com:6443
mkcase() {
  local r="$1"
  rm -rf "$r"
  mkdir -p "$r/layer/values/core" "$r/dev/values/tenant"
  printf 'instance: {name: layer}\nrepoURL: https://git.example/x.git\ndestination: {server: "%s"}\n' "$S1" >"$r/layer/instance.yaml"
  printf 'apps:\n- {app: kyverno, enabled: "true"}\n' >"$r/layer/core.yaml"
  printf 'exceptionNamespaces:\n  rustfs-init-step-limits: [dev-rustfs]\n' >"$r/layer/values/core/kyverno.yaml"
  printf 'instance: {name: dev}\nrepoURL: https://git.example/x.git\ndestination: {server: "%s"}\nnamespacePrefix: dev-\n' "$S1" >"$r/dev/instance.yaml"
  printf 'apps:\n- {app: rustfs, enabled: "true"}\n' >"$r/dev/tenant.yaml"
  printf '{}\n' >"$r/dev/values/tenant/rustfs.yaml"
}
# percluster <label> <instances dir> <fixed text the output must hold>
percluster() {
  local out
  out=$(APPS_DIR="$work/apps" "$SCRIPT" --instances "$2" 2>&1)
  if grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1: the output does not say: $3"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  fi
}
r="$work/cases/argo-cd/instances"
mkcase "$r"
printf 'apps: []\n' >"$r/layer/core.yaml"
printf 'apps:\n- {app: kyverno, enabled: "true"}\n' >"$r/layer/tenant.yaml"
mkdir -p "$r/layer/values/tenant"
mv "$r/layer/values/core/kyverno.yaml" "$r/layer/values/tenant/kyverno.yaml"
percluster "--instances reads a kyverno of the tenant tier" "$r" "ok   dev/tenant/rustfs"
mkcase "$r"
printf 'instance: {name: layer}\nrepoURL: https://git.example/x.git\ndestination: {server: "https://elsewhere.example.com:6443"}\n' >"$r/layer/instance.yaml"
printf 'apps:\n- {app: kyverno, enabled: "true", destination: {server: "%s"}}\n' "$S1" >"$r/layer/core.yaml"
percluster "--instances places a kyverno by its entry's own server" "$r" "ok   dev/tenant/rustfs"
mkcase "$r"
printf 'instance: {name: dev}\nrepoURL: https://git.example/x.git\ndestination: {server: "https://elsewhere.example.com:6443"}\nnamespacePrefix: dev-\n' >"$r/dev/instance.yaml"
printf 'apps:\n- {app: rustfs, enabled: "true", destination: {server: "%s"}}\n' "$S1" >"$r/dev/tenant.yaml"
percluster "--instances places an app by its entry's own server" "$r" "ok   dev/tenant/rustfs"
mkcase "$r"
printf 'apps:\n- {app: kyverno, enabled: "false"}\n' >"$r/layer/core.yaml"
percluster "--instances leaves out a kyverno that is not enabled" "$r" "FAIL dev/tenant/rustfs Deployment/rustfs require-resource-limits"
mkcase "$r"
printf 'exceptionNamespaces:\n  nope: [x]\n' >"$r/layer/values/core/kyverno.yaml"
out=$(APPS_DIR="$work/apps" "$SCRIPT" --instances "$r" 2>&1); rc=$?
if [ "$rc" != 0 ] && grep -q '^FAIL layer/core/kyverno: .*exceptionNamespaces.nope' <<<"$out"; then
  echo "ok   --instances fails a kyverno whose policies do not render, naming it"
else
  echo "FAIL --instances with a kyverno whose policies do not render: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

exit "$fail"
