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

broken="$work/broken-seed"
cp -R "$ROOT/argo-cd/instances" "$broken"
mkdir -p "$broken/badseed/values/core"
printf 'instance: {name: badseed}\nrepoURL: https://git.example/x.git\n' >"$broken/badseed/instance.yaml"
printf 'ohmlab:\n  vault:\n    secrets:\n    - data:\n        key: "<random:8>"\n' >"$broken/badseed/values/core/vault-operator.yaml"
check "check-vault-seeds.sh reads the instances of INSTANCES_DIR" 1 "$(runs "$broken" check-vault-seeds.sh)"

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
