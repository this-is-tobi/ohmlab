#!/usr/bin/env bash
# The instance checks read the instances from INSTANCES_DIR, so that an
# instance kept in a repository of its own is checked by the same scripts.
# Unit cases for scripts/lib/instances.sh, then the Vault checks run on a copy
# of the instances: a copy that is broken only where INSTANCES_DIR points must
# fail, which a script reading this repository's own folder would not.
# Needs helm, yq, jq and go.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

# --- the helper ---------------------------------------------------------------
other="$work/other/argo-cd/instances"
mkdir -p "$other/mylab/values/core" "$other/_tmpl/values/core"
touch "$other/mylab/values/core/foo.yaml" "$other/_tmpl/values/core/foo.yaml"

lookup() { # lookup <INSTANCES_DIR or empty> <function> [args...]
  local dir="$1"
  shift
  ( unset INSTANCES_DIR; [ -n "$dir" ] && export INSTANCES_DIR="$dir"
    # shellcheck source=scripts/lib/instances.sh
    source "$ROOT/scripts/lib/instances.sh" && "$@" ) 2>&1
}

check "INSTANCES_DIR defaults to this repository's instances" \
  "$ROOT/argo-cd/instances" "$(lookup "" eval 'printf %s "$INSTANCES_DIR"')"
check "instance_dirs lists the real instances only" "$other/mylab" "$(lookup "$other" instance_dirs)"
check "values_file prefers a real instance of INSTANCES_DIR" \
  "$other/mylab/values/core/foo.yaml" "$(lookup "$other" values_file foo)"
check "values_file falls back to this repository's _example" \
  "$ROOT/argo-cd/instances/_example/values/core/vault-operator.yaml" "$(lookup "$other" values_file vault-operator)"
check "values_file prints nothing when no instance has the app" "" "$(lookup "$other" values_file no-such-app)"

# --- the scripts --------------------------------------------------------------
copy="$work/copy"
cp -R "$ROOT/argo-cd/instances" "$copy"

runs() { # runs <INSTANCES_DIR or empty> <script> -> its exit code
  if [ -n "$1" ]; then INSTANCES_DIR="$1" "$ROOT/scripts/ci/$2" >"$work/out" 2>&1; else
    (unset INSTANCES_DIR; "$ROOT/scripts/ci/$2") >"$work/out" 2>&1; fi
  echo $?
}

for script in check-vault-access.sh check-vault-seeds.sh; do
  check "$script gives the same result on a faithful copy" "$(runs "" "$script")" "$(runs "$copy" "$script")"
done

broken="$work/broken"
cp -R "$ROOT/argo-cd/instances" "$broken"
mkdir -p "$broken/novault/values/core"
printf 'instance: {name: novault}\nrepoURL: https://git.example/x.git\n' >"$broken/novault/instance.yaml"
printf 'vault:\n  enabled: false\n' >"$broken/novault/values/core/vault-operator.yaml"
check "check-vault-access.sh reads the instances of INSTANCES_DIR" 1 "$(runs "$broken" check-vault-access.sh)"
check "an instance without vault.hub whose chart renders no Vault fails" yes \
  "$(grep -qF 'FAIL novault: the vault-operator chart renders no Vault config' "$work/out" && echo yes)"

# A chart of a local instance that cannot log in fails the check.
solo="$work/solo"
mkdir -p "$solo/solo/values/tenant"
printf 'instance: {name: solo, env: production}\nrepoURL: https://git.example/x.git\n' >"$solo/solo/instance.yaml"
printf 'apps:\n- app: vault-operator\n  enabled: "true"\n  namespace: vault-operator-system\n' >"$solo/solo/core.yaml"
printf 'apps:\n- app: gitea\n  enabled: "true"\n' >"$solo/solo/tenant.yaml"
cp "$ROOT/argo-cd/instances/_example/values/tenant/gitea.yaml" "$solo/solo/values/tenant/"
yq -i '.vso.vaultAuth.default.login.role = "ghost"' "$solo/solo/values/tenant/gitea.yaml"
rc=$(runs "$solo" check-vault-access.sh)
if [ "$rc" = 1 ] && grep -qF 'FAIL solo/gitea: VaultAuth gitea-vso-default logs in with role "ghost", which Vault does not define' "$work/out"; then
  echo "ok   check-vault-access.sh counts a local chart that cannot log in"
