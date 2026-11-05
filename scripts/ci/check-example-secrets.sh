#!/usr/bin/env bash
# Every Secret an app of the example instance reads is one that app writes,
# and by one writer. Adopters copy the example: a pod reading a Secret nothing
# writes never starts, and a Secret that both a chart renders and the Vault
# Secrets Operator writes is rewritten by each in turn (Argo CD's self-heal
# against VSO's refresh) unless the app's entry leaves VSO its fields.
#
# A Secret counts as written by the app when the app renders it, a
# VaultStaticSecret of the app writes it, a Certificate of the app stores it, a
# CNPG Cluster of the app generates it (<cluster>-app, -superuser, -ca,
# -server, -replication), or the app creates it at runtime (RUNTIME below).
# Optional references are not checked.
#
# With --stdin <tier>/<app> [<ignoreDifferences JSON>] the manifests are read
# from standard input; with --instances <dir> each enabled app of every
# instance of <dir> is checked, as <instance>/<tier>/<app>; without arguments
# every app the example lists, enabled or not. Each app is rendered as Argo CD
# would (its release name, namespace and the manager's parameters), its chart
# from APPS_DIR (default argo-cd/apps) or its chartPath. Prints names only,
# never a render. Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
EXAMPLE="$ROOT/argo-cd/instances/_example"

# Secrets an app creates at runtime, by a Job or an operator of its own, rather
# than renders: "<tier>/<app> <Secret>", one per line.
RUNTIME='core/ohmlab argocd-redis
tenant/argo-cd argocd-redis
core/prometheus-stack prometheus-stack-kube-prom-admission'

# check_stream [<instance>/]<tier>/<app> <ignoreDifferences JSON>: reads the
# app's manifests on stdin, prints one line per violation.
check_stream() {
  local runtime
  runtime=$(awk -v app="$1" 'BEGIN {n = split(app, p, "/"); key = p[n - 1] "/" p[n]} $1 == key {print $2}' <<<"$RUNTIME" | jq -R . | jq -s -c .)
  yq ea -o=json -I=0 '[select(. != null and .kind != null)]' - 2>/dev/null \
    | jq -r --arg app "$1" --argjson ignore "$2" --argjson runtime "$runtime" '
        def cnpg: .kind == "Cluster" and ((.apiVersion // "") | startswith("postgresql.cnpg.io/"));
        # Who writes which Secret, with the keys when they are known.
        def writers:
          [ .[]
            | if .kind == "Secret" then {s: .metadata.name, by: "chart", keys: ((.data // {}) + (.stringData // {}) | keys)}
              elif .kind == "VaultStaticSecret" then
                (.spec.destination.transformation // {}) as $t
                | {s: .spec.destination.name, by: "vso",
                   keys: (if (($t.excludes // []) | index(".*")) != null and $t.templates != null then ($t.templates | keys) else null end)}
              elif .kind == "Certificate" then {s: .spec.secretName, by: "cert-manager", keys: null}
              elif cnpg then .metadata.name as $c | ("app", "superuser", "ca", "server", "replication") | {s: "\($c)-\(.)", by: "cnpg", keys: null}
              else empty end
            | select(.s != null) ];
        # Every Secret an object reads, with the key when it reads one.
        def reads:
          .[] | "\(.kind)/\(.metadata.name)" as $who
          | ( (.. | objects | select(has("secretKeyRef")) | .secretKeyRef | select(.optional != true) | {s: .name, k: .key}),
              (.. | objects | select(has("secretRef")) | .secretRef | select(.optional != true) | {s: .name, k: null}),
              (.. | objects | select(has("secret") and (.secret | type) == "object" and (.secret | has("secretName")))
                | .secret | select(.optional != true) | {s: .secretName, k: null}),
              (select(cnpg) | (.spec.superuserSecret.name, .spec.bootstrap.initdb.secret.name, (.spec.managed.roles // [] | .[].passwordSecret.name))
                | {s: ., k: null}),
              (select(.kind == "ObjectStore") | (.spec.configuration.s3Credentials // {}) | (.accessKeyId, .secretAccessKey)
                | select(. != null) | {s: .name, k: .key}) )
          | select(.s != null) | . + {who: $who};
        # The entry keeps Argo CD off what VSO writes: the data, and the
        # fields VSO manages (its labels and owner reference).
        def leaves_vso($s):
          any($ignore[]; .kind == "Secret" and .name == $s
            and ((.managedFieldsManagers // []) | index("vault-secrets-operator")) != null
            and (((.jqPathExpressions // []) | index(".data")) != null or ((.jsonPointers // []) | index("/data")) != null));
        writers as $w
        | ( ( reads | . as $r | [$w[] | select(.s == $r.s)] as $m
              | if ($m | length) == 0 then
                  if ($runtime | index($r.s)) == null then "\($app): \($r.who) reads Secret \($r.s), which nothing in the app writes" else empty end
                elif $r.k != null and ($m | all(.keys != null and (.keys | index($r.k)) == null)) then
                  "\($app): \($r.who) reads key \($r.k) of Secret \($r.s), which none of its writers sets"
                else empty end ),
            ( $w | group_by(.s)[] | select(any(.by == "vso")) | .[0].s as $s
              | ([.[] | select(.by == "vso")] | length) as $n
              | if $n > 1 then "\($app): Secret \($s) is written by \($n) VaultStaticSecrets, each over the other"
                elif any(.by == "chart") and (leaves_vso($s) | not) then
                  "\($app): Secret \($s) is rendered by the chart and written by VSO, and Argo CD and VSO rewrite each other: the entry must ignore its data and the fields VSO manages (jqPathExpressions [.data], managedFieldsManagers [vault-secrets-operator])"
                else empty end ) )' \
    | sort -u
}

if [ "${1:-}" = "--stdin" ]; then
  violations=$(check_stream "${2:-stdin}" "${3:-[]}")
  [ -z "$violations" ] || { printf '%s\n' "$violations"; exit 1; }
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0
: "${APPS_DIR:=$ROOT/argo-cd/apps}"

# check_instance <instance dir> <label prefix> <enabled apps only: true|false>
check_instance() {
  local dir="$1" label="$2" enabled_only="$3" prefix suffix tier entry app id values chart chart_path release namespace ignore chart_dir violations v
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
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
      ignore=$(jq -c '.ignoreDifferences // []' <<<"$entry")
      chart_dir="$APPS_DIR/$chart"
      [ "$chart" = ohmlab ] && chart_dir="$ROOT/charts/ohmlab"
      [ -n "$chart_path" ] && chart_dir="$APPS_DIR/../../$chart_path"
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
      violations=$(check_stream "$id" "$ignore" <"$work/manifest")
      if [ -n "$violations" ]; then
        while IFS= read -r v; do echo "FAIL $v"; done <<<"$violations"
        failures=$((failures + 1))
      else
        echo "ok   $id"
      fi
    done < <(E="$enabled_only" yq -o=json -I=0 '.apps[] | select(strenv(E) == "false" or .enabled == "true" or .enabled == true)' "$dir/$tier.yaml")
  done
}

if [ "${1:-}" = "--instances" ]; then
  INSTANCES_DIR="${2:?--instances needs a directory}"
  INSTANCES_DIR="${INSTANCES_DIR%/}"
  while IFS= read -r d; do
    check_instance "$d" "${d##*/}/" true
  done < <(instance_dirs)
else
  check_instance "$EXAMPLE" "" false
fi

exit $((failures > 0))
