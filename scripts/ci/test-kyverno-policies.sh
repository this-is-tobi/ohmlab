#!/usr/bin/env bash
# Unit-tests the CEL policies and exceptions the kyverno chart renders. The
# chart is rendered the way production does (the values of a real instance of
# INSTANCES_DIR first, the documented _example as fallback; see
# scripts/lib/instances.sh), the policies.kyverno.io objects are
# exported, and `kyverno test` runs every <chart>/tests/policies/<name>/.
# Fixture files reference ../../rendered/policies.yaml and
# ../../rendered/exceptions.yaml; a fixture directory with a values.yaml gets a
# render of its own, those values over the instance's.
#
# The CLI cannot evaluate namespaceObject and reports a resource a policy does
# not apply to as a passing result whatever the test expects. So the tenant
# scoping variable (outOfScope, which reads the namespace labels) is overridden
# to false in the exported policies, and a fixture with no evaluated row for its
# policy fails the run. The scoping itself is checked on a real cluster.
#
# Every ValidatingPolicy must also share one Kyverno webhook: a namespaceSelector,
# matchConditions or webhookConfiguration of its own gives it a webhook of its
# own, which costs one more admission call per pod and policy. Scope inside the
# expressions instead (see kyverno.cel.scope).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"

chart="${1:-argo-cd/apps/kyverno}"
name=$(basename "$chart")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

shopt -s nullglob
dirs=("$chart"/tests/policies/*/)
if [ "${#dirs[@]}" -eq 0 ]; then
  echo "no policy fixtures under $chart/tests/policies"
  exit 0
fi
command -v kyverno >/dev/null || { echo "kyverno CLI not found on PATH (v1.19.1 expected)" >&2; exit 2; }

values_args() {
  local vf
  vf=$(values_file "$name")
  if [ -n "$vf" ]; then printf '%s\n' -f "$vf"; fi
}
mapfile -t args < <(values_args)

# export_rendered <render> <dir>: the render's policies.kyverno.io objects as
# the fixtures read them, <dir>/policies.yaml and <dir>/exceptions.yaml.
export_rendered() {
  mkdir -p "$2"
  yq ea 'select((.apiVersion // "") | test("^policies\\.kyverno\\.io/")) | select(.kind != "PolicyException") | (.spec.variables[]? | select(.name == "outOfScope" and (.expression | contains("namespaceObject"))) | .expression) = "false"' "$1" >"$2/policies.yaml"
  yq ea 'select((.apiVersion // "") | test("^policies\\.kyverno\\.io/")) | select(.kind == "PolicyException")' "$1" >"$2/exceptions.yaml"
}

mkdir -p "$work/tests"
helm template ci "$chart" --namespace kyverno "${args[@]}" >"$work/all.yaml"
export_rendered "$work/all.yaml" "$work/rendered"

own_webhook=$(yq ea 'select(.kind == "ValidatingPolicy") | select(.spec.matchConstraints.namespaceSelector or .spec.matchConstraints.objectSelector or .spec.matchConditions or .spec.webhookConfiguration) | .metadata.name' "$work/all.yaml")
if [ -n "$own_webhook" ]; then
  echo "::error::these ValidatingPolicies would get a Kyverno webhook of their own (namespaceSelector, objectSelector, matchConditions or webhookConfiguration): $own_webhook"
  failures=$((failures + 1))
fi

# The webhook that admits a PolicyException compiles its match conditions
# without the CEL bindings extension, so `cel.bind` is refused there although
# the CLI evaluates it: the object never reaches the cluster and the sync of
# the whole chart fails on it.
bound=$(yq -o=json -N '.' "$work/rendered/exceptions.yaml" | jq -r 'select([.spec.matchConditions[]?.expression | contains("cel.bind(")] | any) | .metadata.name')
if [ -n "$bound" ]; then
  echo "::error::these PolicyExceptions use cel.bind in a match condition, which the admission webhook refuses (undeclared reference to 'cel'); bind with a one-element list instead, [value].all(name, expression): $bound"
  failures=$((failures + 1))
fi

# A chart's exceptionNamespaces is refused at render time when it names no
# built-in exception or holds what is not a namespace name: an int or a bool
# would render, then make the policy's CEL fail to compile in Kyverno.
if [ "$(yq '.exceptionNamespaces | tag' "$chart/values.yaml")" = '!!map' ]; then
  for bad in 'nope[0]=a' 'rustfs-init-step-limits[0]=Dev_X' 'rustfs-init-step-limits[0]=2024' 'rustfs-init-step-limits[0]=true'; do
    if helm template ci "$chart" --namespace kyverno "${args[@]}" --set "exceptionNamespaces.$bad" >/dev/null 2>&1; then
      echo "::error::exceptionNamespaces.$bad renders: the chart must refuse it"
      failures=$((failures + 1))
    fi
  done
fi

for d in "${dirs[@]}"; do
  t=$(basename "$d")
  echo "::group::$t"
  rc=0
  root="$work"
  if [ -f "$d/values.yaml" ]; then
    root="$work/sets/$t"
    mkdir -p "$root/tests"
    helm template ci "$chart" --namespace kyverno "${args[@]}" -f "$d/values.yaml" >"$root/all.yaml"
    export_rendered "$root/all.yaml" "$root/rendered"
  fi
  cp -R "$d" "$root/tests/$t"
  (cd "$root/tests/$t" && kyverno test . --remove-color) >"$work/$t.out" 2>&1 || rc=1
  cat "$work/$t.out"
  # The CLI exits 0 when it finds no test file ("No test yamls available"),
  # which is also what it does when it cannot read the directory.
  if ! grep -Eq '^│ *[0-9]+ ' "$work/$t.out"; then
    echo "::error::$t: no test ran (no kyverno-test.yaml found, or no result in it)"
    rc=1
  fi
  # A workload kind has two rows per policy (the pod rule, which excludes it, and
  # the autogenerated one), so only a policy/resource pair with no evaluated row counts.
  if ! unevaluated=$(awk -F'│' '
      /^│ *[0-9]+ / {
        for (i = 3; i <= 7; i++) gsub(/^ +| +$/, "", $i)
        k = $3 " " $5; seen[k] = 1
        if ($7 != "Excluded") evaluated[k] = 1
      }
      END { for (k in seen) if (!(k in evaluated)) { print k; bad = 1 } exit bad }' "$work/$t.out"); then
    echo "::error::$t: fixtures never evaluated by their policy (the CLI reports that as a pass whatever is expected):"
    echo "$unevaluated"
    rc=1
  fi
  [ "$rc" -eq 0 ] || failures=$((failures + 1))
  echo "::endgroup::"
done

exit "$failures"
