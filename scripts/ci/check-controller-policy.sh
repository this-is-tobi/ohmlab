#!/usr/bin/env bash
# The ohmlab controller's Vault policy lives in two places: the vault-operator
# chart renders it into the Vault CR, and the Go tests (and the kind e2e) run the
# controller under a copy in cli/internal/seed/testdata. A test that passes on
# a policy the cluster does not have proves nothing, so the two must be the same
# text, comments aside. The Kubernetes-auth role that carries the policy is
# checked too: bound to the controller's ServiceAccount and the token audience
# its chart logs in with, and nothing else.
#
#   check-controller-policy.sh
#
# Needs helm and yq (mikefarah v4).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/vault-operator"
TESTDATA="$ROOT/cli/internal/seed/testdata"

# normalize drops comments, blank lines, indentation and repeated spaces, so only the policy remains.
normalize() { sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/[[:space:]][[:space:]]*/ /g' | sed -e '/^$/d'; }

# render prints the Vault CR for the given extra helm arguments.
render() { helm template check "$CHART" --namespace vault-operator-system --set vault.enabled=true "$@" | yq 'select(.kind == "Vault")'; }

failures=0
check() { # <allowDelete> <testdata file>
  local cr rendered expected
  cr=$(render --set ohmlab.controller.enabled=true --set "ohmlab.controller.allowDelete=$1")
  rendered=$(yq '.spec.externalConfig.policies[] | select(.name == "ohmlab-controller-vault") | .rules' <<<"$cr" | normalize)
  expected=$(normalize <"$TESTDATA/$2")
  if [ -z "$rendered" ] || [ "$rendered" != "$expected" ]; then
    echo "FAIL: the chart's policy (allowDelete=$1) differs from $2:" >&2
    diff <(echo "$expected") <(echo "$rendered") >&2 || true
    failures=$((failures + 1))
  fi
}
check false controller-policy.hcl
check true controller-policy-allow-delete.hcl

# An instance whose mount is not the system's: the same stanzas for the system
# mount, then for the instance's.
cr=$(render --set ohmlab.controller.enabled=true --set-string global.ohmlab.names.vaultMount=lab)
rendered=$(yq '.spec.externalConfig.policies[] | select(.name == "ohmlab-controller-vault") | .rules' <<<"$cr" | normalize)
expected=$(normalize <"$TESTDATA/controller-policy.hcl"; sed 's|"ohmlab/|"lab/|' "$TESTDATA/controller-policy.hcl" | normalize)
if [ "$rendered" != "$expected" ]; then
  echo "FAIL: the chart's policy for an instance mount (lab) is not the system mount's stanzas followed by the instance's:" >&2
  diff <(echo "$expected") <(echo "$rendered") >&2 || true
  failures=$((failures + 1))
fi

cr=$(render --set ohmlab.controller.enabled=true)
role=$(yq -o=json -I=0 '.spec.externalConfig.auth[] | select(.type == "kubernetes") | .roles[] | select(.name == "ohmlab-controller-vault")' <<<"$cr")
want='{"name":"ohmlab-controller-vault","bound_service_account_names":["ohmlab-controller-vault"],"bound_service_account_namespaces":["ohmlab-system"],"policies":["ohmlab-controller-vault"],"ttl":"15m","audience":"vault"}'
if [ "$(jq -cS . <<<"$role")" != "$(jq -cS . <<<"$want")" ]; then
  echo "FAIL: the controller's role is not bound to its ServiceAccount and the vault audience alone:" >&2
  echo "  got  $role" >&2
  echo "  want $want" >&2
  failures=$((failures + 1))
fi

# An empty audience drops the key: the role then accepts any audience.
cr=$(render --set ohmlab.controller.enabled=true --set ohmlab.controller.audience=)
if [ "$(yq '.spec.externalConfig.auth[] | select(.type == "kubernetes") | .roles[] | select(.name == "ohmlab-controller-vault") | has("audience")' <<<"$cr")" != false ]; then
  echo "FAIL: an empty ohmlab.controller.audience must leave the role without one" >&2
  failures=$((failures + 1))
fi

# Off by default: no policy, no role.
off=$(render)
if grep -q 'ohmlab-controller-vault' <<<"$off"; then
  echo "FAIL: the controller's policy or role renders while ohmlab.controller.enabled is false" >&2
  failures=$((failures + 1))
fi

# The storage target's policy is the one its tests run under
# (cli/internal/controller/storage/testdata/storage-policy.hcl), for one mount
# and one store: its mount and the store's root keys path move with them.
STORAGE="$ROOT/cli/internal/controller/storage/testdata/storage-policy.hcl"
storage_check() { # <mount> <store root keys path> [<helm args>...]
  local mount="$1" path="$2" cr rendered expected
  shift 2
  cr=$(render --set-json "ohmlab.controller.storage.stores=[{\"name\":\"s\",\"path\":\"$path\"}]" "$@")
  rendered=$(yq '.spec.externalConfig.policies[] | select(.name == "ohmlab-controller-storage") | .rules' <<<"$cr" | normalize)
  expected=$(sed -e "s|\"ohmlab/data/platforms/e2e/core/rustfs\"|\"${path%%/*}/data/${path#*/}\"|" -e "s|\"ohmlab/|\"$mount/|" "$STORAGE" | normalize)
  if [ -z "$rendered" ] || [ "$rendered" != "$expected" ]; then
    echo "FAIL: the chart's storage policy (mount $mount, store $path) differs from storage-policy.hcl:" >&2
    diff <(echo "$expected") <(echo "$rendered") >&2 || true
    failures=$((failures + 1))
  fi
}
storage_check ohmlab ohmlab/platforms/e2e/core/rustfs
storage_check lab lab/platforms/production/core/rustfs-backups --set systemMount=lab --set-string global.ohmlab.names.vaultMount=lab

cr=$(render --set-json 'ohmlab.controller.storage.stores=[{"name":"s","path":"ohmlab/platforms/e2e/core/rustfs"}]')
role=$(yq -o=json -I=0 '.spec.externalConfig.auth[] | select(.type == "kubernetes") | .roles[] | select(.name == "ohmlab-controller-storage")' <<<"$cr")
want='{"name":"ohmlab-controller-storage","bound_service_account_names":["ohmlab-controller-storage"],"bound_service_account_namespaces":["ohmlab-system"],"policies":["ohmlab-controller-storage"],"ttl":"15m","audience":"vault"}'
if [ "$(jq -cS . <<<"$role")" != "$(jq -cS . <<<"$want")" ]; then
  echo "FAIL: the storage target's role is not bound to its ServiceAccount and the vault audience alone:" >&2
  echo "  got  $role" >&2
  echo "  want $want" >&2
  failures=$((failures + 1))
fi
if grep -q 'ohmlab-controller-storage' <<<"$off"; then
  echo "FAIL: the storage target's policy or role renders without stores" >&2
  failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] && echo "ok: the controller's policy and role match the tests'"
exit "$failures"
