#!/usr/bin/env bash
# Decision table for scripts/ci/check-values-keys.sh on throwaway trees.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-values-keys.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# tree <name>: a root whose catalog has one app, `app`, with a dependency
# under an alias (sub as subalias), one without (plain), and templates that
# read `own` and `dashed-key`; the example lists it in tenant. Prints the root.
tree() {
  local root="$work/$1"
  mkdir -p "$root/argo-cd/apps/app/templates" "$root/argo-cd/instances/_example/values/tenant"
  printf 'apiVersion: v2\nname: app\nversion: 0.1.0\ndependencies:\n- name: sub\n  alias: subalias\n  version: 1.0.0\n- name: plain\n  version: 1.0.0\n' >"$root/argo-cd/apps/app/Chart.yaml"
  printf 'own: {{ .Values.own }}\ndashed: {{ index $.Values "dashed-key" }}\n' >"$root/argo-cd/apps/app/templates/cm.yaml"
  printf 'subalias: {}\n' >"$root/argo-cd/apps/app/values.yaml"
  printf 'apps:\n- app: app\n' >"$root/argo-cd/instances/_example/tenant.yaml"
  echo "$root"
}

check() { # label expected-exit root [text the output must contain]
  local out rc
  out=$("$SCRIPT" "$3" 2>&1); rc=$?
  if [ "$rc" != "$2" ]; then
    echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${4:-}" ] && ! grep -qF -- "$4" <<<"$out"; then
    echo "FAIL $1: output does not name '$4'"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $1"
  fi
}

r=$(tree reached)
printf 'subalias: {}\nplain: {}\nglobal: {}\nown: x\ndashed-key: y\n' >"$r/argo-cd/apps/app/values.yaml"
printf 'subalias: {}\nown: z\n' >"$r/argo-cd/instances/_example/values/tenant/app.yaml"
check "a dependency's alias, a dependency without one, global and keys the templates read all reach the chart" 0 "$r"

r=$(tree typo)
printf 'sbualias: {}\n' >"$r/argo-cd/apps/app/values.yaml"
check "a misspelled dependency key in the app's values fails" 1 "$r" "argo-cd/apps/app/values.yaml: 'sbualias'"

r=$(tree name)
printf 'sub: {}\n' >"$r/argo-cd/apps/app/values.yaml"
check "a dependency with an alias is not reached by its name" 1 "$r" "'sub'"

r=$(tree instance)
printf 'nope: {}\n' >"$r/argo-cd/instances/_example/values/tenant/app.yaml"
check "a key of an instance's values file that reaches no chart fails" 1 "$r" "argo-cd/instances/_example/values/tenant/app.yaml: 'nope'"

r=$(tree chart)
printf 'apps:\n- app: other\n  chart: app\n' >"$r/argo-cd/instances/_example/tenant.yaml"
printf 'own: x\n' >"$r/argo-cd/instances/_example/values/tenant/other.yaml"
check "an entry naming another chart is checked against that chart" 0 "$r"
printf 'other: {}\n' >"$r/argo-cd/instances/_example/values/tenant/other.yaml"
check "and fails on a key that chart does not read" 1 "$r" "'other'"

r=$(tree path)
mkdir -p "$r/utils/helm/templates"
printf 'apiVersion: v2\nname: ohmlab\nversion: 0.1.0\ndependencies:\n- name: argo-cd\n  version: 1.0.0\n' >"$r/utils/helm/Chart.yaml"
printf 'apps:\n- app: tools\n  chartPath: utils/helm\n' >"$r/argo-cd/instances/_example/tenant.yaml"
printf 'argo-cd: {}\n' >"$r/argo-cd/instances/_example/values/tenant/tools.yaml"
check "an entry's chartPath is the chart it is checked against" 0 "$r"
printf 'apps:\n- app: ohmlab\n' >"$r/argo-cd/instances/_example/tenant.yaml"
printf 'argo-cd: {}\n' >"$r/argo-cd/instances/_example/values/tenant/ohmlab.yaml"
check "the ohmlab app is checked against utils/helm without a chartPath" 0 "$r"
printf 'argocd: {}\n' >"$r/argo-cd/instances/_example/values/tenant/ohmlab.yaml"
check "and fails on a key utils/helm does not read" 1 "$r" "'argocd'"

r=$(tree missing)
printf 'apps:\n- app: app\n- app: gone\n' >"$r/argo-cd/instances/_example/tenant.yaml"
printf '' >"$r/argo-cd/apps/app/values.yaml"
check "an entry without a values file and an empty values file pass" 0 "$r"

r=$(tree dir)
mkdir -p "$work/elsewhere/argo-cd/instances/mylab/values/tenant"
printf 'apps:\n- app: app\n' >"$work/elsewhere/argo-cd/instances/mylab/tenant.yaml"
printf 'nope: {}\n' >"$work/elsewhere/argo-cd/instances/mylab/values/tenant/app.yaml"
INSTANCES_DIR="$work/elsewhere/argo-cd/instances" \
  check "the instances of INSTANCES_DIR are checked too" 1 "$r" "mylab/values/tenant/app.yaml: 'nope'"

exit "$fail"
