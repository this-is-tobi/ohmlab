# shellcheck shell=bash
# Where the instance folders are, for the checks that render charts with an
# instance's values. INSTANCES_DIR is a directory laid out like
# argo-cd/instances, for an instance kept in a repository of its own; it
# defaults to this repository's. The documented _example always comes from this
# repository. The caller sets ROOT, the repository root.

: "${INSTANCES_DIR:=$ROOT/argo-cd/instances}"
INSTANCES_DIR="${INSTANCES_DIR%/}"
EXAMPLE_DIR="$ROOT/argo-cd/instances/_example"

# instance_dirs: the real instance folders of INSTANCES_DIR (a leading `_`
# marks a template), one per line, sorted.
instance_dirs() {
  local d
  for d in "$INSTANCES_DIR"/*/; do
    [ -d "$d" ] || continue
    d="${d%/}"
    case "${d##*/}" in _*) continue ;; esac
    printf '%s\n' "$d"
  done
}

# values_file <app>: the values file production would render the app with, the
# first found among the real instances and then the example; nothing when none
# has one.
values_file() {
  local d scope
  while IFS= read -r d; do
    for scope in core tenant; do
      if [ -f "$d/values/$scope/$1.yaml" ]; then printf '%s\n' "$d/values/$scope/$1.yaml"; return; fi
    done
  done < <(instance_dirs; printf '%s\n' "$EXAMPLE_DIR")
}

# manager_parameters <instance dir> <scope> [<manager chart>]: the Helm
# parameters the instance's manager gives every app of the scope, one
# `name=value` per line, commas escaped for --set-string. They come from a
# render of the instance-manager chart (default this repository's) with the
# instance's instance.yaml, so a check renders an app the way its Application
# does without copying the manager's rules. The instance name is the folder's,
# as the root manager passes it, without the leading `_` of a template. When
# the manager does not render, prints its reason on stderr and returns 1.
manager_parameters() {
  local rendered
  rendered=$(manager_render "$1" "${3:-}") || return 1
  parameters_of "$(instance_name "$1")" "$2" <<<"$rendered"
}

# instance_name <instance dir>: the name the root manager gives the instance,
# its folder's without the leading `_` of a template.
instance_name() {
  local name="${1%/}"
  name="${name##*/}"
  printf '%s\n' "${name#_}"
}

# manager_render <instance dir> [<manager chart>]: the instance-manager chart
# (default this repository's) rendered with the instance's instance.yaml. When
# it does not render, prints the reason on stderr and returns 1.
manager_render() {
  local dir="$1" chart="${2:-$ROOT/argo-cd/apps/instance-manager}" rendered err
  err=$(mktemp)
  if ! rendered=$(helm template manager "$chart" -f "$dir/instance.yaml" --set "instance.name=$(instance_name "$dir")" 2>"$err"); then
    sed -n 's/^Error: //p' "$err" | head -n1 | sed -E 's/^execution error at \([^)]*\): //' | cut -c1-240 >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
  printf '%s\n' "$rendered"
}

# parameters_of <instance name> <scope>: manager_parameters' lines, from a
# manager render on stdin.
parameters_of() {
  N="$2-$1" yq ea -r 'select(.kind == "ApplicationSet" and .metadata.name == strenv(N))
    | .spec.template.spec.sources[0].helm.parameters // [] | .[] | .name + "=" + (.value | sub(","; "\\,"))' -
}

# storage_values <instance dir> <scope> [<manager chart>]: the inline values
# the instance's manager merges into each app of the scope (its storage values,
# from the storage block), as one JSON object by app; {} without a storage
# block. When the manager does not render, prints its reason on stderr and
# returns 1.
storage_values() {
  local rendered
  rendered=$(manager_render "$1" "${3:-}") || return 1
  storage_of "$(instance_name "$1")" "$2" <<<"$rendered"
}

# storage_of <instance name> <scope>: storage_values' object, from a manager
# render on stdin: the elements of the list the tier's ApplicationSet merges
# with its apps.
storage_of() {
  N="$2-$1" yq ea -o=json -I=0 'select(.kind == "ApplicationSet" and .metadata.name == strenv(N))
    | .spec.generators[0].merge.generators[1].list.elements // [] | .[]' - \
    | jq -sc 'map({(.app): .storageValues}) | add // {}'
}

# storage_sets <storage values> <app>: the app's part of storage_values' object
# as --set-json arguments, one path=json per line: each leaf of its maps and
# each list whole, which merges into the values before them as Argo CD merges
# an Application's inline values into its values files. Helm applies
# --set-json after the files and before --set-string, the manager's
# parameters. Nothing when the app has no storage values.
storage_sets() {
  jq -r --arg app "$2" '.[$app] // empty | . as $v
    | paths(type != "object") | select(all(.[]; type == "string")) as $p
    | ($p | map(gsub("\\."; "\\.")) | join(".")) + "=" + ($v | getpath($p) | tojson)' <<<"$1"
}

