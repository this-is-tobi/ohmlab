#!/usr/bin/env bash
# Every app an instance enables must render with that instance's values, the
# way its Application renders it: the chart its catalog gives (this
# repository's argo-cd/apps for the git and bundle catalogs, the pinned chart
# for a registry catalog), its release name and namespace, and its values file,
# which Argo CD requires to exist. A change here that breaks an instance's
# values then fails before it reaches that instance's cluster.
#
#   render-instance.sh
#
# Needs helm and yq (mikefarah). APPS_DIR (default argo-cd/apps of this
# repository) is where the git and bundle catalogs' charts are read; registry
# charts are pulled anonymously.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
: "${APPS_DIR:=$ROOT/argo-cd/apps}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Anonymous, like the cluster: no registry credentials of the caller.
export HELM_REGISTRY_CONFIG="$work/registry.json" HELM_CACHE_HOME="$work/cache"

failures=0
rendered=0
fail() { echo "FAIL $1"; failures=$((failures + 1)); }

while IFS= read -r dir; do
  instance="${dir##*/}"
  # The values repository: Argo CD reads every values file under
  # argo-cd/instances/<instance>/ of it.
  values_root="$(cd "$dir/../../.." && pwd)"
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
  default_catalog=$(yq '.defaultCatalog // "git"' "$dir/instance.yaml")
  for tier in core tenant; do
    [ -f "$dir/$tier.yaml" ] || continue
    # '|' separates the fields: unlike a tab it is not whitespace to read,
    # so empty fields do not collapse.
    while IFS='|' read -r app chart release namespace catalog revision values_path; do
      [ -n "$app" ] || continue
      id="$instance/$tier/$app"
      [ -n "$namespace" ] || namespace="$prefix$app$suffix"
      [ -n "$catalog" ] || catalog="$default_catalog"
      if [ -n "$values_path" ]; then values="$values_root/$values_path"; else values="$dir/values/$tier/$app.yaml"; fi
      if [ ! -f "$values" ]; then
        fail "$id: no values file ${values#"$values_root"/} (Argo CD refuses a missing value file)"
        continue
      fi
      registry=$(C="$catalog" yq '.catalogs[strenv(C)] | select(tag == "!!str")' "$dir/instance.yaml")
      if [ -n "$registry" ]; then
        if [ -z "$revision" ]; then
          fail "$id: catalog $catalog is a registry, the entry needs its exact chart version in targetRevision"
          continue
        fi
        ref=(oci://"$registry/$chart" --version "$revision")
      else
        ref=("$APPS_DIR/$chart")
      fi
      if out=$(helm template "$release" "${ref[@]}" --namespace "$namespace" -f "$values" 2>&1 >/dev/null); then
        echo "ok   $id"
        rendered=$((rendered + 1))
      else
        fail "$id: $(grep -m1 -i 'error' <<<"$out" | cut -c1-240)"
      fi
    done < <(yq -r '.apps[] | select(.enabled == "true" or .enabled == true)
      | [.app, (.chart // .app), (.releaseName // .app), (.namespace // ""), (.catalog // ""), (.targetRevision // ""), (.valuesPath // "")]
      | join("|")' "$dir/$tier.yaml")
  done
done < <(instance_dirs)

echo "$rendered app(s) rendered, $failures failure(s)"
exit $((failures > 0))
