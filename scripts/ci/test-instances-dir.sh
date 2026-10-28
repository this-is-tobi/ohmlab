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
printf 'vault:\n  enabled: false\n' >"$broken/novault/values/core/vault-operator.yaml"
check "check-vault-access.sh reads the instances of INSTANCES_DIR" 1 "$(runs "$broken" check-vault-access.sh)"

broken="$work/broken-seed"
cp -R "$ROOT/argo-cd/instances" "$broken"
mkdir -p "$broken/badseed/values/core"
printf 'ohmlab:\n  vault:\n    secrets:\n    - data:\n        key: "<random:8>"\n' >"$broken/badseed/values/core/vault-operator.yaml"
check "check-vault-seeds.sh reads the instances of INSTANCES_DIR" 1 "$(runs "$broken" check-vault-seeds.sh)"

exit "$fail"
