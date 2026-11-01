#!/usr/bin/env bash
# Renders every chart the way a new adopter would deploy it: against the
# values of the documented _example instance. The example is a template that
# people copy, so a chart that cannot render with it (a required value, a
# stale key, a schema mismatch) breaks the first deployment of anyone else.
# Needs helm; validates against the Kubernetes schemas when kubeconform is
# available. CATALOG_ROOT renders another tree with the layout of this
# repository, such as an extracted catalog bundle.
set -uo pipefail

ROOT="${CATALOG_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
EXAMPLE="$ROOT/argo-cd/instances/_example"
# shellcheck source=scripts/lib/instances.sh
source "$(dirname "${BASH_SOURCE[0]}")/../lib/instances.sh"

failures=0
for chart in "$ROOT/utils/helm" "$ROOT"/argo-cd/apps/*/; do
  chart="${chart%/}"
  [ -f "$chart/Chart.yaml" ] || continue
  name="$(basename "$chart")"
  [ "$name" = helm ] && name=ohmlab

  args=()
  if [ "$name" = instance-manager ]; then
    args=(-f "$EXAMPLE/instance.yaml" --set instance.name=example)
  else
    for scope in core tenant; do
      if [ -f "$EXAMPLE/values/$scope/$name.yaml" ]; then
        # The example leaves its secrets' mount and path to the manager.
        app_args "$EXAMPLE/values/$scope/$name.yaml" 2>"${TMPDIR:-/tmp}/render-example.err" \
          || { echo "FAIL $name: the example's manager does not render: $(head -c 240 "${TMPDIR:-/tmp}/render-example.err")"; failures=$((failures + 1)); continue 2; }
        break
      fi
    done
  fi

  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL $name: helm dependency build"; failures=$((failures + 1)); continue; }
  fi

  if ! rendered=$(helm template example "$chart" "${args[@]+"${args[@]}"}" 2>"${TMPDIR:-/tmp}/render-example.err"); then
    echo "FAIL $name: $(grep -m1 '^Error' "${TMPDIR:-/tmp}/render-example.err" | cut -c1-240)"
    failures=$((failures + 1))
    continue
  fi
  if command -v kubeconform >/dev/null 2>&1 \
     && ! printf '%s\n' "$rendered" | kubeconform -ignore-missing-schemas -strict -summary >/dev/null 2>"${TMPDIR:-/tmp}/render-example.err"; then
    echo "FAIL $name: kubeconform: $(head -c 240 "${TMPDIR:-/tmp}/render-example.err")"
    failures=$((failures + 1))
    continue
  fi
  echo "ok   $name"
done
rm -f "${TMPDIR:-/tmp}/render-example.err"

exit "$failures"
