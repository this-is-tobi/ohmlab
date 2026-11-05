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
# input; with --instances <dir> each enabled app of every instance of <dir> is
# checked, as <instance>/<tier>/<app>, against the policies of the instance
# that runs kyverno on its cluster (with that instance's values), else the
# example's; without arguments every app the example lists, enabled or not.
# Each app is rendered as Argo CD would (its release
# name, namespace and the manager's parameters), its chart from APPS_DIR
# (default argo-cd/apps) or its chartPath. Prints names only, never a render.
# Needs helm, yq, jq and the kyverno CLI.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
EXAMPLE="$ROOT/argo-cd/instances/_example"

# Failures still to fix, "<app label> <Kind>/<name> <policy>" one per line: a
# failure not listed fails the run, and so does a listed one that passes, until
# its line is removed. POD_SECURITY_KNOWN replaces the list, which is the
# example's: other instances start from none.
KNOWN_DEFAULT='
# Containers without CPU and memory limits (Audit).
# The mlflow chart has no resources key for its dbchecker init container.
tenant/mlflow Deployment/mlflow require-resource-limits
# An image from a registry off the allowlist (Audit).
core/apisix StatefulSet/apisix-etcd restrict-image-registries
'
[ "${1:-}" = "--instances" ] && KNOWN_DEFAULT=''
KNOWN=$(grep -v -E '^[[:space:]]*(#|$)' <<<"${POD_SECURITY_KNOWN-$KNOWN_DEFAULT}" | sort -u)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# render_policies [<kyverno values file> <dir>]: the kyverno chart's
# policies, one copy per tier, and its exceptions, into <dir>, with the values
# of the example's kyverno unless given.
render_policies() {
  local dir="$ROOT/argo-cd/apps/kyverno" vf="${1:-$EXAMPLE/values/core/kyverno.yaml}" out="${2:-$work}" scope
  if [ -f "$dir/Chart.lock" ] && [ ! -d "$dir/charts" ]; then
    helm dependency build "$dir" >/dev/null 2>&1 || { echo "FAIL kyverno: helm dependency build"; return 1; }
  fi
  args=()
  app_args "$vf" 2>"$work/err" \
    || { echo "FAIL kyverno: the manager of $vf does not render: $(head -c 240 "$work/err")"; return 1; }
  helm template kyverno "$dir" --namespace kyverno "${args[@]}" >"$out/kyverno.yaml" 2>"$work/err" \
    || { echo "FAIL kyverno: $(grep -m1 Error "$work/err" | cut -c1-240)"; return 1; }
  for scope in true false; do
    SCOPE=$scope yq ea 'select(.kind == "ValidatingPolicy")
      | (.spec.variables[]? | select(.name == "outOfScope" and (.expression | contains("namespaceObject"))) | .expression) = strenv(SCOPE)' \
      "$out/kyverno.yaml" >"$out/policies-$scope.yaml"
  done
  yq ea 'select(.kind == "PolicyException")' "$out/kyverno.yaml" >"$out/exceptions.yaml"
  [ -s "$out/policies-true.yaml" ] || { echo "FAIL kyverno: the chart renders no ValidatingPolicy"; return 1; }
}
# The policies check_stream applies: render_policies' output directory.
policies="$work"

