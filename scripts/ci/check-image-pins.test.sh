#!/usr/bin/env bash
# Decision table for scripts/ci/check-image-pins.sh on throwaway trees.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-image-pins.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
digest=sha256:$(printf 'a%.0s' $(seq 64))

# tree <name> <comma-separated image globs> <file relative to the root> <image line>
tree() {
  local root="$work/$1" glob
  mkdir -p "$root/argo-cd/apps/kyverno" "$root/$(dirname "$3")"
  printf 'policies:\n  verifyImages:\n    images:\n' >"$root/argo-cd/apps/kyverno/values.yaml"
  IFS=, read -ra globs <<<"$2"
  for glob in "${globs[@]}"; do printf '    - "%s"\n' "$glob" >>"$root/argo-cd/apps/kyverno/values.yaml"; done
  [ -n "$4" ] && printf 'image: %s\n' "$4" >"$root/$3"
  echo "$root"
}

check() { # label expected-exit root [text the output must contain]
  local out rc
  out=$("$SCRIPT" "$3" 2>&1); rc=$?
  if [ "$rc" != "$2" ]; then
    echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${4:-}" ] && ! grep -qF -- "$4" <<<"$out"; then
    echo "FAIL $1: output does not name '$4'"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $1"
  fi
}

check "a pin under a listed wildcard passes" 0 \
  "$(tree listed 'ghcr.io/this-is-tobi/ohmlab/*' argo-cd/apps/x/values.yaml "ghcr.io/this-is-tobi/ohmlab/controller:0.3.0@$digest")"
check "a pin under an unlisted name fails and names the file" 1 \
  "$(tree unlisted 'ghcr.io/this-is-tobi/ohmlab/*' argo-cd/apps/x/values.yaml "ghcr.io/this-is-tobi/other/x:1.0.0@$digest")" \
  argo-cd/apps/x/values.yaml
check "a pin in an instance values file is checked too" 1 \
  "$(tree instance 'ghcr.io/this-is-tobi/ohmlab/*' argo-cd/instances/i/values/core/x.yaml "ghcr.io/this-is-tobi/other/x:1.0.0@$digest")" \
  argo-cd/instances/i/values/core/x.yaml
check "a tree without pins passes" 0 \
  "$(tree nopins 'ghcr.io/this-is-tobi/ohmlab/*' argo-cd/apps/x/values.yaml '')"
check "a pin matching only the old wildcard passes while it is listed" 0 \
  "$(tree old 'ghcr.io/this-is-tobi/homelab/*,ghcr.io/this-is-tobi/ohmlab/*' argo-cd/apps/x/values.yaml "ghcr.io/this-is-tobi/homelab/ohmlab-controller:0.3.0@$digest")"
check "an image without a digest is ignored" 0 \
  "$(tree nodigest 'ghcr.io/this-is-tobi/ohmlab/*' argo-cd/instances/i/values/tenant/x.yaml 'ghcr.io/this-is-tobi/tools/gh-runner:1.5.0')"

exit "$fail"
