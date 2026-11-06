#!/usr/bin/env bash
# Every app an instance enables must render with that instance's values, the
# way its Application renders it: the chart its catalog gives (this
# repository's argo-cd/apps for the git and bundle catalogs, the pinned chart
# for a registry catalog), its release name and namespace, its values file,
# which Argo CD requires to exist, and the Helm parameters its manager gives. A change here that breaks an instance's
# values then fails before it reaches that instance's cluster.
#
# Like Argo CD, each app renders with its charts' crds/ directories, and with
# the API versions of every CRD the instance's own apps install, as on a
# cluster where the instance has synced once: a first pass collects those
# CRDs, the second renders and records. Objects a chart renders only when
# their API exists (VaultSeeds, ServiceMonitors) are then checked too.
#
#   render-instance.sh
#
# Needs helm and yq (mikefarah). APPS_DIR (default argo-cd/apps of this
# repository) is where the git and bundle catalogs' charts are read; registry
# charts are pulled anonymously. With RENDER_OUT set, each app's objects are
# recorded in RENDER_OUT/<instance>/<tier>/<app>.objects, one line per object:
# kind/namespace/name and a hash of its content (never the content itself,
# some objects are Secrets), for compare-instance-renders.sh, and the objects
# themselves in <app>.yaml, a Secret's values blanked, for the checks that read
# them.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
: "${APPS_DIR:=$ROOT/argo-cd/apps}"
manager_chart="$ROOT/argo-cd/apps/instance-manager"
[ -d "$APPS_DIR/instance-manager" ] && manager_chart="$APPS_DIR/instance-manager"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Anonymous, like the cluster: no registry credentials of the caller.
export HELM_REGISTRY_CONFIG="$work/registry.json" HELM_CACHE_HOME="$work/cache"

failures=0
rendered=0
fail() { echo "FAIL $1"; failures=$((failures + 1)); }

# record <manifest> <release namespace> <file>: the identity and content hash
# of every object of a rendered manifest, sorted. Values a chart generates at
# every render are left out of the hash so that only real changes show: the
# values of a Secret (its keys still count) and checksum/* annotations.
record() {
  mkdir -p "$(dirname "$3")"
  yq -o=json -I=0 'select(. != null and .kind != null)' "$1" \
    | jq -r --arg ns "$2" '
        def blank: if type == "object" then map_values("") else . end;
        def nochecksums: if type == "object" then with_entries(select(.key | startswith("checksum/") | not)) else . end;
        (if .kind == "Secret" then (.data |= blank) | (.stringData |= blank) else . end)
        | (if .metadata.annotations then .metadata.annotations |= nochecksums else . end)
        | (if .spec.template.metadata.annotations then .spec.template.metadata.annotations |= nochecksums else . end)
        | "\(.kind)/\(.metadata.namespace // $ns)/\(.metadata.name)\t\(tojson)"' \
    | while IFS=$'\t' read -r id object; do
        printf '%s %s\n' "$id" "$(printf '%s' "$object" | shasum -a 256 | cut -c1-16)"
      done | sort >"$3"
}

# keep <manifest> <file>: the objects of a rendered manifest, a Secret's values
# blanked.
keep() {
  yq 'select(. != null and .kind != null) | (select(.kind == "Secret") | .data[], .stringData[]) = ""' "$1" >"$2"
}

# record_cluster <manifest> <server> <namespace> <app path> <CRD scopes file>:
# what check-shared-clusters.sh reads: the app's destination in
# <app path>.destination, the objects it renders without a namespace in
# <app path>.unnamespaced, and the scopes of its CRDs added to the instance's
# file.
record_cluster() {
  printf '%s %s\n' "$2" "$3" >"$4.destination"
  yq -r 'select(. != null and .kind != null and (.metadata.namespace // "") == "") | .kind + "/" + .metadata.name' "$1" \
    | grep -v '^---$' | sort -u >"$4.unnamespaced"
  yq -r 'select(.kind == "CustomResourceDefinition") | .spec.names.kind + " " + .spec.scope' "$1" | grep -v '^---$' >>"$5"
  sort -u -o "$5" "$5"
}

# crd_api_versions <manifest>...: the API versions the CRDs of the manifests
# serve, as Helm's --api-versions takes them: <group>/<version> and
# <group>/<version>/<Kind> for each served version, sorted and unique.
crd_api_versions() {
  [ "$#" -gt 0 ] || return 0
  # One document at a time (eval, not eval-all, which would pair every
  # group with every kind); yq reads "a | b, c" as "(a | b), c", hence the
  # parentheses around the pair.
  yq e -r 'select(.kind == "CustomResourceDefinition") | .spec.group as $g | .spec.names.kind as $k
    | .spec.versions[] | select(.served != false) | (($g + "/" + .name), ($g + "/" + .name + "/" + $k))' "$@" 2>/dev/null \
    | grep -v -e '^---$' -e '^$' | sort -u
}