# check_stream [<instance>/]<tier>/<app> <namespace>: reads the app's manifests
# on stdin, prints one FAIL line per failure the list does not know and per
# listed one that passes, and returns 1 when there is any.
check_stream() {
  local app="$1" scope=false out known pods report tier
  tier="${app%/*}"
  [ "${tier##*/}" = core ] && scope=true
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
    kyverno apply "$policies/policies-$scope.yaml" --exceptions "$policies/exceptions.yaml" --resource "$pods" \
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
: "${APPS_DIR:=$ROOT/argo-cd/apps}"

# check_instance <instance dir> <label prefix> <enabled apps only: true|false>
check_instance() {
  local dir="$1" label="$2" enabled_only="$3" prefix suffix server tier entry app id values chart chart_path release namespace chart_dir n
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
  server=$(yq '.destination.server // "https://kubernetes.default.svc"' "$dir/instance.yaml")
  for tier in core tenant; do
    [ -f "$dir/$tier.yaml" ] || continue
    while IFS= read -r entry; do
      app=$(jq -r .app <<<"$entry")
      id="$label$tier/$app"
      values="$dir/values/$tier/$app.yaml"
      # Argo CD refuses an entry without its values file: nothing to deploy.
      [ -f "$values" ] || continue
      chart=$(jq -r '.chart // .app' <<<"$entry")
      chart_path=$(jq -r '.chartPath // ""' <<<"$entry")
      release=$(jq -r '.releaseName // .app' <<<"$entry")
      namespace=$(jq -r --arg default "$prefix$app$suffix" '.namespace // $default' <<<"$entry")
      chart_dir="$APPS_DIR/$chart"
      [ "$chart" = ohmlab ] && chart_dir="$ROOT/charts/ohmlab"
      [ -n "$chart_path" ] && chart_dir="$APPS_DIR/../../$chart_path"
      checked="$checked $id"
      if [ -f "$chart_dir/Chart.lock" ] && [ ! -d "$chart_dir/charts" ]; then
        helm dependency build "$chart_dir" >/dev/null 2>&1 || { echo "FAIL $id: helm dependency build"; failures=$((failures + 1)); continue; }
      fi
      args=()
      app_args "$values" 2>"$work/err" \
        || { echo "FAIL $id: the instance's manager does not render: $(head -c 240 "$work/err")"; failures=$((failures + 1)); continue; }
      if ! helm template "$release" "$chart_dir" --namespace "$namespace" "${args[@]}" >"$work/manifest" 2>"$work/err"; then
        echo "FAIL $id: $(grep -m1 Error "$work/err" | cut -c1-240)"
        failures=$((failures + 1))
        continue
      fi
      # The policies of the kyverno on the app's cluster, else the example's.
      policies=$(S=$(jq -r --arg s "$server" '.destination.server // $s' <<<"$entry") awk -F'\t' '$1 == ENVIRON["S"] {print $2; exit}' "$work/clusters" 2>/dev/null)
      policies="${policies:-$work}"
      if check_stream "$id" "$namespace" <"$work/manifest"; then
        n=$(awk -v app="$id" '$1 == app' <<<"$KNOWN" | grep -c .)
        echo "ok   $id$([ "$n" -gt 0 ] && echo " ($n known)")"
      else
        failures=$((failures + 1))
      fi
    done < <(E="$enabled_only" yq -o=json -I=0 '.apps[] | select(strenv(E) == "false" or .enabled == "true" or .enabled == true)' "$dir/$tier.yaml")
  done
}

if [ "${1:-}" = "--instances" ]; then
  INSTANCES_DIR="${2:?--instances needs a directory}"
  INSTANCES_DIR="${INSTANCES_DIR%/}"
  # The policies of each cluster that an instance runs kyverno on, with that
  # instance's values: "<server>\t<dir>" lines.
  : >"$work/clusters"
  n=0
  while IFS= read -r d; do
    for tier in core tenant; do
      entry=$(yq -o=json -I=0 '.apps[] | select(.app == "kyverno" and (.enabled == "true" or .enabled == true))' "$d/$tier.yaml" 2>/dev/null | head -n1)
      [ -n "$entry" ] && [ -f "$d/values/$tier/kyverno.yaml" ] || continue
      n=$((n + 1))
      mkdir -p "$work/cluster-$n"
      if render_policies "$d/values/$tier/kyverno.yaml" "$work/cluster-$n"; then
        server=$(jq -r --arg s "$(yq '.destination.server // "https://kubernetes.default.svc"' "$d/instance.yaml")" '.destination.server // $s' <<<"$entry")
        printf '%s\t%s\n' "$server" "$work/cluster-$n" >>"$work/clusters"
      else
        failures=$((failures + 1))
      fi
    done
  done < <(instance_dirs)
  while IFS= read -r d; do
    check_instance "$d" "${d##*/}/" true
  done < <(instance_dirs)
else
  check_instance "$EXAMPLE" "" false
fi
# A listed failure of an app the example no longer renders.
while read -r line; do
  [ -n "$line" ] || continue
  case " $checked " in *" ${line%% *} "*) ;; *) echo "FAIL $line: the example has no such app"; failures=$((failures + 1)) ;; esac
done <<<"$KNOWN"

exit $((failures > 0))
