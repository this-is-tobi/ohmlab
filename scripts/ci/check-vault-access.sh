#!/usr/bin/env bash
# The Vault Secrets Operator only works when what a chart asks for matches
# what the vault-operator chart configures in Vault: the role the chart's
# VaultAuth logs in with must exist and be bound to that service account and
# namespace, the role must require a token audience and the VaultAuth request
# it (not log in with a Kubernetes API token), the KV
# mount of every VaultStaticSecret must exist, and one of the
# role's policies must let it read the secret path. A mismatch is a silent
# `403 permission denied` on the cluster, and the instance values and the
# chart defaults can drift apart (an instance can replace the policy and role
# lists wholesale).
#
# With --stdin <label> <namespace> <vault.json> the chart manifests are read
# from standard input and checked against that Vault description; without
# arguments every instance under argo-cd/instances is rendered (vault-operator
# and each chart that declares VSO objects, with the instance values) and
# checked. Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"

# check_stream <label> <namespace> <vault.json>: reads manifests on stdin,
# prints one line per violation. vault.json is {mounts, roles, policies}:
# the KV mounts, the kubernetes-auth roles and the policies (name, rules).
check_stream() {
  yq ea -o=json -I=0 '[select(.kind == "VaultAuth" or .kind == "VaultStaticSecret")]' - 2>/dev/null \
    | jq -r --arg label "$1" --arg ns "$2" --slurpfile vault "$3" '
        # A Vault policy path as a regular expression: "+" is one path
        # segment, a trailing "*" is any suffix.
        def pattern: gsub("\\."; "\\.") | gsub("\\+"; "[^/]+") | sub("\\*$"; ".*") | "^" + . + "$";
        # Whether the policy rules let one read $api.
        def reads($api):
          [ split("\n") | map(select(test("^\\s*#") | not)) | join("\n")
            | match("path\\s+\"([^\"]+)\"\\s*\\{([^}]*)\\}"; "g")
            | {path: .captures[0].string, body: .captures[1].string}
            | select(.body | test("\"read\""))
            | (.path | pattern) as $re
            | select($api | test($re)) ] | length > 0;
        $vault[0] as $v
        | . as $objs
        | ($objs | map(select(.kind == "VaultAuth"))) as $auths
        | ($auths[]
           | . as $a
           | ($a.metadata.namespace // $ns) as $ans
           | ($v.roles | map(select(.name == $a.spec.kubernetes.role)) | first) as $role
           | if $role == null then
               "\($label): VaultAuth \($a.metadata.name) logs in with role \"\($a.spec.kubernetes.role)\", which Vault does not define"
             elif (($role.bound_service_account_names // []) | (index($a.spec.kubernetes.serviceAccount) or index("*"))) | not then
               "\($label): role \"\($role.name)\" is not bound to the service account \"\($a.spec.kubernetes.serviceAccount)\" that VaultAuth \($a.metadata.name) uses (bound to: \(($role.bound_service_account_names // []) | join(", ")))"
             elif (($role.bound_service_account_namespaces // []) | (index($ans) or index("*"))) | not then
               "\($label): role \"\($role.name)\" is not bound to the namespace \"\($ans)\" of VaultAuth \($a.metadata.name) (bound to: \(($role.bound_service_account_namespaces // []) | join(", ")))"
             elif ($a.spec.kubernetes.audiences // []) == [] then
               "\($label): VaultAuth \($a.metadata.name) requests no token audience, so VSO logs in with a Kubernetes API token: set kubernetes.audiences: [vault]"
             elif ($role.audience // "") == "" then
               "\($label): role \"\($role.name)\" of VaultAuth \($a.metadata.name) requires no token audience, so any token of the service account logs in, the API token of its pods too: set audience: vault"
             elif (($a.spec.kubernetes.audiences // []) | index($role.audience)) == null then
               "\($label): role \"\($role.name)\" accepts only the audience \"\($role.audience)\", which VaultAuth \($a.metadata.name) does not request (requests: \(($a.spec.kubernetes.audiences // []) | join(", ")))"
             else empty end),
          ($objs[] | select(.kind == "VaultStaticSecret") as $s
           | ($auths | map(select(.metadata.name == $s.spec.vaultAuthRef)) | first) as $a
           | if $a == null then
               "\($label): \($s.metadata.name) references VaultAuth \"\($s.spec.vaultAuthRef // "")\", which the chart does not render"
             elif ($v.mounts | index($s.spec.mount)) == null then
               "\($label): \($s.metadata.name) reads the mount \"\($s.spec.mount)\", which is not a KV mount of Vault (mounts: \($v.mounts | join(", ")))"
             else
               ($v.roles | map(select(.name == $a.spec.kubernetes.role)) | first) as $role
               | "\($s.spec.mount)/data/\($s.spec.path)" as $api
               | if $role != null and ([ ($role.policies // [])[] as $p | $v.policies[] | select(.name == $p) | .rules | select(reads($api)) ] | length) == 0 then
                   "\($label): role \"\($role.name)\" cannot read \($api) for \($s.metadata.name) (policies: \(($role.policies // []) | join(", ")))"
                 else empty end
             end)'
}

if [ "${1:-}" = "--stdin" ]; then
  violations=$(check_stream "${2:?label}" "${3:?namespace}" "${4:?vault.json}")
  [ -z "$violations" ] || { printf '%s\n' "$violations"; exit 1; }
  exit 0
fi

# vault_json <instance>: the Vault description of an instance, from the
# vault-operator chart rendered with that instance's values.
vault_json() {
  local vf="$INSTANCES_DIR/$1/values/core/vault-operator.yaml" args=()
  [ -f "$vf" ] && args=(-f "$vf")
  helm template check "$ROOT/argo-cd/apps/vault-operator" "${args[@]+"${args[@]}"}" 2>/dev/null \
    | yq ea -o=json -I=0 '[select(.kind == "Vault")][0].spec.externalConfig
        | {"mounts": [.secrets[] | select(.type == "kv") | .path],
           "roles": [.auth[] | select(.type == "kubernetes") | .roles[]],
           "policies": .policies}' -
}

failures=0
for dir in "$INSTANCES_DIR"/*/; do
  inst="$(basename "$dir")"
  vault="${TMPDIR:-/tmp}/check-vault-access.$$.json"
  if ! vault_json "$inst" >"$vault" || ! jq -e '.roles | length > 0' "$vault" >/dev/null 2>&1; then
    echo "FAIL $inst: the vault-operator chart renders no Vault config with the values of this instance"
    failures=$((failures + 1))
    continue
  fi
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")

  for chart in "$ROOT/utils/helm" "$ROOT"/argo-cd/apps/*/; do
    chart="${chart%/}"
    [ -f "$chart/Chart.yaml" ] || continue
    name="$(basename "$chart")"
    [ "$name" = helm ] && name=ohmlab
    # Charts without the vso-utils dependency render no VSO objects, except
    # the two that write theirs by hand.
    case "$name" in
      vault-operator | ohmlab) ;;
      *) grep -q 'name: vso-utils' "$chart/Chart.yaml" || continue ;;
    esac

    # The catalogue entry of this instance that deploys the chart.
    scope="" entry=""
    for s in core tenant; do
      e=$(CHART="$name" yq -o=json -I=0 '.apps[] | select((.chart // .app) == strenv(CHART))' "$dir/$s.yaml" 2>/dev/null | head -n1)
      if [ -n "$e" ]; then scope="$s" entry="$e"; break; fi
    done
    [ -n "$entry" ] || continue
    app=$(jq -r '.app' <<<"$entry")
    ns=$(jq -r --arg p "$prefix" --arg s "$suffix" '.namespace // "\($p)\(.app)\($s)"' <<<"$entry")
    release=$(jq -r '.releaseName // .app' <<<"$entry")

    args=()
    vf="$dir/values/$scope/$app.yaml"
    [ -f "$vf" ] && args=(-f "$vf")
    if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
      helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL $inst/$name: helm dependency build"; failures=$((failures + 1)); continue; }
    fi
    if ! rendered=$(helm template "$release" "$chart" --namespace "$ns" "${args[@]+"${args[@]}"}" 2>/dev/null); then
      echo "FAIL $inst/$name: does not render (see the render step)"
      failures=$((failures + 1))
      continue
    fi
    if violations=$(printf '%s\n' "$rendered" | check_stream "$inst/$name" "$ns" "$vault") && [ -z "$violations" ]; then
      echo "ok   $inst/$name"
    else
      printf '%s\n' "$violations" | sed 's/^/FAIL /'
      failures=$((failures + 1))
    fi
  done
  rm -f "$vault"
done

exit "$failures"
