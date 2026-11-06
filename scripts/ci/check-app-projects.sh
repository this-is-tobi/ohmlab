#!/usr/bin/env bash
# Every Application an instance enables is allowed by the AppProject it names:
# Argo CD refuses one whose destination (server and namespace) no destination
# of its project matches, so an instance on another cluster, or with a
# namespace prefix, needs a project that allows exactly that.
#
# The projects are those the ohmlab app renders, with the values of the
# instance that enables it; without such an instance, nothing is checked. An
# Application is <instance>-<app> of an enabled entry: its project is the
# instance's projects.<tier> (default admin-<tier>), its server the entry's
# destination.server, else the instance's, else the in-cluster one, its
# namespace the entry's, else <namespacePrefix><app><namespaceSuffix>, as the
# instance-manager sets them. Destination patterns are matched as Argo CD's
# globs (* and ?); a pattern starting with ! is reported, not read.
#
# The same render's root manager must find its chart: it reads
# argo-cd/apps/instance-manager from rootManager.repoURL.sources, so when that
# is the instances' own repository (their repoURL), the repository must hold
# the chart, which a repository of instances alone does not.
#
#   check-app-projects.sh
#   check-app-projects.sh --match <projects.json> <apps.json>
#
# projects.json: [{name, destinations: [{server, namespace}]}]; apps.json:
# [{id, project, server, namespace}]. Prints `ok   <id>` or `FAIL <id>: ...`,
# never a render; exits with the number of failures, at most 255 (an exit
# status is taken modulo 256). Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
IN_CLUSTER=https://kubernetes.default.svc

# match <projects.json> <apps.json>: one line per Application.
match() {
  jq -r --slurpfile projects "$1" '
    def rx: "^" + (gsub("(?<c>[.+^$(){}|\\[\\]\\\\])"; "\\\(.c)") | gsub("\\*"; ".*") | gsub("\\?"; ".")) + "$";
    def allows($a): . as $d
      | ((($d.server // "") as $s | $s != "" and ($a.server | test($s | rx))) or ($d.name // "") == "*")
        and ($a.namespace | test(($d.namespace // "") | rx));
    .[] as $a
    | ($projects[0] | map(select(.name == $a.project)) | first) as $p
    | if $p == null then "FAIL \($a.id): project \($a.project) does not exist"
      elif any($p.destinations[]?; ((.server // "") | startswith("!")) or ((.namespace // "") | startswith("!")))
        then "FAIL \($a.id): project \($a.project) has a destination starting with !, which this check does not read"
      elif any($p.destinations[]?; allows($a)) then "ok   \($a.id)"
      else "FAIL \($a.id): project \($a.project) allows no destination \($a.server) \($a.namespace)" end' "$2"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if [ "${1:-}" = "--match" ]; then
  match "$2" "$3" >"$work/out"
  cat "$work/out"
  failures=$(grep -c '^FAIL' "$work/out")
  exit $((failures > 255 ? 255 : failures))
fi

failures=0
printf '[]\n' >"$work/projects.json"
printf '[]\n' >"$work/apps.json"
found=""

while IFS= read -r dir; do
  instance="${dir##*/}"
  # The projects of the instance that enables the ohmlab app.
  entry=$(yq -o=json -I=0 '.apps[] | select(.app == "ohmlab" and (.enabled == "true" or .enabled == true))' "$dir/core.yaml" 2>/dev/null | head -n1)
  if [ -n "$entry" ]; then
    found=yes
    id="$instance/core/ohmlab"
    values=$(jq -r '.valuesPath // ""' <<<"$entry")
    if [ -n "$values" ]; then values="$dir/../../../$values"; else values="$dir/values/core/ohmlab.yaml"; fi
    args=()
    if ! chart_ref "$dir" "$(jq -r '.chart // .app' <<<"$entry")" "$(jq -r '.chartPath // ""' <<<"$entry")" \
        "$(jq -r '.catalog // ""' <<<"$entry")" "$(jq -r '.targetRevision // ""' <<<"$entry")" >"$work/why"; then
      echo "FAIL $id: $(cat "$work/why")"; failures=$((failures + 1))
    elif ! app_args "$values" 2>"$work/err"; then
      echo "FAIL $id: the instance's manager does not render: $(head -c 240 "$work/err")"; failures=$((failures + 1))
    elif ! helm template "$(jq -r '.releaseName // .app' <<<"$entry")" "${chart_args[@]}" \
        --namespace "$(jq -r '.namespace // "argocd-system"' <<<"$entry")" "${args[@]}" >"$work/manifest" 2>"$work/err"; then
      echo "FAIL $id: $(grep -m1 Error "$work/err" | cut -c1-240)"; failures=$((failures + 1))
    else
      yq -o=json -I=0 'select(.kind == "AppProject") | {"name": .metadata.name, "destinations": (.spec.destinations // [])}' "$work/manifest" \
        | jq -s -c --slurpfile have "$work/projects.json" '$have[0] + .' >"$work/more.json" && mv "$work/more.json" "$work/projects.json"
      chart_repo=$(yq 'select(.kind == "ApplicationSet") | (.spec.template.spec.sources // [])[] | select(.path == "argo-cd/apps/instance-manager") | .repoURL' "$work/manifest" | head -n1)
      if [ -n "$chart_repo" ] && [ "$chart_repo" = "$(yq '.repoURL // ""' "$dir/instance.yaml")" ] && [ ! -f "$dir/../../apps/instance-manager/Chart.yaml" ]; then
        echo "FAIL $id: the root manager reads argo-cd/apps/instance-manager from $chart_repo, the instances' own repository, which holds no such chart: set rootManager.repoURL.sources to the product (its git repository or an oci:// catalog) and rootManager.repoURL.values to this repository"
        failures=$((failures + 1))
      fi
    fi
  fi
  # Its Applications.
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
  server=$(yq '.destination.server // ""' "$dir/instance.yaml")
  for tier in core tenant; do
    [ -f "$dir/$tier.yaml" ] || continue
    project=$(T="$tier" yq '.projects[strenv(T)] // ("admin-" + strenv(T))' "$dir/instance.yaml")
    yq -o=json -I=0 '.apps[] | select(.enabled == "true" or .enabled == true)' "$dir/$tier.yaml" \
      | jq -c --arg i "$instance" --arg t "$tier" --arg p "$project" --arg pre "$prefix" --arg suf "$suffix" \
          --arg s "${server:-$IN_CLUSTER}" \
          '{id: "\($i)/\($t)/\(.app)", project: $p, server: (.destination.server // $s), namespace: (.namespace // "\($pre)\(.app)\($suf)")}' \
      | jq -s -c --slurpfile have "$work/apps.json" '$have[0] + .' >"$work/more.json" && mv "$work/more.json" "$work/apps.json"
  done
done < <(instance_dirs)

if [ -z "$found" ]; then
  echo "ok   projects not checked: no instance enables the ohmlab app"
  exit $((failures > 255 ? 255 : failures))
fi
match "$work/projects.json" "$work/apps.json" >"$work/out"
cat "$work/out"
failures=$((failures + $(grep -c '^FAIL' "$work/out")))
exit $((failures > 255 ? 255 : failures))
