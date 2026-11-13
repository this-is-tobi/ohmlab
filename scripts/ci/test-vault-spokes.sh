#!/usr/bin/env bash
# Tests of the vault-operator chart's spoke clusters: the remote clusters whose
# instances read their secrets from this Vault. Each instance gets its own KV
# mount, a jwt auth mount jwt-<instance> with the cluster's keys (pinned, or
# from its issuer's discovery), and per app a read-only role bound to its own
# namespace and a policy on its own mount only, both derived from this Vault's
# kubernetes role and policies of that app. Renders that would hand a remote
# cluster more fail.
#
#   test-vault-spokes.sh
#
# Needs helm, yq (mikefarah v4) and jq. No render is printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/vault-operator"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
ok() { echo "ok   $*"; }

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/vault-operator.yaml" || { echo "no manager parameters for vault-operator" >&2; exit 1; }

# config [<helm args>...]: the Vault CR's externalConfig, as JSON, or nothing
# when the chart does not render (the error is kept in $work/err).
config() {
  helm template vault-operator "$CHART" --namespace vault-operator-system "${args[@]}" \
    --set ohmlab.controller.enabled=true "$@" 2>"$work/err" \
    | yq -o=json -I=0 'select(.kind == "Vault") | .spec.externalConfig'
}

# check <label> <jq filter that is true> <config JSON>
check() {
  if jq -e "$2" >/dev/null 2>&1 <<<"$3"; then ok "$1"; else fail "$1"; fi
}

# refuse <label> <message> <helm args>...: the render fails, saying message.
refuse() {
  local label="$1" message="$2"
  shift 2
  if [ -n "$(config "$@")" ]; then
    fail "$label: the chart rendered"
  elif ! grep -qF -- "$message" "$work/err"; then
    fail "$label: expected '$message', got: $(grep -m1 -i error "$work/err" | cut -c1-240)"
  else
    ok "$label"
  fi
}

cat >"$work/spokes.yaml" <<'YAML'
vault:
  externalConfig:
    spokeClusters:
    - name: nonprod
      issuer: https://oidc.example.com/id/nonprod
      oidcDiscoveryUrl: https://oidc.example.com/id/nonprod
      instances:
      - name: dev
        namespacePrefix: dev-
        apps: [gitea, {name: keycloak, namespace: dev-sso, serviceAccount: vso}]
      - name: preprod
        namespacePrefix: preprod-
        apps: [gitea]
    - name: prod
      publicKeys:
      - |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEnotarealkey
        -----END PUBLIC KEY-----
      instances:
      - name: prod
        mount: prod-secrets
        apps: [gitea]
YAML

base=$(config)
[ -n "$base" ] || { echo "the chart does not render without spoke clusters: $(grep -m1 -i error "$work/err")" >&2; exit 1; }
check "without spoke clusters, no jwt auth mount" '[.auth[] | select(.type == "jwt")] | length == 0' "$base"

got=$(config --values "$work/spokes.yaml")
[ -n "$got" ] || { fail "the chart does not render with spoke clusters: $(grep -m1 -i error "$work/err" | cut -c1-240)"; exit "$failures"; }

jwt() { printf '.auth[] | select(.type == "jwt" and .path == "jwt-%s")' "$1"; }
role() { printf '%s | .roles[] | select(.name == "%s")' "$(jwt "$1")" "$2"; }
policy() { printf '.policies[] | select(.name == "%s") | .rules' "$1"; }

