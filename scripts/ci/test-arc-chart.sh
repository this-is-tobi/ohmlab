#!/usr/bin/env bash
# Tests of the pods actions-runner-controller creates at runtime, which no
# render check sees, against the kyverno chart's pod policies. Each pod is
# built from the render the way ARC builds it:
#
# - a runner pod from each AutoscalingRunnerSet's template, which the
#   controller uses nearly as is;
# - a listener pod: the controller's own image in a `listener` container, with
#   the listenerTemplate's pod security context and its `listener` container's
#   security context and resources, which replace the controller's
#   (mergeListenerPodWithTemplate);
# - a job pod from the container hook's template: its pod fields replace the
#   hook's, and its `$job` container's apply to the job container (ADR 0134 of
#   actions/runner-container-hooks), here the runner image.
#
#   test-arc-chart.sh
#
# Needs what scripts/ci/check-example-pod-security.sh needs. No render is
# printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/actions-runner-controller"
CHECK="$ROOT/scripts/ci/check-example-pod-security.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/tenant/actions-runner-controller.yaml" || { echo "no manager parameters for actions-runner-controller" >&2; exit 1; }
helm template actions-runner-controller "$CHART" --namespace actions-runner-controller "${args[@]}" >"$work/render.yaml" 2>"$work/err" \
  || { echo "FAIL: the chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)" >&2; exit 1; }

# admitted <label> <pod JSON file>: every policy admits the pod, which no
# known failure covers.
admitted() {
  local out
  # The check passes on no input at all: a pod that was not built fails here.
  jq -e '.kind == "Pod"' "$2" >/dev/null 2>&1 || { fail "$1: no pod was built"; return; }
  if ! out=$(yq -p=json -o=yaml '.' "$2" | POD_SECURITY_KNOWN='' "$CHECK" --stdin tenant/actions-runner-controller actions-runner-controller 2>&1); then
    fail "$1:"
    printf '%s\n' "$out" | sed 's/^/     /' >&2
  fi
}

controller_image=$(yq 'select(.kind == "Deployment" and .spec.template.spec.containers[0].name == "manager") | .spec.template.spec.containers[0].image' "$work/render.yaml")
[ -n "$controller_image" ] || fail "no controller Deployment in the render"
yq -o=json -I=0 'select(.kind == "AutoscalingRunnerSet")' "$work/render.yaml" >"$work/sets.json"
sets=$(jq -r '.metadata.name' "$work/sets.json")
[ -n "$sets" ] || fail "no AutoscalingRunnerSet in the render"

for set in $sets; do
  jq -c --arg set "$set" 'select(.metadata.name == $set)
      | {apiVersion: "v1", kind: "Pod", metadata: {name: "\($set)-runner"}, spec: .spec.template.spec}' \
    "$work/sets.json" >"$work/runner-$set.json"
  admitted "the runner pods of $set" "$work/runner-$set.json"

  jq -c --arg set "$set" --arg image "$controller_image" 'select(.metadata.name == $set) | .spec.listenerTemplate as $t
      | (($t.spec.containers // []) | map(select(.name == "listener")) | .[0] // {}) as $c
      | {apiVersion: "v1", kind: "Pod", metadata: {name: "\($set)-listener"},
         spec: {securityContext: ($t.spec.securityContext // {}),
                containers: [{name: "listener", image: $image, securityContext: ($c.securityContext // {}), resources: ($c.resources // {})}]}}' \
    "$work/sets.json" >"$work/listener-$set.json"
  admitted "the listener pod of $set" "$work/listener-$set.json"
done

runner_image=$(jq -r '.spec.template.spec.containers[] | select(.name == "runner") | .image' "$work/sets.json" | head -1)
template=$(yq 'select(.kind == "ConfigMap" and .metadata.name == "runner-hook-template") | .data["job-pod.yaml"]' "$work/render.yaml")
if [ -z "$template" ]; then
  fail "no runner-hook-template ConfigMap: the hook would create job pods with no security context"
else
  printf '%s\n' "$template" | yq -o=json -I=0 '.' | jq -c --arg image "$runner_image" '
      ((.spec.containers // []) | map(select(.name == "$job")) | .[0] // {}) as $c
      | {apiVersion: "v1", kind: "Pod", metadata: {name: "job"},
         spec: (.spec | .containers = [$c | .name = "job" | .image = $image])}' >"$work/job.json"
  admitted "a job pod of the container hook" "$work/job.json"
  # The runners must read the template: the env var names it, and the file is
  # the ConfigMap's key at the mounted path.
  for set in $sets; do
    yq -e "select(.kind == \"AutoscalingRunnerSet\" and .metadata.name == \"$set\") | .spec.template.spec as \$s |
        (\$s.containers[] | select(.name == \"runner\")) as \$c |
        (\$c.env[] | select(.name == \"ACTIONS_RUNNER_CONTAINER_HOOK_TEMPLATE\") | .value) as \$path |
        (\$s.volumes[] | select(.configMap.name == \"runner-hook-template\") | .name) as \$vol |
        (\$c.volumeMounts[] | select(.name == \$vol) | .mountPath + \"/job-pod.yaml\") == \$path" \
      "$work/render.yaml" >/dev/null 2>&1 || fail "the runners of $set do not read the hook template"
  done
fi

if [ "$failures" -gt 0 ]; then
  echo "test-arc-chart: $failures failure(s)" >&2
  exit 1
fi
echo "ok   actions-runner-controller: runner, listener and job pods pass the pod policies"
