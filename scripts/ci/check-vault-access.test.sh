#!/usr/bin/env bash
# Decision table for scripts/ci/check-vault-access.sh on hand-written manifests
# and a hand-written Vault description.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-vault-access.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

cat >"$work/vault.json" <<'JSON'
{
  "mounts": ["kv", "dev"],
  "roles": [
    {"name": "app", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["app"], "audience": "vault"},
    {"name": "any", "bound_service_account_names": ["*"], "bound_service_account_namespaces": ["*"], "policies": ["app"], "audience": "vault"},
    {"name": "nopolicy", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["gone"], "audience": "vault"},
    {"name": "listonly", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["list"], "audience": "vault"},
    {"name": "commented", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["commented"], "audience": "vault"},
    {"name": "glob", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["glob"], "audience": "vault"},
    {"name": "dotted", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["dotted"], "audience": "vault"},
    {"name": "open", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "policies": ["app"]},
    {"name": "tokpol", "bound_service_account_names": ["vso"], "bound_service_account_namespaces": ["app-ns"], "token_policies": ["app"], "audience": "vault"}
  ],
  "jwt": {
    "jwt-dev": [
      {"name": "app", "role_type": "jwt", "bound_audiences": ["vault"], "bound_subject": "system:serviceaccount:app-ns:vso", "bound_claims": {"/kubernetes.io/namespace": "app-ns"}, "token_policies": ["dev-app"]},
      {"name": "listonly", "role_type": "jwt", "bound_audiences": ["vault"], "bound_subject": "system:serviceaccount:app-ns:vso", "bound_claims": {"/kubernetes.io/namespace": "app-ns"}, "token_policies": ["dev-list"]},
      {"name": "otheraud", "role_type": "jwt", "bound_audiences": ["other"], "bound_subject": "system:serviceaccount:app-ns:vso", "bound_claims": {"/kubernetes.io/namespace": "app-ns"}, "token_policies": ["dev-app"]},
      {"name": "noaud", "role_type": "jwt", "bound_subject": "system:serviceaccount:app-ns:vso", "bound_claims": {"/kubernetes.io/namespace": "app-ns"}, "token_policies": ["dev-app"]}
    ],
    "jwt-prod": [
      {"name": "app", "role_type": "jwt", "bound_audiences": ["vault"], "bound_subject": "system:serviceaccount:prod-ns:vso", "bound_claims": {"/kubernetes.io/namespace": "prod-ns"}, "token_policies": ["prod-app"]}
    ]
  },
  "policies": [
    {"name": "app", "rules": "# path \"kv/data/platforms/+/+/other\" { capabilities = [\"read\"] }\npath \"kv/data/platforms/+/+/app\" {\n  capabilities = [\"read\", \"list\"]\n}"},
    {"name": "list", "rules": "path \"kv/data/platforms/+/+/app\" {\n  capabilities = [\"list\"]\n}"},
    {"name": "commented", "rules": "# path \"kv/data/platforms/+/+/app\" {\n#   capabilities = [\"read\"]\n# }"},
    {"name": "glob", "rules": "path \"kv/data/platforms/*\" {\n  capabilities = [\"read\"]\n}"},
    {"name": "dotted", "rules": "path \"kv/data/a.b\" {\n  capabilities = [\"read\"]\n}"},
    {"name": "dev-app", "rules": "path \"dev/data/platforms/+/tenant/app\" {\n  capabilities = [\"read\", \"list\"]\n}"},
    {"name": "dev-list", "rules": "path \"dev/data/platforms/+/tenant/app\" {\n  capabilities = [\"list\"]\n}"}
  ]
}
JSON