check "one jwt mount per instance" '[.auth[] | select(.type == "jwt") | .path] == ["jwt-dev", "jwt-preprod", "jwt-prod"]' "$got"
check "one KV v2 mount per instance" '[.secrets[] | select(.type == "kv" and .options.version == 2) | .path] | contains(["dev", "preprod", "prod-secrets"])' "$got"
for inst in dev preprod; do
  check "$inst: role gitea is bound to its own namespace, with the core role's service account" \
    "[$(role "$inst" gitea)] | length == 1 and .[0].role_type == \"jwt\" and .[0].user_claim == \"sub\"
      and .[0].bound_subject == \"system:serviceaccount:$inst-gitea:vso\"
      and .[0].bound_claims[\"/kubernetes.io/namespace\"] == \"$inst-gitea\"
      and .[0].bound_audiences == [\"vault\"] and .[0].token_policies == [\"$inst-gitea\"]
      and .[0].token_ttl == \"1h\" and .[0].token_max_ttl == \"4h\"" "$got"
  check "$inst: policy $inst-gitea reads its own mount only: its paths and its claim's record, under platforms/+/" \
    "[$(policy "$inst-gitea") | scan(\"path \\\"([^\\\"]*)\\\"\") | .[0]] | sort == ([\"core/gitea\", \"tenant/gitea\", \"core/gitea/s3\", \"tenant/gitea/s3\"] | map(\"$inst/data/platforms/+/\" + .) | sort)" "$got"
done
check "an entry's namespace and service account override the derived ones" \
  "[$(role dev keycloak)] | length == 1 and .[0].bound_subject == \"system:serviceaccount:dev-sso:vso\" and .[0].bound_claims[\"/kubernetes.io/namespace\"] == \"dev-sso\"" "$got"
check "a mount follows the instance's mount setting" \
  "[$(policy prod-gitea) | scan(\"path \\\"([^\\\"]*)\\\"\") | .[0]] | all(startswith(\"prod-secrets/data/\"))" "$got"
check "oidcDiscoveryUrl gives the discovery URL and no pinned keys" \
  "$(jwt dev) | .config.oidc_discovery_url == \"https://oidc.example.com/id/nonprod\" and (.config | has(\"jwt_validation_pubkeys\") | not) and .config.bound_issuer == \"https://oidc.example.com/id/nonprod\"" "$got"
check "publicKeys gives the pinned keys and no discovery URL" \
  "$(jwt prod) | (.config.jwt_validation_pubkeys | length) == 1 and (.config | has(\"oidc_discovery_url\") | not)" "$got"
check "no spoke policy names this Vault's own mounts" \
  '[.policies[] | select(.name | test("^(dev|preprod|prod)-")) | .rules | test("\"(ohmlab|example)/")] | any | not' "$got"
check "the spoke policies only read" \
  '[.policies[] | select(.name | test("^(dev|preprod|prod)-")) | .rules | scan("capabilities *= *\\[([^]]*)\\]") | .[0] | scan("\"([^\"]*)\"") | .[0]] | unique == ["list", "read"]' "$got"
check "the controller writes every instance's mount" \
  '[.policies[] | select(.name == "ohmlab-controller-vault") | .rules | scan("path \"([^/]*)/data/") | .[0]] | contains(["ohmlab", "dev", "preprod", "prod-secrets"])' "$got"

# spoke <cluster JSON>: --set-json of spokeClusters with that one cluster.
spoke() { printf 'vault.externalConfig.spokeClusters=[%s]' "$1"; }
keys='"publicKeys":["-----BEGIN PUBLIC KEY-----\nMFkw\n-----END PUBLIC KEY-----\n"]'
disco='"oidcDiscoveryUrl":"https://oidc.example.com/id/c"'
refuse "both publicKeys and oidcDiscoveryUrl" "needs exactly one of oidcDiscoveryUrl and publicKeys" \
  --set-json "$(spoke "{\"name\":\"c\",$keys,$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"gitea\"]}]}")"
refuse "neither publicKeys nor oidcDiscoveryUrl" "needs exactly one of oidcDiscoveryUrl and publicKeys" \
  --set-json "$(spoke '{"name":"c","instances":[{"name":"dev","apps":["gitea"]}]}')"
refuse "an oidcDiscoveryUrl over plain HTTP" "oidcDiscoveryUrl must be https" \
  --set-json "$(spoke '{"name":"c","oidcDiscoveryUrl":"http://oidc.example.com/id/c","instances":[{"name":"dev","apps":["gitea"]}]}')"
# <instance JSON>|<what the refusal says>
for bad in '"name":"ohmlab"|ohmlab is a KV mount of this Vault'"'"'s own instance' \
  '"name":"example"|example is a KV mount of this Vault'"'"'s own instance' \
  '"name":"dev","mount":"ohmlab"|mount ohmlab overlaps ohmlab, a KV mount of this Vault'"'"'s own instance' \
  '"name":"dev","mount":"example"|mount example overlaps example, a KV mount of this Vault'"'"'s own instance' \
  '"name":"dev","mount":"ohmlab/dev"|mount ohmlab/dev overlaps ohmlab, a KV mount of this Vault'"'"'s own instance' \
  '"name":"sys"|sys is reserved by Vault' '"name":"dev","mount":"sys/kv"|sys/kv is reserved by Vault' \
  '"name":"audit"|audit is reserved by Vault'; do
  refuse "an instance {${bad%%|*}}" "${bad#*|}" \
    --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{${bad%%|*},\"apps\":[\"gitea\"]}]}")"
done
refuse "one instance name twice" "spoke instance dev is declared twice" \
  --set-json "vault.externalConfig.spokeClusters=[{\"name\":\"a\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"gitea\"]}]},{\"name\":\"b\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"gitea\"]}]}]"
refuse "one mount twice" "mount dev overlaps dev, the mount of spoke instance dev" \
  --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"gitea\"]},{\"name\":\"qa\",\"mount\":\"dev\",\"apps\":[\"gitea\"]}]}")"
refuse "a mount inside another spoke instance's" "mount dev/qa overlaps dev, the mount of spoke instance dev" \
  --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"gitea\"]},{\"name\":\"qa\",\"mount\":\"dev/qa\",\"apps\":[\"gitea\"]}]}")"