else
  echo "FAIL check-vault-access.sh on a local chart that cannot log in (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

# A remote instance (vault.hub) has no Vault of its own: its apps are checked
# against its hub's, where they log in with the jwt method on jwt-<instance>.
hubs="$work/hubs"
mkdir -p "$hubs/hub/values/core" "$hubs/dev/values/core" "$hubs/dev/values/tenant"
printf 'instance: {name: hub, env: production}\nrepoURL: https://git.example/x.git\n' >"$hubs/hub/instance.yaml"
printf 'apps:\n- app: vault-operator\n  enabled: "true"\n  namespace: vault-operator-system\n' >"$hubs/hub/core.yaml"
cat >"$hubs/hub/values/core/vault-operator.yaml" <<'YAML'
vault:
  externalConfig:
    spokeClusters:
    - name: nonprod
      oidcDiscoveryUrl: https://oidc.example.com/id/nonprod
      instances:
      - name: dev
        apps: [gitea]
YAML
printf 'instance: {name: dev, env: dev}\nrepoURL: https://git.example/x.git\nvault: {hub: hub}\n' >"$hubs/dev/instance.yaml"
printf 'apps:\n- app: vault-operator\n  enabled: "true"\n  namespace: vault-operator-system\n' >"$hubs/dev/core.yaml"
printf 'vault-operator:\n  enabled: false\nvault:\n  enabled: false\nvaultOidcCredentials:\n  enabled: false\n' >"$hubs/dev/values/core/vault-operator.yaml"
printf 'apps:\n- app: gitea\n  enabled: "true"\n' >"$hubs/dev/tenant.yaml"
cp "$ROOT/argo-cd/instances/_example/values/tenant/gitea.yaml" "$hubs/dev/values/tenant/"
rc=$(runs "$hubs" check-vault-access.sh)
if [ "$rc" = 0 ] && grep -qx 'ok   dev/tenant/gitea' "$work/out"; then
  echo "ok   check-vault-access.sh checks a remote instance's apps against its hub's Vault"
else
  echo "FAIL check-vault-access.sh on a remote instance and its hub (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

# VSO alone takes three switches: the one left on logs in to the hub with the
# kubernetes method, which the hub runs for its own cluster only.
half="$work/half"
cp -R "$hubs" "$half"
printf 'vault-operator:\n  enabled: false\nvault:\n  enabled: false\n' >"$half/dev/values/core/vault-operator.yaml"
rc=$(runs "$half" check-vault-access.sh)
if [ "$rc" != 0 ] && grep -qF 'FAIL dev/core/vault-operator: VaultAuth vault-operator-vso-default logs in with the kubernetes method' "$work/out"; then
  echo "ok   check-vault-access.sh refuses a remote instance's VaultAuth of the kubernetes method"
else
  echo "FAIL check-vault-access.sh on a remote instance that keeps vaultOidcCredentials (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

# The namespace checked is the one the manager deploys to: the prefix applies
# to an entry's default namespace on both sides, never to its own namespace.
prefixed="$work/prefixed"
cp -R "$hubs" "$prefixed"
printf 'instance: {name: dev, env: dev}\nrepoURL: https://git.example/x.git\nvault: {hub: hub}\nnamespacePrefix: dev-\n' >"$prefixed/dev/instance.yaml"
yq -i '.vault.externalConfig.spokeClusters[0].instances[0].namespacePrefix = "dev-"' "$prefixed/hub/values/core/vault-operator.yaml"
rc=$(runs "$prefixed" check-vault-access.sh)
if [ "$rc" = 0 ] && grep -qx 'ok   dev/tenant/gitea' "$work/out"; then
  echo "ok   check-vault-access.sh passes a remote instance's prefixed namespace that its hub's role binds"
