#!/usr/bin/env bash
# The ohmlab controller's Vault policy lives in two places: the vault-operator
# chart renders it into the Vault CR, and the Go tests (and the kind e2e) run the
# controller under a copy in utils/internal/seed/testdata. A test that passes on
# a policy the cluster does not have proves nothing, so the two must be the same
# text, comments aside. The Kubernetes-auth role that carries the policy is
# checked too: bound to the controller's ServiceAccount and nothing else.
#
#   check-controller-policy.sh
#
# Needs helm and yq (mikefarah v4).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/vault-operator"
TESTDATA="$ROOT/utils/internal/seed/testdata"

# normalize drops comments, blank lines and indentation, so only the policy remains.
normalize() { sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | sed -e '/^$/d'; }

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

cr=$(render --set ohmlab.controller.enabled=true)
role=$(yq -o=json -I=0 '.spec.externalConfig.auth[] | select(.type == "kubernetes") | .roles[] | select(.name == "ohmlab-controller-vault")' <<<"$cr")
want='{"name":"ohmlab-controller-vault","bound_service_account_names":["ohmlab-controller-vault"],"bound_service_account_namespaces":["ohmlab-system"],"policies":["ohmlab-controller-vault"],"ttl":"15m"}'
if [ "$(jq -cS . <<<"$role")" != "$(jq -cS . <<<"$want")" ]; then
  echo "FAIL: the controller's role is not bound to its ServiceAccount alone:" >&2
  echo "  got  $role" >&2
  echo "  want $want" >&2
  failures=$((failures + 1))
fi

# Off by default: no policy, no role.
off=$(render)
if grep -q 'ohmlab-controller-vault' <<<"$off"; then
  echo "FAIL: the controller's policy or role renders while ohmlab.controller.enabled is false" >&2
  failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] && echo "ok: the controller's policy and role match the tests'"
exit "$failures"