refuse "a mount that is a secrets engine declared with a trailing slash" "mount pki overlaps pki, a secrets engine" \
  --set-json 'vault.externalConfig.secrets=[{"path":"pki/","type":"pki"}]' \
  --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"pki\",\"apps\":[\"gitea\"]}]}")"
refuse "two instances of one cluster on one namespace" "namespace dev-gitea is already bound by spoke instance dev" \
  --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"namespacePrefix\":\"dev-\",\"apps\":[\"gitea\"]},{\"name\":\"preprod\",\"apps\":[{\"name\":\"gitea\",\"namespace\":\"dev-gitea\"}]}]}")"
refuse "an app with no kubernetes role here" "app nope has no kubernetes role" \
  --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"nope\"]}]}")"

cat >"$work/roles.yaml" <<'YAML'
vault:
  externalConfig:
    policies:
    - name: two
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/two" {
          capabilities = ["read"]
        }
    - name: writer
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/writer" {
          capabilities = ["read", "create"]
        }
    - name: wide
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/wide" {
          capabilities = ["read"]
        }
        path "{{ .systemMount }}/data/platforms/+/core/wide" {
          capabilities = ["read"]
        }
        path "secret/data/wide" {
          capabilities = ["read"]
        }
    - name: legacy
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/legacy" {
          policy = "write"
        }
    - name: json
      rules: |
        {"path": {"{{ .kvMount }}/data/platforms/+/tenant/json": {"capabilities": ["create", "read"]}}}
    - name: denier
      rules: |
        # Comment lines are allowed.
        path "{{ .kvMount }}/data/platforms/+/tenant/denier" {
          capabilities = ["read", "list"]
        }
        path "{{ .kvMount }}/data/platforms/+/tenant/denier/private" {
          capabilities = ["deny"]
        }
    # Texts that once passed the read-only checks, each confirmed against Vault.
    - name: comment
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/comment" {
          capabilities = ["read", "list"]
        }
        /* xpath "{{ .kvMount }}/x" path "{{ .kvMount }}/ */ path "ohmlab/*" { capabilities = ["read", "list"] }
    - name: escaped
      rules: |
        {"path": {"*": {"capabilities": ["create", "read", "update", "delete", "list", "sudo"]}}}
    - name: heredoc
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/heredoc" {
          capabilities = [<<EOT
        create
        EOT
        , "read"]
        }
    - name: templated
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/templated" {
          capabilities = ["read"]
        }
        ${ printf `%sth "ohmlab/*" { capa%s = ["read", "list"] }` `pa` `bilities` }
    - name: quoted
      rules: |
        path "{{ .kvMount }}/data/platforms/+/tenant/quoted" {
          "policy" = "sudo"
        }
    - name: hash
      rules: |
        path "{{ .kvMount }}/x#" { capabilities = ["read"] } path "ohmlab/*" { capabilities = ["read", "list"] }
        /* xpath "{{ .kvMount }}/z" */ path "{{ .kvMount }}/data/platforms/+/tenant/hash" { capabilities = ["read"] }
    auth:
    - type: kubernetes
      roles:
      - {name: two, bound_service_account_names: [vso], bound_service_account_namespaces: [a, b], policies: [two]}
      - {name: writer, bound_service_account_names: [vso], bound_service_account_namespaces: [writer], policies: [writer]}
      - {name: wide, bound_service_account_names: [vso], bound_service_account_namespaces: [wide], policies: [wide]}
      - {name: legacy, bound_service_account_names: [vso], bound_service_account_namespaces: [legacy], policies: [legacy]}
      - {name: json, bound_service_account_names: [vso], bound_service_account_namespaces: [json], policies: [json]}
      - {name: denier, bound_service_account_names: [vso], bound_service_account_namespaces: [denier], policies: [denier]}
      - {name: tokenpolicies, bound_service_account_names: [vso], bound_service_account_namespaces: [tp], token_policies: [two]}
      - {name: nopolicy, bound_service_account_names: [vso], bound_service_account_namespaces: [np]}
      - {name: comment, bound_service_account_names: [vso], bound_service_account_namespaces: [comment], policies: [comment]}
      - {name: escaped, bound_service_account_names: [vso], bound_service_account_namespaces: [escaped], policies: [escaped]}
      - {name: heredoc, bound_service_account_names: [vso], bound_service_account_namespaces: [heredoc], policies: [heredoc]}
      - {name: templated, bound_service_account_names: [vso], bound_service_account_namespaces: [templated], policies: [templated]}
      - {name: quoted, bound_service_account_names: [vso], bound_service_account_namespaces: [quoted], policies: [quoted]}
      - {name: hash, bound_service_account_names: [vso], bound_service_account_namespaces: [hash], policies: [hash]}
    - type: jwt
      path: jwt-taken
      roles:
      - {name: ci, role_type: jwt, user_claim: sub, bound_audiences: [vault], token_policies: [two]}
