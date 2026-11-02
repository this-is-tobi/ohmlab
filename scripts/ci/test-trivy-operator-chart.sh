#!/usr/bin/env bash
# Tests of the pods trivy-operator creates at runtime, which no render check
# sees, against the kyverno chart's pod policies and exceptions, and of the
# settings that keep its scans light. The operator builds those pods from its
# ConfigMaps, so each one is built here the same way:
#
# - a scan pod: the trivy image, with the scan pods' security contexts and
#   resources;
# - with infra assessment on, the node-collector pod it runs on every node:
#   trivy-kubernetes' node-collector Job template (the host PID namespace, the
#   node-collector image, its host paths) under those same contexts and
#   resources, which replace the template's.
#
# A key the chart renames would otherwise leave every such pod refused at
# admission, and only a running cluster would show it.
#
#   test-trivy-operator-chart.sh
#
# Needs what scripts/ci/check-example-pod-security.sh needs. No render is
# printed.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
CHART="$ROOT/argo-cd/apps/trivy-operator"
CHECK="$ROOT/scripts/ci/check-example-pod-security.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

if [ -f "$CHART/Chart.lock" ] && [ ! -d "$CHART/charts" ]; then
  helm dependency build "$CHART" >/dev/null || { echo "cannot build the dependencies of $CHART" >&2; exit 1; }
fi
args=()
app_args "$ROOT/argo-cd/instances/_example/values/core/trivy-operator.yaml" || { echo "no manager parameters for trivy-operator" >&2; exit 1; }
helm template trivy-operator "$CHART" --namespace trivy-operator "${args[@]}" >"$work/render.yaml" 2>"$work/err" \
  || { echo "FAIL: the chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)" >&2; exit 1; }

# The data of each ConfigMap the operator reads, as JSON.
for cm in trivy-operator trivy-operator-trivy-config trivy-operator-config; do
  yq -o=json -I=0 "select(.kind == \"ConfigMap\" and .metadata.name == \"$cm\") | .data" "$work/render.yaml" >"$work/$cm.json"
  [ -s "$work/$cm.json" ] || fail "no $cm ConfigMap in the render"
done
setting() { jq -r --arg k "$2" '.[$k] // empty' "$work/$1.json"; }

# admitted <label> <pod JSON file>: every policy admits the pod, which no
# known failure covers.
admitted() {
  local out
  # The check passes on no input at all: a pod that was not built fails here.
  jq -e '.kind == "Pod"' "$2" >/dev/null 2>&1 || { fail "$1: no pod was built"; return; }
  if ! out=$(yq -p=json -o=yaml '.' "$2" | POD_SECURITY_KNOWN='' "$CHECK" --stdin core/trivy-operator trivy-operator 2>&1); then
    fail "$1:"
    printf '%s\n' "$out" | sed 's/^/     /' >&2
  fi
}

# pod <name> <kind: scan|node-collector>: the pod the operator would create.
pod() {
  jq -n --arg name "$1" --arg kind "$2" \
    --slurpfile op "$work/trivy-operator.json" --slurpfile tr "$work/trivy-operator-trivy-config.json" '
    $op[0] as $op | $tr[0] as $tr
    | ($op["scanJob.podTemplatePodSecurityContext"] // "{}" | fromjson) as $psc
    | ($op["scanJob.podTemplateContainerSecurityContext"] // "{}" | fromjson) as $csc
    | {requests: {cpu: $tr["trivy.resources.requests.cpu"], memory: $tr["trivy.resources.requests.memory"]},
       limits: {cpu: $tr["trivy.resources.limits.cpu"], memory: $tr["trivy.resources.limits.memory"]}} as $res
    | {apiVersion: "v1", kind: "Pod", metadata: {name: $name},
       spec: ({restartPolicy: "Never", securityContext: $psc} + if $kind == "scan" then
         {containers: [{name: "scan", image: "\($tr["trivy.repository"]):\($tr["trivy.tag"])", securityContext: $csc, resources: $res,
                        volumeMounts: [{name: "tmp", mountPath: "/tmp"}]}],
          volumes: [{name: "tmp", emptyDir: {}}]}
       else
         {hostPID: true,
          containers: [{name: "node-collector", image: $op["node.collector.imageRef"], args: ["k8s"], securityContext: $csc, resources: $res,
                        volumeMounts: ($op["nodeCollector.volumeMounts"] // "[]" | fromjson)}],
          volumes: ($op["nodeCollector.volumes"] // "[]" | fromjson)}
       end)}'
}

pod scan-vulnerabilityreport-5668d99795-x7k2p scan >"$work/scan.json"
admitted "a scan pod" "$work/scan.json"
if [ "$(setting trivy-operator-config OPERATOR_INFRA_ASSESSMENT_SCANNER_ENABLED)" = true ]; then
  pod node-collector-6fbc4cc977-h26k8 node-collector >"$work/node-collector.json"
  admitted "a node-collector pod" "$work/node-collector.json"
fi

mode=$(setting trivy-operator-trivy-config trivy.mode)
[ "$mode" = ClientServer ] \
  || fail "scans must use the built-in Trivy server, not a database download per scan pod (trivy.mode: '$mode')"
cache=$(setting trivy-operator-config OPERATOR_CLUSTER_SBOM_CACHE_ENABLED)
[ "$cache" = true ] \
  || fail "rescans must read each image's cached SBOM rather than pull the image again (OPERATOR_CLUSTER_SBOM_CACHE_ENABLED: '$cache')"

if [ "$failures" -gt 0 ]; then
  echo "test-trivy-operator-chart: $failures failure(s)" >&2
  exit 1
fi
echo "ok   trivy-operator: scan and node-collector pods pass the pod policies, client-server scans, SBOM cache"
