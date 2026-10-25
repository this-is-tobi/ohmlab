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
  # The seeds are the app's last sync wave, so nothing else waits for them.
  if ! yq -o=json -I=0 'select(. != null) | [.kind, (.metadata.annotations["argocd.argoproj.io/sync-wave"] // "0")]' <<<"$rendered" \
      | jq -se 'map([.[0], (.[1] | tonumber)]) | (map(select(.[0] == "VaultSeed") | .[1]) | min) as $seed
                | (map(select(.[0] != "VaultSeed") | .[1]) | max) as $other | $seed == null or $seed > $other' >/dev/null; then
    echo "FAIL $instance: an object of vault-operator syncs in the same wave as the VaultSeeds or after them"
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
