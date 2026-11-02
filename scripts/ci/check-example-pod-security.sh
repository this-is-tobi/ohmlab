#!/usr/bin/env bash
# Every pod an app of the example instance renders is admitted by the
# product's own pod policies: the ValidatingPolicies and PolicyExceptions the
# kyverno chart renders with the example's values, applied by the Kyverno CLI.
# Adopters copy the example, and an instance's policy reports only cover the
# apps it runs: a pod an Enforce policy refuses never starts, and one an Audit
# policy fails keeps that policy from ever being enforced.
#
# The pods are read the way the API server admits them: in the app's
# namespace when the chart sets none, without null fields, and with a volume
# that names no source made an emptyDir (on the raw render the CLI errors or
# fails on both). Helm test hooks are left out: Argo CD never runs them. The
# CLI cannot read a namespace's labels, so the tenant-scope variable is set to
# true (out of scope) for core apps and false for tenant apps, as
# test-kyverno-policies.sh does.
#
# With --stdin <tier>/<app> [<namespace>] the manifests are read from standard
# input; without arguments every app the example lists, enabled or not, is
# rendered as Argo CD would (its release name, namespace and the manager's
# parameters) and checked. Prints names only, never a render. Needs helm, yq,
# jq and the kyverno CLI.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
EXAMPLE="$ROOT/argo-cd/instances/_example"

# Failures still to fix, "<tier>/<app> <Kind>/<name> <policy>" one per line: a
# failure not listed fails the run, and so does a listed one that passes, until
# its line is removed. POD_SECURITY_KNOWN replaces the list.
KNOWN_DEFAULT='
# Refused at admission: pods that may run as root (require-non-root) or run a
# privileged container (pod-security-baseline), both Enforce.
tenant/sonarqube StatefulSet/sonarqube-sonarqube pod-security-baseline
tenant/teleport Deployment/teleport-auth require-non-root
tenant/teleport Deployment/teleport-proxy require-non-root
tenant/teleport Job/teleport-auth-test require-non-root
tenant/teleport Job/teleport-proxy-test require-non-root
# Tenant pods short of the restricted profile, which stays Audit until every
# tenant chart meets it.
tenant/sonarqube StatefulSet/sonarqube-sonarqube pod-security-restricted
tenant/teleport Deployment/teleport-auth pod-security-restricted
tenant/teleport Deployment/teleport-proxy pod-security-restricted
tenant/teleport Job/teleport-auth-test pod-security-restricted
tenant/teleport Job/teleport-proxy-test pod-security-restricted
# Containers without CPU and memory limits (Audit).
core/apisix Deployment/apisix-ingress-controller require-resource-limits
core/ingress-nginx Job/ingress-nginx-admission-create require-resource-limits
core/ingress-nginx Job/ingress-nginx-admission-patch require-resource-limits
core/ohmlab Deployment/argocd-redis require-resource-limits
core/prometheus-stack Job/prometheus-stack-kube-prom-admission-create require-resource-limits
core/prometheus-stack Job/prometheus-stack-kube-prom-admission-patch require-resource-limits
core/vault Deployment/vault-agent-injector require-resource-limits
core/vault StatefulSet/vault require-resource-limits
tenant/argo-workflows Deployment/argo-workflows-server require-resource-limits
tenant/argo-workflows Deployment/argo-workflows-workflow-controller require-resource-limits
tenant/argo-workflows Job/argo-workflows-crd-install require-resource-limits
tenant/harbor Deployment/harbor-core require-resource-limits
tenant/harbor Deployment/harbor-exporter require-resource-limits
tenant/harbor Deployment/harbor-jobservice require-resource-limits
tenant/harbor Deployment/harbor-portal require-resource-limits
tenant/harbor Deployment/harbor-registry require-resource-limits
tenant/harbor StatefulSet/harbor-redis require-resource-limits
tenant/mlflow Deployment/mlflow require-resource-limits
tenant/mlflow Deployment/mlflow-sso require-resource-limits
tenant/sonarqube StatefulSet/sonarqube-sonarqube require-resource-limits
# An image from a registry off the allowlist (Audit).
core/apisix StatefulSet/apisix-etcd restrict-image-registries
'
KNOWN=$(grep -v -E '^[[:space:]]*(#|$)' <<<"${POD_SECURITY_KNOWN-$KNOWN_DEFAULT}" | sort -u)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# render_policies: the kyverno chart's policies, one copy per tier, and its
# exceptions, into $work.
render_policies() {
  local dir="$ROOT/argo-cd/apps/kyverno" scope
  if [ -f "$dir/Chart.lock" ] && [ ! -d "$dir/charts" ]; then
    helm dependency build "$dir" >/dev/null 2>&1 || { echo "FAIL kyverno: helm dependency build"; return 1; }
  fi
  args=()
  app_args "$EXAMPLE/values/core/kyverno.yaml" 2>"$work/err" \
    || { echo "FAIL kyverno: the example's manager does not render: $(head -c 240 "$work/err")"; return 1; }
  helm template kyverno "$dir" --namespace kyverno "${args[@]}" >"$work/kyverno.yaml" 2>"$work/err" \
    || { echo "FAIL kyverno: $(grep -m1 Error "$work/err" | cut -c1-240)"; return 1; }
  for scope in true false; do
    SCOPE=$scope yq ea 'select(.kind == "ValidatingPolicy")
      | (.spec.variables[]? | select(.name == "outOfScope" and (.expression | contains("namespaceObject"))) | .expression) = strenv(SCOPE)' \
      "$work/kyverno.yaml" >"$work/policies-$scope.yaml"
  done
  yq ea 'select(.kind == "PolicyException")' "$work/kyverno.yaml" >"$work/exceptions.yaml"
  [ -s "$work/policies-true.yaml" ] || { echo "FAIL kyverno: the chart renders no ValidatingPolicy"; return 1; }
}

