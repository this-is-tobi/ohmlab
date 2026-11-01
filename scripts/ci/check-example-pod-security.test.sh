#!/usr/bin/env bash
# Decision table for scripts/ci/check-example-pod-security.sh on hand-written
# pods, against the policies the kyverno chart renders. Needs what the check
# needs (helm, yq, jq and the kyverno CLI).
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-example-pod-security.sh"
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

exit "$fail"