YAML
refuse "an app whose core role binds two namespaces, with none on its entry" "not one namespace" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"two\"]}]}")"
check "the same app with its namespace on its entry renders" '[.auth[] | select(.type == "jwt" and .path == "jwt-dev")] | length == 1' \
  "$(config --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[{\"name\":\"two\",\"namespace\":\"dev-a\"}]}]}")")"
refuse "a policy that would let a remote cluster create" "a remote cluster only reads" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"writer\"]}]}")"
refuse "a policy with a path outside the instance's mount" "outside the mount dev" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"wide\"]}]}")"
# The guards read HCL capabilities: a policy in another syntax, which they
# could not check, is refused rather than handed over.
refuse "a policy in the legacy policy = syntax" "only HCL" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"legacy\"]}]}")"
refuse "a policy written as JSON" "only HCL" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"json\"]}]}")"
for trick in comment escaped heredoc templated quoted hash; do
  refuse "a policy that hides more than reads of its own mount ($trick)" "policy dev-$trick" \
    --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"$trick\"]}]}")"
done
check "deny stanzas and comment lines are allowed" '[.policies[] | select(.name == "dev-denier")] | length == 1' \
  "$(config --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"denier\"]}]}")")"
check "a core role that lists token_policies derives the same rules" \
  '[.policies[] | select(.name == "dev-tokenpolicies") | .rules | test("dev/data/platforms/\\+/tenant/two")] == [true]' \
  "$(config --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[{\"name\":\"tokenpolicies\",\"namespace\":\"dev-tp\"}]}]}")")"
refuse "a core role that lists no policy" "its kubernetes role lists no policy" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"dev\",\"apps\":[\"nopolicy\"]}]}")"
refuse "a jwt mount an auth entry here already uses" "jwt-taken is already an auth mount" \
  --values "$work/roles.yaml" --set-json "$(spoke "{\"name\":\"c\",$disco,\"instances\":[{\"name\":\"taken\",\"apps\":[{\"name\":\"two\",\"namespace\":\"t-a\"}]}]}")"

# The hub seeds the spoke instances' paths too. A seed's name drops the mount
# only when it is one of this Vault's own, so two instances with one env do not
# collide, and the names of the hub's own seeds stay.
# seeds <helm args>...: "<VaultSeed name> <path>" per seed, sorted.
seeds() {
  helm template vault-operator "$CHART" --namespace vault-operator-system "${args[@]}" \
    --set ohmlab.vault.seeds.enabled=true --set ohmlab.vault.seeds.requireCRD=false "$@" 2>"$work/err" \
    | yq -r 'select(.kind == "VaultSeed") | .metadata.name + " " + .spec.path' | { grep -v '^---$' || true; } | sort
}
kv=$(printf '%s\n' "${args[@]}" | sed -n 's/^global\.ohmlab\.names\.vaultMount=//p')
[ -n "$kv" ] || fail "the manager gives no global.ohmlab.names.vaultMount"
cat >"$work/seeds.yaml" <<YAML
ohmlab:
  vault:
    secrets:
    - {path: dev/platforms/staging/tenant/gitea, data: {k: ""}}
    - {path: preprod/platforms/staging/tenant/gitea, data: {k: ""}}
    - {path: ohmlab/platforms/production/core/keycloak, data: {k: ""}}
    - {path: $kv/platforms/production/tenant/gitea, data: {k: ""}}
    - {path: kv2/platforms/production/tenant/rustfs, data: {k: ""}}
YAML
got=$(seeds --values "$work/seeds.yaml" --set ohmlab.controller.kvMount=kv2)
want="dev-staging-tenant-gitea dev/platforms/staging/tenant/gitea
preprod-staging-tenant-gitea preprod/platforms/staging/tenant/gitea
production-core-keycloak ohmlab/platforms/production/core/keycloak
production-tenant-gitea $kv/platforms/production/tenant/gitea
production-tenant-rustfs kv2/platforms/production/tenant/rustfs"
if [ "$got" = "$want" ]; then
  ok "a seed of another mount keeps it in its name; the system, instance and controller mounts' seeds keep theirs"
else
  fail "seed names: expected '$want', got '$got' $(grep -m1 -i error "$work/err" | cut -c1-200)"
fi

[ "$failures" -eq 0 ] && echo "ok   vault-operator spoke clusters: per instance mounts, read-only roles and policies"
exit "$failures"
