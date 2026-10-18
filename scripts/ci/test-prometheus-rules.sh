#!/usr/bin/env bash
# Unit-tests the alert rules each app chart renders. For every
# <apps-root>/<app>/tests/prometheusrules.test.yaml, render the chart the
# way production does (homelab instance values first, the documented
# _example as fallback, or the chart's own tests/values.yaml when it has one),
# keep its PrometheusRule groups, and run promtool.
# Charts render into namespace "test", so rules filtering on the release
# namespace are tested with namespace="test".
set -euo pipefail

root="${1:-argo-cd/apps}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

promtool_in() { # $1 = directory holding rules.yaml and test.yaml, rest = promtool args
  local dir="$1"; shift
  if command -v promtool >/dev/null; then
    (cd "$dir" && promtool "$@")
  else
    docker run --rm -v "$dir:/w" -w /w --entrypoint promtool \
      "quay.io/prometheus/prometheus:${PROMETHEUS_VERSION:-v3.15.0}" "$@"
  fi
}

# values_args <chart dir>: the values to render a chart with. A chart that
# renders nothing until a feature is on (the ohmlab controller is one) brings
# its own in tests/values.yaml; the others are rendered the way production does.
values_args() {
  local inst scope vf name
  name=$(basename "$1")
  if [ -f "$1/tests/values.yaml" ]; then printf '%s\n' -f "$1/tests/values.yaml"; return; fi
  for inst in homelab _example; do
    for scope in core tenant; do
      vf="argo-cd/instances/$inst/values/$scope/$name.yaml"
      if [ -f "$vf" ]; then printf '%s\n' -f "$vf"; return; fi
    done
  done
}

for test in "$root"/*/tests/prometheusrules.test.yaml; do
  [ -f "$test" ] || continue
  chart=$(dirname "$(dirname "$test")")
  name=$(basename "$chart")
  dir="$work/$name"
  mkdir -p "$dir"
  mapfile -t args < <(values_args "$chart")
  echo "::group::$name"
  if helm template ci "$chart" --namespace test "${args[@]}" \
      | yq ea '[select(.kind == "PrometheusRule") | .spec.groups[]] | {"groups": .}' >"$dir/rules.yaml" \
    && cp "$test" "$dir/test.yaml" \
    && promtool_in "$dir" check rules rules.yaml \
    && promtool_in "$dir" test rules test.yaml; then
    :
  else
    failures=$((failures + 1))
  fi
  echo "::endgroup::"
done

exit "$failures"