else
  echo "FAIL check-vault-access.sh on a prefixed remote instance (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi
printf 'apps:\n- app: gitea\n  enabled: "true"\n  namespace: code\n' >"$prefixed/dev/tenant.yaml"
rc=$(runs "$prefixed" check-vault-access.sh)
if [ "$rc" != 0 ] && grep -qF 'FAIL dev/tenant/gitea: role "gitea" of the jwt mount jwt-dev is not bound to the namespace "code" of VaultAuth gitea-vso-default (its namespace claim: dev-gitea)' "$work/out"; then
  echo "ok   check-vault-access.sh: a role bound to the prefixed default namespace refuses an entry with its own namespace"
else
  echo "FAIL check-vault-access.sh on an entry with its own namespace (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

# Every enabled entry of a remote instance logs in to the hub, and only those: a
# disabled entry of the same chart hides nothing, a disabled app is not checked.
shadow="$work/shadow"
cp -R "$hubs" "$shadow"
printf 'apps:\n- app: vault-operator\n  enabled: "true"\n  namespace: vault-operator-system\n- app: gitea\n  enabled: "false"\n' >"$shadow/dev/core.yaml"
printf 'apps:\n- app: gitea\n  enabled: "true"\n  namespace: code\n- app: mattermost\n  enabled: "false"\n' >"$shadow/dev/tenant.yaml"
rc=$(runs "$shadow" check-vault-access.sh)
if [ "$rc" != 0 ] && grep -qF 'FAIL dev/tenant/gitea: role "gitea" of the jwt mount jwt-dev is not bound to the namespace "code"' "$work/out"; then
  echo "ok   check-vault-access.sh: a disabled entry does not hide the enabled entry of its chart on a remote instance"
else
  echo "FAIL check-vault-access.sh on a remote instance with a disabled entry of an enabled chart (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi
if grep -q 'mattermost' "$work/out"; then
  echo "FAIL check-vault-access.sh checks a disabled app of a remote instance"; sed 's/^/     /' "$work/out" | head -8; fail=1
else
  echo "ok   check-vault-access.sh does not check a disabled app of a remote instance"
fi

nospoke="$work/nospoke"
cp -R "$hubs" "$nospoke"
printf 'vault:\n  externalConfig:\n    spokeClusters: []\n' >"$nospoke/hub/values/core/vault-operator.yaml"
rc=$(runs "$nospoke" check-vault-access.sh)
if [ "$rc" != 0 ] && grep -qF 'FAIL dev/tenant/gitea: VaultAuth gitea-vso-default logs in on the jwt mount "jwt-dev", which Vault does not define' "$work/out"; then
  echo "ok   check-vault-access.sh refuses a remote instance its hub declares no spoke for"
else
  echo "FAIL check-vault-access.sh on a hub without the instance's spoke (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

rm -rf "$nospoke/hub"
rc=$(runs "$nospoke" check-vault-access.sh)
if [ "$rc" != 0 ] && grep -qxF "FAIL dev: hub hub is not an instance of $nospoke" "$work/out"; then
  echo "ok   check-vault-access.sh refuses a remote instance whose hub is not an instance of INSTANCES_DIR"
else
  echo "FAIL check-vault-access.sh on a remote instance without its hub (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

broken="$work/broken-seed"
cp -R "$ROOT/argo-cd/instances" "$broken"
mkdir -p "$broken/badseed/values/core"
printf 'instance: {name: badseed}\nrepoURL: https://git.example/x.git\n' >"$broken/badseed/instance.yaml"
printf 'ohmlab:\n  vault:\n    secrets:\n    - data:\n        key: "<random:8>"\n' >"$broken/badseed/values/core/vault-operator.yaml"
check "check-vault-seeds.sh reads the instances of INSTANCES_DIR" 1 "$(runs "$broken" check-vault-seeds.sh)"