# auth <name> <role> <service account> [<namespace>] [<audiences>] -> one
# VaultAuth; the audiences default to vault, an empty value requests none.
auth() {
  printf -- '---\napiVersion: secrets.hashicorp.com/v1beta1\nkind: VaultAuth\nmetadata:\n  name: %s\n' "$1"
  [ -n "${4:-}" ] && printf '  namespace: %s\n' "$4"
  printf 'spec:\n  method: kubernetes\n  mount: kubernetes\n  kubernetes:\n    role: %s\n    serviceAccount: %s\n' "$2" "$3"
  [ -n "${5-vault}" ] && printf '    audiences: [%s]\n' "${5-vault}"
  return 0
}

# jwtauth <name> <role> <service account> [<mount>] [<namespace>] [<audiences>]
# -> one VaultAuth of the jwt method, on jwt-dev by default; the audiences
# default to vault, an empty value requests none.
jwtauth() {
  printf -- '---\napiVersion: secrets.hashicorp.com/v1beta1\nkind: VaultAuth\nmetadata:\n  name: %s\n' "$1"
  [ -n "${5:-}" ] && printf '  namespace: %s\n' "$5"
  printf 'spec:\n  method: jwt\n  mount: %s\n  jwt:\n    role: %s\n    serviceAccount: %s\n' "${4:-jwt-dev}" "$2" "$3"
  [ -n "${6-vault}" ] && printf '    audiences: [%s]\n' "${6-vault}"
  return 0
}

# vss <name> <VaultAuth name> <mount> <path> -> one VaultStaticSecret
vss() {
  printf -- '---\napiVersion: secrets.hashicorp.com/v1beta1\nkind: VaultStaticSecret\nmetadata:\n  name: %s\nspec:\n  vaultAuthRef: %s\n  mount: %s\n  path: %s\n  type: kv-v2\n' "$1" "$2" "$3" "$4"
}

# check <label> <expected exit> <namespace> <manifests> [<expected text in the output>] [<remote instance>]
check() {
  local out rc
  out=$(printf '%s\n' "$4" | "$SCRIPT" --stdin t "$3" "$work/vault.json" ${6:+"$6"} 2>&1); rc=$?
  if [ "$rc" != "$2" ]; then
    echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${5:-}" ] && ! grep -qF -- "$5" <<<"$out"; then
    echo "FAIL $1: the output does not say: $5"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $1"
  fi
}

app=platforms/production/core/app

check "a role bound to the service account and namespace that can read the path passes" 0 app-ns "$(auth a app vso; vss s a kv $app)"
check "no VSO object passes" 0 app-ns "kind: ConfigMap"
check "a VaultAuth alone passes when its role exists" 0 app-ns "$(auth a app vso)"
check "a role that Vault does not define is refused" 1 app-ns "$(auth a ghost vso)" 'role "ghost", which Vault does not define'
check "a service account the role is not bound to is refused" 1 app-ns "$(auth a app default)" 'not bound to the service account "default"'
check "a namespace the role is not bound to is refused" 1 other-ns "$(auth a app vso)" 'not bound to the namespace "other-ns"'
check "the namespace of the VaultAuth is the one checked" 1 app-ns "$(auth a app vso other-ns)" 'not bound to the namespace "other-ns"'
check "a VaultAuth in the bound namespace passes whatever the release namespace" 0 other-ns "$(auth a app vso app-ns; vss s a kv $app)"
check "a wildcard role passes in any namespace" 0 whatever "$(auth a any vso; vss s a kv $app)"
check "a mount that is not a KV mount of Vault is refused" 1 app-ns "$(auth a app vso; vss s a example $app)" 'mount "example", which is not a KV mount'
check "a path the policy does not grant is refused" 1 app-ns "$(auth a app vso; vss s a kv platforms/production/core/other)" 'cannot read kv/data/platforms/production/core/other'
check "a '+' stands for one path segment only" 1 app-ns "$(auth a app vso; vss s a kv platforms/production/core/extra/app)" 'cannot read'
check "a role whose policy is not defined cannot read" 1 app-ns "$(auth a nopolicy vso; vss s a kv $app)" 'cannot read'
check "a policy that only lists cannot read" 1 app-ns "$(auth a listonly vso; vss s a kv $app)" 'cannot read'
check "a commented-out policy path grants nothing" 1 app-ns "$(auth a commented vso; vss s a kv $app)" 'cannot read'
check "a trailing glob grants the whole subtree" 0 app-ns "$(auth a glob vso; vss s a kv $app)"
check "a dot in a policy path is a dot" 1 app-ns "$(auth a dotted vso; vss s a kv aXb)" 'cannot read'
check "a VaultStaticSecret whose VaultAuth is not rendered is refused" 1 app-ns "$(vss s missing kv $app)" 'references VaultAuth "missing"'
check "a VaultAuth that requests no audience is refused" 1 app-ns "$(auth a app vso '' '')" 'requests no token audience'
check "a role that requires no audience is refused" 1 app-ns "$(auth a open vso; vss s a kv $app)" 'requires no token audience'
check "a role that requires an audience refuses a VaultAuth that requests another" 1 app-ns "$(auth a app vso '' other)" 'accepts only the audience "vault"'
check "one bad secret among good ones is refused" 1 app-ns "$(auth a app vso; vss ok a kv $app; vss bad a kv platforms/production/core/other)" 'bad'
check "a role's token_policies are its policies" 0 app-ns "$(auth a tokpol vso; vss s a kv $app)"