# app_args <values file> [<app>]: sets the array `args` to render an app the
# way its Application does: its values file, the storage values and the Helm
# parameters its instance's manager gives it, read from the path
# (<instance dir>/values/<scope>/<app>.yaml); <app> names the entry when the
# file is not named after it. When the manager does not render, prints its
# reason on stderr and returns 1.
app_args() {
  local vf="$1" dir scope app name rendered line
  dir="${vf%/values/*}"
  scope="${vf#"$dir"/values/}"
  scope="${scope%%/*}"
  app="${2:-$(basename "$vf" .yaml)}"
  args=(-f "$vf")
  rendered=$(manager_render "$dir") || return 1
  name=$(instance_name "$dir")
  while IFS= read -r line; do
    [ -n "$line" ] && args+=(--set-json "$line")
  done < <(storage_sets "$(storage_of "$name" "$scope" <<<"$rendered")" "$app")
  while IFS= read -r line; do
    [ -n "$line" ] && args+=(--set-string "$line")
  done < <(parameters_of "$name" "$scope" <<<"$rendered")
  return 0
}

# chart_ref <instance dir> <chart> <chartPath> <catalog> <targetRevision>:
# sets the array `chart_args` to the chart an entry's Application renders: from a
# registry catalog oci://<registry>/<chart> --version <targetRevision>, else
# the chartPath of the catalog whose argo-cd/apps APPS_DIR is (default this
# repository's), else APPS_DIR/<chart>. The catalog defaults to the instance's
# defaultCatalog. A registry entry without its version prints why and returns 1.
# shellcheck disable=SC2034 # chart_args is the caller's
chart_ref() {
  local dir="$1" chart="$2" chart_path="$3" catalog="$4" revision="$5" apps="${APPS_DIR:-$ROOT/argo-cd/apps}" registry
  [ -n "$catalog" ] || catalog=$(yq '.defaultCatalog // "git"' "$dir/instance.yaml")
  registry=$(C="$catalog" yq '.catalogs[strenv(C)] | select(tag == "!!str")' "$dir/instance.yaml")
  if [ -n "$registry" ]; then
    [ -n "$revision" ] || { echo "catalog $catalog is a registry, the entry needs its exact chart version in targetRevision"; return 1; }
    chart_args=(oci://"$registry/$chart" --version "$revision")
  elif [ -n "$chart_path" ]; then
    chart_args=("$apps/../../$chart_path")
  else
    chart_args=("$apps/$chart")
  fi
}

# vso_render <instance dir> <scope> <entry> <parameters> <namespace> <file>
# [<storage values>]: renders one entry of the instance's <scope>.yaml (JSON)
# into <file> the way its Application does: its chart (chart_ref), its values
# file (valuesPath, else values/<scope>/<app>.yaml when there is one), its
# storage values (from storage_values' object of the scope), the manager's
# parameters for the scope (manager_parameters' lines), its release name and
# namespace. Returns 2 without rendering for a local chart that renders no VSO
# object (no vso-utils dependency, other than vault-operator and ohmlab, which
# write theirs by hand), 1 with the reason on stdout when it does not render.
vso_render() {
  local dir="$1" scope="$2" entry="$3" params="$4" ns="$5" out="$6" storage="${7:-}" chart values p args=()
  local -a chart_args
  chart_ref "$dir" "$(jq -r '.chart // .app' <<<"$entry")" "$(jq -r '.chartPath // ""' <<<"$entry")" \
    "$(jq -r '.catalog // ""' <<<"$entry")" "$(jq -r '.targetRevision // ""' <<<"$entry")" || return 1
  chart="${chart_args[0]}"
  if [ -f "$chart/Chart.yaml" ]; then
    case "${chart##*/}" in
      vault-operator | ohmlab) ;;
      *) grep -q 'name: vso-utils' "$chart/Chart.yaml" || return 2 ;;
    esac
    if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
      helm dependency build "$chart" >/dev/null 2>&1 || { echo "helm dependency build fails"; return 1; }
    fi
  fi
  values=$(jq -r '.valuesPath // ""' <<<"$entry")
  if [ -n "$values" ]; then values="$dir/../../../$values"; else values="$dir/values/$scope/$(jq -r '.app' <<<"$entry").yaml"; fi
  [ -f "$values" ] && args=(-f "$values")
  if [ -n "$storage" ]; then
    while IFS= read -r p; do [ -n "$p" ] && args+=(--set-json "$p"); done < <(storage_sets "$storage" "$(jq -r '.app' <<<"$entry")")
  fi
  while IFS= read -r p; do [ -n "$p" ] && args+=(--set-string "$p"); done <<<"$params"
  if ! helm template "$(jq -r '.releaseName // .app' <<<"$entry")" "${chart_args[@]}" --namespace "$ns" \
      "${args[@]+"${args[@]}"}" >"$out" 2>"$out.err"; then
    grep -m1 Error "$out.err" | cut -c1-240
    return 1
  fi
}
