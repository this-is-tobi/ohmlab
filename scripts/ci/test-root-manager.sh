#!/usr/bin/env bash
# Decision table for the root `manager` ApplicationSet and the AppProjects of
# the bootstrap chart: where the instance-manager chart comes from (git or an
# oci:// bundle), where instances and values are discovered, and which sources
# the projects allow when no list is given. Needs helm and yq; the chart's
# dependencies must be built.
set -uo pipefail

CHART="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/utils/helm}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

SRC=https://git.example/product.git
VAL=https://git.example/instance.git
OCI=oci://registry.example/product/catalog

# render <helm args>... -> the manifests in $work/out.yaml, the error in $work/err
render() { helm template t "$CHART" --namespace argocd-system "$@" >"$work/out.yaml" 2>"$work/err"; }

# manager -> "generator@revision | chart@revision path | values@revision"
manager() {
  yq 'select(.kind == "ApplicationSet" and .metadata.name == "manager") |
    .spec.generators[0].git.repoURL + "@" + .spec.generators[0].git.revision + " | " +
    .spec.template.spec.sources[0].repoURL + "@" + .spec.template.spec.sources[0].targetRevision + " " + .spec.template.spec.sources[0].path + " | " +
    .spec.template.spec.sources[1].repoURL + "@" + .spec.template.spec.sources[1].targetRevision' "$work/out.yaml"
}

# project <name> -> its sourceRepos, comma-separated
project() { yq "select(.kind == \"AppProject\" and .metadata.name == \"$1\") | .spec.sourceRepos | join(\",\")" "$work/out.yaml"; }

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

refuses() { # label text-the-error-must-contain helm-args...
  local label="$1" want="$2"
  shift 2
  if render "$@"; then echo "FAIL $label: the render succeeded"; fail=1
  elif grep -qF -- "$want" "$work/err"; then echo "ok   $label"
  else echo "FAIL $label: the error does not mention '$want'"; sed 's/^/     /' "$work/err"; fail=1
  fi
}

render --set "rootManager.repoURL.sources=$SRC" || { cat "$work/err"; exit 1; }
check "defaults render as before" \
  "$SRC@main | $SRC@main argo-cd/apps/instance-manager | $SRC@main" "$(manager)"
check "defaults: the core project allows the sources repository only" "$SRC" "$(project admin-core)"

render --set "rootManager.repoURL.sources=$SRC" --set "rootManager.repoURL.values=$VAL" || { cat "$work/err"; exit 1; }
check "git sources and git values are split" \
  "$VAL@main | $SRC@main argo-cd/apps/instance-manager | $VAL@main" "$(manager)"

render --set "rootManager.repoURL.sources=$SRC" --set "rootManager.repoURL.values=$VAL" \
  --set rootManager.sourcesRevision=v1.2.3 --set rootManager.targetRevision=prod || { cat "$work/err"; exit 1; }
check "git sources honour sourcesRevision" \
  "$VAL@prod | $SRC@v1.2.3 argo-cd/apps/instance-manager | $VAL@prod" "$(manager)"

render --set "rootManager.repoURL.sources=$OCI" --set "rootManager.repoURL.values=$VAL" \
  --set rootManager.sourcesRevision=0.8.0 || { cat "$work/err"; exit 1; }
check "an oci source becomes repoURL oci, path argo-cd/apps/instance-manager, targetRevision sourcesRevision" \
  "$VAL@main | $OCI@0.8.0 argo-cd/apps/instance-manager | $VAL@main" "$(manager)"

refuses "an oci source without sourcesRevision fails" "rootManager.sourcesRevision" \
  --set "rootManager.repoURL.sources=$OCI" --set "rootManager.repoURL.values=$VAL"
refuses "an oci source without values fails" "rootManager.repoURL.values" \
  --set "rootManager.repoURL.sources=$OCI" --set rootManager.sourcesRevision=0.8.0

render --set "rootManager.repoURL.sources=$OCI" --set "rootManager.repoURL.values=$VAL" \
  --set rootManager.sourcesRevision=0.8.0 \
  --set 'projects.core.extraSourceRepos[0]=ghcr.io/example/charts' \
  --set "projects.core.extraSourceRepos[1]=$VAL" || { cat "$work/err"; exit 1; }
check "derived project sources are distinct, keep the oci scheme and include extraSourceRepos" \
  "$OCI,$VAL,ghcr.io/example/charts" "$(project admin-core)"
check "derived project sources apply to the tenant project too" "$OCI,$VAL" "$(project admin-tenant)"

render --set "rootManager.repoURL.sources=$SRC" --set 'projects.core.sourceRepos[0]=https://only.example/repo.git' || { cat "$work/err"; exit 1; }
check "an explicit list is kept as written" "https://only.example/repo.git" "$(project admin-core)"

exit "$fail"
