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
# A remote instance (vault.hub) runs no Vault: its apps log in to its hub's
# with the jwt method, on the hub's mount jwt-<instance>, whose role must be
# bound to the app's namespace and service account and share an audience with
# its VaultAuth. The hub is the instance of that name in the same folder, and
# every enabled entry of the remote instance is checked, in either tier.
#
# With --stdin <label> <namespace> <vault.json> [<remote instance>] the chart
# manifests are read from standard input and checked against that Vault
# description, as the manifests of that remote instance when one is named;
# without arguments every instance under argo-cd/instances is rendered
# (vault-operator and each chart that declares VSO objects, with the instance
# values) and checked. Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"

# check_stream <label> <namespace> <vault.json> [<remote instance>]: reads
# manifests on stdin, prints one line per violation. vault.json is {mounts,
# roles, jwt, policies}: the KV mounts, the kubernetes-auth roles, the roles of
# each jwt auth mount by its path, and the policies (name, rules).
check_stream() {
  yq ea -o=json -I=0 '[select(.kind == "VaultAuth" or .kind == "VaultStaticSecret")]' - 2>/dev/null \
    | jq -r --arg label "$1" --arg ns "$2" --slurpfile vault "$3" --arg remote "${4:-}" '
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
        # The role a VaultAuth logs in with, or null: a kubernetes role, or
        # with the jwt method a role of the jwt mount it names.
        def role_of($a):
          if $a.spec.method == "jwt" then
            (($vault[0].jwt // {})[$a.spec.mount // ""] // []) | map(select(.name == $a.spec.jwt.role)) | first
          else $vault[0].roles | map(select(.name == $a.spec.kubernetes.role)) | first end;
        # The namespaces a jwt role binds through the namespace claim.
        def claimed($r): [$r.bound_claims["/kubernetes.io/namespace"]] | flatten | map(select(. != null) | tostring);
        $vault[0] as $v
        | . as $objs
        | ($objs | map(select(.kind == "VaultAuth"))) as $auths
        | ($auths[]
           | . as $a
           | ($a.metadata.namespace // $ns) as $ans
           | role_of($a) as $role
           | if $remote != "" and $a.spec.method != "jwt" then
               "\($label): VaultAuth \($a.metadata.name) logs in with the \($a.spec.method // "") method, which the hub runs for its own cluster only: the apps of a remote instance log in with jwt on jwt-\($remote)"
             elif $remote != "" and $a.spec.mount != "jwt-\($remote)" then
               "\($label): VaultAuth \($a.metadata.name) logs in on the jwt mount \"\($a.spec.mount // "")\", not jwt-\($remote), the mount of this instance, whose keys are those of its cluster"
             elif $a.spec.method == "jwt" then
               ($a.spec.jwt // {}) as $j
               | "system:serviceaccount:\($ans):\($j.serviceAccount)" as $subject
               | if (($v.jwt // {}) | has($a.spec.mount // "")) | not then
                   "\($label): VaultAuth \($a.metadata.name) logs in on the jwt mount \"\($a.spec.mount // "")\", which Vault does not define (jwt mounts: \(($v.jwt // {}) | keys | join(", ")))"
                 elif $role == null then
                   "\($label): VaultAuth \($a.metadata.name) logs in with role \"\($j.role)\" of the jwt mount \"\($a.spec.mount)\", which Vault does not define"
                 elif (claimed($role) | index($ans)) == null then
                   "\($label): role \"\($role.name)\" of the jwt mount \($a.spec.mount) is not bound to the namespace \"\($ans)\" of VaultAuth \($a.metadata.name) (its namespace claim: \(claimed($role) | if . == [] then "none" else join(", ") end))"
                 elif $role.bound_subject != $subject then
                   "\($label): role \"\($role.name)\" of the jwt mount \($a.spec.mount) is bound to the subject \"\($role.bound_subject // "")\", not the service account \"\($subject)\" of VaultAuth \($a.metadata.name)"
                 elif ($j.audiences // []) == [] then
                   "\($label): VaultAuth \($a.metadata.name) requests no token audience, so VSO logs in with a Kubernetes API token: set jwt.audiences: [vault]"
                 elif ($role.bound_audiences // []) == [] then
                   "\($label): role \"\($role.name)\" of the jwt mount \($a.spec.mount) binds no audience, and Vault refuses a token that carries one, as every VSO token does: set bound_audiences"
                 elif any($j.audiences[]; . as $x | $role.bound_audiences | index($x) != null) | not then
                   "\($label): role \"\($role.name)\" of the jwt mount \($a.spec.mount) accepts only the audiences \"\($role.bound_audiences | join(", "))\", none of which VaultAuth \($a.metadata.name) requests (requests: \($j.audiences | join(", ")))"
                 else empty end
             elif $role == null then
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
               role_of($a) as $role
               | "\($s.spec.mount)/data/\($s.spec.path)" as $api
               | ($role.policies // $role.token_policies // []) as $policies
               | if $role != null and ([ $policies[] as $p | ($v.policies // [])[] | select(.name == $p) | .rules | select(reads($api)) ] | length) == 0 then
                   "\($label): role \"\($role.name)\" cannot read \($api) for \($s.metadata.name) (policies: \($policies | join(", ")))"
                 else empty end
             end)'
}

if [ "${1:-}" = "--stdin" ]; then
  violations=$(check_stream "${2:?label}" "${3:?namespace}" "${4:?vault.json}" "${5:-}")
  [ -z "$violations" ] || { printf '%s\n' "$violations"; exit 1; }
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
manager_err="$work/manager"

# scope_args <instance dir>: sets core_args and tenant_args, the --set-string
# arguments for the parameters the instance's manager gives every app of each
# scope. Returns 1, the manager's reason in $manager_err, when it does not
# render: the instance is then not checked against the charts' fallbacks.
scope_args() {
  local lines p
  core_args=() tenant_args=()
  lines=$(manager_parameters "$1" core 2>"$manager_err") || return 1
  while IFS= read -r p; do [ -n "$p" ] && core_args+=(--set-string "$p"); done <<<"$lines"
  lines=$(manager_parameters "$1" tenant 2>"$manager_err") || return 1
  while IFS= read -r p; do [ -n "$p" ] && tenant_args+=(--set-string "$p"); done <<<"$lines"
}

# vault_json <instance dir>: the description of the instance's Vault, from the
# vault-operator chart rendered with that instance's values and the parameters
# its own manager gives the core tier.
vault_json() {
  local vf="$1/values/core/vault-operator.yaml" args=() lines p
  [ -f "$vf" ] && args=(-f "$vf")
  lines=$(manager_parameters "$1" core 2>/dev/null) || return 1
  while IFS= read -r p; do [ -n "$p" ] && args+=(--set-string "$p"); done <<<"$lines"
  helm template check "$ROOT/argo-cd/apps/vault-operator" "${args[@]+"${args[@]}"}" 2>/dev/null \
    | yq ea -o=json -I=0 '[select(.kind == "Vault")][0].spec.externalConfig' - \
    | jq -c '{mounts: [.secrets[]? | select(.type == "kv") | .path],
        roles: [.auth[]? | select(.type == "kubernetes") | .roles[]?],
        jwt: ([.auth[]? | select(.type == "jwt") | {(.path // .type): (.roles // [])}] | add // {}),
        policies: (.policies // [])}'
}

# report <label> <namespace> [<remote instance>]: checks the manifests on stdin
# against the Vault description, one ok or FAIL line per violation.
report() {
  local violations
  if violations=$(check_stream "$1" "$2" "$vault" "${3:-}") && [ -z "$violations" ]; then
    echo "ok   $1"
  else
    printf '%s\n' "$violations" | sed 's/^/FAIL /'
    failures=$((failures + 1))
  fi
}

failures=0
vault="$work/vault.json"
for dir in "$INSTANCES_DIR"/*/; do
  inst="$(basename "$dir")"
  if ! scope_args "$dir"; then
    echo "FAIL $inst: the instance-manager does not render: $(cat "$manager_err")"
    failures=$((failures + 1))
    continue
  fi
  hub=$(yq '.vault.hub // ""' "$dir/instance.yaml")
  if [ -n "$hub" ]; then
    if [ ! -f "$INSTANCES_DIR/$hub/instance.yaml" ]; then
      echo "FAIL $inst: hub $hub is not an instance of $INSTANCES_DIR"
      failures=$((failures + 1))
      continue
    fi
    if ! vault_json "$INSTANCES_DIR/$hub" >"$vault" || ! jq -e '.roles | length > 0' "$vault" >/dev/null 2>&1; then
      echo "FAIL $inst: the vault-operator chart renders no Vault config with the values of its hub $hub"
      failures=$((failures + 1))
      continue
    fi
  elif ! vault_json "$dir" >"$vault" || ! jq -e '.roles | length > 0' "$vault" >/dev/null 2>&1; then
    echo "FAIL $inst: the vault-operator chart renders no Vault config with the values of this instance"
    failures=$((failures + 1))
    continue
  fi
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")

  if [ -n "$hub" ]; then
    for scope in core tenant; do
      [ -f "$dir/$scope.yaml" ] || continue
      lines=$(manager_parameters "$dir" "$scope" 2>/dev/null)
      while IFS= read -r entry; do
        [ -n "$entry" ] || continue
        label="$inst/$scope/$(jq -r '.app' <<<"$entry")"
        ns=$(jq -r --arg p "$prefix" --arg s "$suffix" '.namespace // "\($p)\(.app)\($s)"' <<<"$entry")
        vso_render "$dir" "$scope" "$entry" "$lines" "$ns" "$work/app" >"$work/why"
        case $? in
          0) report "$label" "$ns" "$inst" <"$work/app" ;;
          2) ;;
          *) echo "FAIL $label: does not render: $(cat "$work/why")"; failures=$((failures + 1)) ;;
        esac
      done < <(yq -o=json -I=0 '.apps[] | select(.enabled == "true" or .enabled == true)' "$dir/$scope.yaml")
    done
    continue
  fi

  for chart in "$ROOT/charts/ohmlab" "$ROOT"/argo-cd/apps/*/; do
    chart="${chart%/}"
    [ -f "$chart/Chart.yaml" ] || continue
    name="$(basename "$chart")"
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
    if [ "$scope" = core ]; then args+=("${core_args[@]+"${core_args[@]}"}"); else args+=("${tenant_args[@]+"${tenant_args[@]}"}"); fi
    if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
      helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL $inst/$name: helm dependency build"; failures=$((failures + 1)); continue; }
    fi
    if ! rendered=$(helm template "$release" "$chart" --namespace "$ns" "${args[@]+"${args[@]}"}" 2>/dev/null); then
      echo "FAIL $inst/$name: does not render (see the render step)"
      failures=$((failures + 1))
      continue
    fi
    report "$inst/$name" "$ns" <<<"$rendered"
  done
done

# An exit status is taken modulo 256.
exit $((failures > 255 ? 255 : failures))