# The hub seeds what the remote instance's apps read, on the remote's mount.
rc=$(runs "$hubs" check-vault-seeds.sh)
if [ "$rc" != 0 ] && grep -qxF 'FAIL dev/tenant/gitea: dev/platforms/dev/tenant/gitea key valkey.password is not seeded by hub hub' "$work/out"; then
  echo "ok   check-vault-seeds.sh refuses a key a remote app reads that its hub does not seed"
else
  echo "FAIL check-vault-seeds.sh on a hub without seeds (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi
seeded="$work/seeded"
cp -R "$hubs" "$seeded"
cat >>"$seeded/hub/values/core/vault-operator.yaml" <<'YAML'
ohmlab:
  vault:
    secrets:
    - path: dev/platforms/dev/tenant/gitea
      data:
        admin: {username: gitea, password: "<random:32>"}
        keycloak: {clientId: gitea, clientSecret: ""}
        postgres:
          admin: {username: postgres, password: "<random:32>"}
          app: {username: gitea, password: "<random:32>"}
          database: gitea
          host: gitea-pg-rw
          port: "5432"
          s3: {accessKey: "", bucketName: "", bucketPrefix: "", endpoint: "", region: "", secretKey: ""}
        valkey: {password: "<random:32>"}
YAML
rc=$(runs "$seeded" check-vault-seeds.sh)
if [ "$rc" = 0 ] && grep -qxF 'ok   dev: 18 key(s) read, all seeded by hub' "$work/out"; then
  echo "ok   check-vault-seeds.sh passes a remote instance whose hub seeds every key its apps read"
else
  echo "FAIL check-vault-seeds.sh on a hub that seeds every key (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi
printf 'apps:\n- app: gitea\n  enabled: "true"\n- app: wiki\n  chart: nosuchchart\n  enabled: "true"\n' >"$seeded/dev/tenant.yaml"
rc=$(runs "$seeded" check-vault-seeds.sh)
if [ "$rc" != 0 ] && grep -qF 'FAIL dev/tenant/wiki: does not render' "$work/out" && ! grep -q '^ok   dev: .* key(s) read' "$work/out"; then
  echo "ok   check-vault-seeds.sh fails a remote app that does not render, with no ok line for its instance"
else
  echo "FAIL check-vault-seeds.sh on a remote app that does not render (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi
rm -rf "$seeded/hub"
rc=$(runs "$seeded" check-vault-seeds.sh)
if [ "$rc" != 0 ] && grep -qxF "FAIL dev: hub hub is not an instance of $seeded" "$work/out"; then
  echo "ok   check-vault-seeds.sh refuses a remote instance whose hub is not an instance of INSTANCES_DIR"
else
  echo "FAIL check-vault-seeds.sh on a remote instance without its hub (rc $rc)"; sed 's/^/     /' "$work/out" | head -8; fail=1
fi

# An instance whose manager does not render fails both Vault checks with the
# manager's reason, instead of being checked against the charts' fallbacks.
broken="$work/broken-names"
mkdir -p "$broken/badnames/values/core"
printf 'instance: {name: badnames}\nrepoURL: https://git.example/x.git\nnames: {nope: x}\n' >"$broken/badnames/instance.yaml"
printf 'apps: []\n' >"$broken/badnames/core.yaml"
cp "$ROOT/argo-cd/instances/_example/values/core/vault-operator.yaml" "$broken/badnames/values/core/"
for script in check-vault-access.sh check-vault-seeds.sh; do
  rc=$(runs "$broken" "$script")
  if [ "$rc" != 0 ] && grep -qF "badnames: the instance-manager does not render: names.nope is not a name the product reads" "$work/out"; then
    echo "ok   $script reports an instance whose manager does not render, with the reason"
  else
    echo "FAIL $script on an instance whose manager does not render (rc $rc)"; sed 's/^/     /' "$work/out" | head -5; fail=1
  fi
done

exit "$fail"