# check_stream <tier>/<app> <namespace>: reads the app's manifests on stdin,
# prints one FAIL line per failure the list does not know and per listed one
# that passes, and returns 1 when there is any.
check_stream() {
  local app="$1" scope=false out known pods report
  [ "${app%%/*}" = core ] && scope=true
  # One file per app: a CLI in a container can read a reused path stale.
  pods="$work/pods-${app//\//-}.yaml" report="$work/report-${app//\//-}.json"
  NS="$2" yq e '
    select(.kind != null and (.kind | test("^(Deployment|StatefulSet|DaemonSet|Job|CronJob|Pod)$")))
    | select(((.metadata.annotations["helm.sh/hook"] // "") | test("test")) | not)
    | .metadata.namespace = (.metadata.namespace // strenv(NS))
    | del(.. | select(. == null))
    | (.. | select(tag == "!!map" and has("volumes")) | .volumes[] | select((keys | length) == 1 and has("name"))) |= . + {"emptyDir": {}}' \
    - >"$pods" 2>"$work/err" || { echo "FAIL $app: unreadable manifests: $(head -c 240 "$work/err")"; return 1; }
  known=$(awk -v app="$app" '$1 == app {print $1, $2, $3}' <<<"$KNOWN")
  if [ -z "$(yq e '.kind' "$pods" 2>/dev/null | grep -v -E '^(---|null)?$')" ]; then
    # No pod: every listed failure of the app passes.
    out=""
  else
    kyverno apply "$work/policies-$scope.yaml" --exceptions "$work/exceptions.yaml" --resource "$pods" \
      --policy-report --output-format json >"$report" 2>"$work/err"
    out=$(jq -r '[.. | objects | select(has("results") and (.results | type) == "array") | .results[]]
      | if length == 0 then "none" else .[] | select(.result == "fail" or .result == "error")
        | "\(.resources[0].kind)/\(.resources[0].name)\t\(.policy)\t\(.result): \(.message // "" | gsub("\n"; " ") | .[0:160])" end' \
      "$report" 2>/dev/null) || out="none"
    if [ "$out" = none ] || [ -z "$(jq -r '[.. | objects | select(has("results")) | .results[]] | length' "$report" 2>/dev/null | grep -v '^0$')" ]; then
      echo "FAIL $app: the Kyverno CLI returned no result for its pods: $(head -c 240 "$work/err")"
      return 1
    fi
  fi
  local failed=0 key message
  while IFS=$'\t' read -r key policy message; do
    [ -n "$key" ] || continue
    if grep -qxF "$app $key $policy" <<<"$known"; then continue; fi
    echo "FAIL $app $key $policy: $message"
    failed=1
  done <<<"$out"
  while read -r line; do
    [ -n "$line" ] || continue
    # shellcheck disable=SC2086
    set -- $line
    if ! printf '%s\n' "$out" | awk -F'\t' -v k="$2" -v p="$3" '$1 == k && $2 == p {found = 1} END {exit !found}'; then
      echo "FAIL $line no longer fails: remove it from the list"
      failed=1
    fi
  done <<<"$known"
  return "$failed"
}

render_policies || exit 1

if [ "${1:-}" = "--stdin" ]; then
  app="${2:-tenant/stdin}"
  check_stream "$app" "${3:-${app#*/}}" || exit 1
  exit 0
fi

failures=0
checked=""
prefix=$(yq '.namespacePrefix // ""' "$EXAMPLE/instance.yaml")
suffix=$(yq '.namespaceSuffix // ""' "$EXAMPLE/instance.yaml")
for tier in core tenant; do
  [ -f "$EXAMPLE/$tier.yaml" ] || continue
  while IFS= read -r entry; do
    app=$(jq -r .app <<<"$entry")
    values="$EXAMPLE/values/$tier/$app.yaml"
    # Argo CD refuses an entry without its values file: nothing to deploy.
    [ -f "$values" ] || continue
    chart=$(jq -r '.chart // .app' <<<"$entry")
    release=$(jq -r '.releaseName // .app' <<<"$entry")
    namespace=$(jq -r --arg default "$prefix$app$suffix" '.namespace // $default' <<<"$entry")
    dir="$ROOT/argo-cd/apps/$chart"
    [ "$chart" = ohmlab ] && dir="$ROOT/utils/helm"
    checked="$checked $tier/$app"
    if [ -f "$dir/Chart.lock" ] && [ ! -d "$dir/charts" ]; then
      helm dependency build "$dir" >/dev/null 2>&1 || { echo "FAIL $tier/$app: helm dependency build"; failures=$((failures + 1)); continue; }
    fi
    args=()
    app_args "$values" 2>"$work/err" \
      || { echo "FAIL $tier/$app: the example's manager does not render: $(head -c 240 "$work/err")"; failures=$((failures + 1)); continue; }
    if ! helm template "$release" "$dir" --namespace "$namespace" "${args[@]}" >"$work/manifest" 2>"$work/err"; then
      echo "FAIL $tier/$app: $(grep -m1 Error "$work/err" | cut -c1-240)"
      failures=$((failures + 1))
      continue
    fi
    if check_stream "$tier/$app" "$namespace" <"$work/manifest"; then
      n=$(awk -v app="$tier/$app" '$1 == app' <<<"$KNOWN" | grep -c .)
      echo "ok   $tier/$app$([ "$n" -gt 0 ] && echo " ($n known)")"
    else
      failures=$((failures + 1))
    fi
  done < <(yq -o=json -I=0 '.apps[]' "$EXAMPLE/$tier.yaml")
done
# A listed failure of an app the example no longer renders.
while read -r line; do
  [ -n "$line" ] || continue
  case " $checked " in *" ${line%% *} "*) ;; *) echo "FAIL $line: the example has no such app"; failures=$((failures + 1)) ;; esac
done <<<"$KNOWN"

exit $((failures > 0))
