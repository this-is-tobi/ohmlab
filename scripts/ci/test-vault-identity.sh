#!/usr/bin/env bash
# Identity groups and their OIDC aliases are bank-vaults' to apply and nobody
# else's: a group created with the right policies and an alias to an OIDC group
# the creator controls is a way to any policy, and bank-vaults already writes the
# policies and the auth roles, so it gains no power by writing these too. This
# checks the vault-operator chart renders them into the Vault CR, and that the
# retired post-config Job, which used to write them, does not come back.
#
#   test-vault-identity.sh
#
# Needs helm and yq (mikefarah v4).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/vault-operator"
# Its own values, so the check does not depend on what an instance declares.
VALUES="$(mktemp)"
trap 'rm -f "$VALUES"' EXIT
cat >"$VALUES" <<'YAML'
vault:
  externalConfig:
    oidcEnabled: true
ohmlab:
  vault:
    identity:
      groups:
      - name: homelab-admins
        type: external
        policies: [admin]
      groupAliases:
      - name: /admin
        group: homelab-admins
YAML

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
render() { helm template check "$CHART" -f "$VALUES" --namespace vault-operator-system "$@" 2>&1; }

out=$(render)
cr=$(yq 'select(.kind == "Vault") | .spec.externalConfig' <<<"$out")

[ "$(yq -o=json -I=0 '.groups[0] | [.name, .type, .policies[0]]' <<<"$cr")" = '["homelab-admins","external","admin"]' ] \
  || fail "the Vault CR lacks the homelab-admins group (external, policy admin)"
[ "$(yq -o=json -I=0 '.["group-aliases"][0] | [.name, .mountpath, .group]' <<<"$cr")" = '["/admin","oidc","homelab-admins"]' ] \
  || fail "the Vault CR lacks the /admin alias on the oidc mount"

[ -z "$(yq 'select(.metadata.name == "vault-post-config*") | .kind' <<<"$out")" ] \
  || fail "the chart renders a vault-post-config object again"
[ "$(yq -o=json -I=0 '[.policies[].name, .auth[].roles[]?.name] | map(select(. == "vault-post-config")) | length' <<<"$cr")" = 0 ] \
  || fail "the Vault CR declares the vault-post-config policy or role again"

# A custom mount path is honoured, per alias or as the default.
out=$(render --set ohmlab.vault.identity.mountPath=jwt-main)
custom=$(yq 'select(.kind == "Vault") | .spec.externalConfig["group-aliases"][0].mountpath' <<<"$out")
[ "$custom" = jwt-main ] || fail "the identity mountPath is ignored (got $custom)"

# Aliases on the oidc mount need the oidc auth method.
out=$(render --set vault.externalConfig.oidcEnabled=false)
grep -q 'groupAliases map groups of the oidc auth method' <<<"$out" || fail "aliases without the oidc auth method must fail the render"

# Nothing declared, nothing rendered.
out=$(render --set-json 'ohmlab.vault.identity.groups=[]' --set-json 'ohmlab.vault.identity.groupAliases=[]')
cr=$(yq 'select(.kind == "Vault")' <<<"$out")
[ "$(yq '.spec.externalConfig | has("groups")' <<<"$cr")" = false ] && [ "$(yq '.spec.externalConfig | has("group-aliases")' <<<"$cr")" = false ] \
  || fail "empty identity values still render groups"

[ "$failures" -eq 0 ] && echo "ok: identity groups come from the Vault CR"
exit "$failures"