# The jwt method, as a remote instance's apps log in to their hub's Vault.
devapp=platforms/dev/tenant/app
check "a jwt role bound to the namespace and service account that can read the path passes" 0 app-ns "$(jwtauth a app vso; vss s a dev $devapp)"
check "a jwt role that its mount does not define is refused" 1 app-ns "$(jwtauth a ghost vso)" 'role "ghost" of the jwt mount "jwt-dev", which Vault does not define'
check "a jwt mount that Vault does not define is refused" 1 app-ns "$(jwtauth a app vso jwt-ghost)" 'the jwt mount "jwt-ghost", which Vault does not define (jwt mounts: jwt-dev, jwt-prod)'
check "the jwt role of another instance's mount is refused" 1 app-ns "$(jwtauth a app vso jwt-prod)" 'not bound to the namespace "app-ns"'
check "a jwt role bound to another namespace is refused" 1 other-ns "$(jwtauth a app vso)" 'not bound to the namespace "other-ns"'
check "the namespace of a jwt VaultAuth is the one checked" 0 other-ns "$(jwtauth a app vso jwt-dev app-ns)"
check "a jwt role bound to another service account is refused" 1 app-ns "$(jwtauth a app default)" 'not the service account "system:serviceaccount:app-ns:default"'
check "a jwt VaultAuth that requests no audience is refused" 1 app-ns "$(jwtauth a app vso jwt-dev '' '')" 'requests no token audience'
check "a jwt role that binds no audience is refused" 1 app-ns "$(jwtauth a noaud vso)" 'binds no audience'
check "a jwt role that shares no audience with its VaultAuth is refused" 1 app-ns "$(jwtauth a otheraud vso)" 'accepts only the audiences "other", none of which VaultAuth a requests'
check "a jwt role whose policy only lists cannot read" 1 app-ns "$(jwtauth a listonly vso; vss s a dev $devapp)" 'cannot read dev/data/platforms/dev/tenant/app'
check "a jwt role cannot read the path on another mount" 1 app-ns "$(jwtauth a app vso; vss s a kv $devapp)" 'cannot read kv/data/platforms/dev/tenant/app'
check "a remote instance's VaultAuth on its own jwt mount passes" 0 app-ns "$(jwtauth a app vso; vss s a dev $devapp)" '' dev
check "a remote instance's kubernetes VaultAuth is refused" 1 app-ns "$(auth a app vso)" 'logs in with the kubernetes method, which the hub runs for its own cluster only' dev
check "a remote instance's VaultAuth on another instance's jwt mount is refused" 1 app-ns "$(jwtauth a app vso jwt-prod)" 'not jwt-dev, the mount of this instance' dev

exit "$fail"