# render_apps <instance folder> <collect|check>: every app the instance
# enables, both tiers. collect only writes each manifest to $work/collect, so
# that its CRDs can be read; check renders with the API versions in api,
# reports each app and records it.
render_apps() {
  local dir="$1" mode="$2" instance values_root prefix suffix default_catalog default_server tier lines p
  local app chart release namespace catalog revision values_path chart_path server id values out
  local -a params chart_args
  instance="${dir##*/}"
  # The values repository: Argo CD reads every values file under
  # argo-cd/instances/<instance>/ of it.
  values_root="$(cd "$dir/../../.." && pwd)"
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
  default_catalog=$(yq '.defaultCatalog // "git"' "$dir/instance.yaml")
  default_server=$(yq '.destination.server // "https://kubernetes.default.svc"' "$dir/instance.yaml")
  for tier in core tenant; do
    [ -f "$dir/$tier.yaml" ] || continue
    # The parameters the manager gives every app of the tier, from the
    # manager of APPS_DIR when it has one, so that a base render uses the
    # base's manager.
    params=()
    if ! lines=$(manager_parameters "$dir" "$tier" "$manager_chart" 2>"$work/manager.err"); then
      [ "$mode" = collect ] || fail "$instance/$tier: the instance-manager does not render: $(cat "$work/manager.err")"
      continue
    fi
    while IFS= read -r p; do
      [ -n "$p" ] && params+=(--set-string "$p")
    done <<<"$lines"
    # '|' separates the fields: unlike a tab it is not whitespace to read,
    # so empty fields do not collapse.
    while IFS='|' read -r app chart release namespace catalog revision values_path chart_path server; do
      [ -n "$app" ] || continue
      id="$instance/$tier/$app"
      [ -n "$namespace" ] || namespace="$prefix$app$suffix"
      [ -n "$catalog" ] || catalog="$default_catalog"
      if [ -n "$values_path" ]; then values="$values_root/$values_path"; else values="$dir/values/$tier/$app.yaml"; fi
      if [ ! -f "$values" ]; then
        [ "$mode" = collect ] || fail "$id: no values file ${values#"$values_root"/} (Argo CD refuses a missing value file)"
        continue
      fi
      if ! chart_ref "$dir" "$chart" "$chart_path" "$catalog" "$revision" >"$work/why"; then
        [ "$mode" = collect ] || fail "$id: $(cat "$work/why")"
        continue
      fi
      if [ "$mode" = collect ]; then
        helm template "$release" "${chart_args[@]}" --namespace "$namespace" -f "$values" --include-crds \
          "${params[@]+"${params[@]}"}" >"$work/collect/$tier-$app.yaml" 2>/dev/null || true
        continue
      fi
      if out=$(helm template "$release" "${chart_args[@]}" --namespace "$namespace" -f "$values" --include-crds \
          "${api[@]+"${api[@]}"}" "${params[@]+"${params[@]}"}" 2>&1 >"$work/manifest"); then
        echo "ok   $id"
        rendered=$((rendered + 1))
        if [ -n "${RENDER_OUT:-}" ]; then
          record "$work/manifest" "$namespace" "$RENDER_OUT/$id.objects"
          keep "$work/manifest" "$RENDER_OUT/$id.yaml"
          record_cluster "$work/manifest" "${server:-$default_server}" "$namespace" "$RENDER_OUT/$id" "$RENDER_OUT/$instance/crd-scopes"
        fi
      else
        fail "$id: $(grep -m1 -i 'error' <<<"$out" | cut -c1-240)"
      fi
    done < <(yq -r '.apps[] | select(.enabled == "true" or .enabled == true)
      | [.app, (.chart // .app), (.releaseName // .app), (.namespace // ""), (.catalog // ""), (.targetRevision // ""), (.valuesPath // ""), (.chartPath // ""), (.destination.server // "")]
      | join("|")' "$dir/$tier.yaml")
  done
}

while IFS= read -r dir; do
  rm -rf "$work/collect" && mkdir -p "$work/collect"
  render_apps "$dir" collect
  manifests=("$work"/collect/*.yaml)
  [ -e "${manifests[0]}" ] || manifests=()
  api=()
  while IFS= read -r v; do
    [ -n "$v" ] && api+=(--api-versions "$v")
  done < <(crd_api_versions "${manifests[@]+"${manifests[@]}"}")
  render_apps "$dir" check
done < <(instance_dirs)

echo "$rendered app(s) rendered, $failures failure(s)"
exit $((failures > 0))
