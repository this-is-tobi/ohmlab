#!/usr/bin/env bash
# The VaultSeed objects the vault-operator chart renders for an instance must be
# valid: templates well formed, no two objects on one path, no reference cycle.
# `ohmlab validate` is what the controller applies, run here without a cluster
# on what each instance would deploy.
#
#   check-vault-seeds.sh
#
# Needs helm and Go (it runs the CLI from utils/).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
failures=0
(cd "$ROOT/utils" && go build -o "${TMPDIR:-/tmp}/ohmlab-validate" .) || { echo "FAIL: cannot build the CLI" >&2; exit 1; }
trap 'rm -f "${TMPDIR:-/tmp}/ohmlab-validate"' EXIT

for values in "$ROOT"/argo-cd/instances/*/values/core/vault-operator.yaml; do
  instance="$(basename "$(dirname "$(dirname "$(dirname "$values")")")")"
  if ! rendered=$(helm template check "$ROOT/argo-cd/apps/vault-operator" -f "$values" --namespace vault-operator-system \
        --set ohmlab.vault.seeds.enabled=true --set ohmlab.vault.seeds.requireCRD=false 2>&1); then
    echo "FAIL $instance: $(grep -m1 '^Error' <<<"$rendered" | cut -c1-240)"
    failures=$((failures + 1))
    continue
  fi
  if out=$("${TMPDIR:-/tmp}/ohmlab-validate" validate - <<<"$rendered" 2>&1); then
    echo "ok   $instance: $out"
  else
    echo "FAIL $instance:"; printf '%s\n' "$out" | sed 's/^/     /'
    failures=$((failures + 1))
  fi
done
exit "$failures"
