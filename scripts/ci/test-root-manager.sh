#!/usr/bin/env bash
# Decision table for the root `manager` ApplicationSet and the AppProjects of
# the bootstrap chart: where the instance-manager chart comes from (git or an
# oci:// bundle), where instances and values are discovered, and which sources
# the projects allow when no list is given. Needs helm and yq; the chart's
# dependencies must be built.
set -uo pipefail

CHART="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/charts/ohmlab}"
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

# A remote cluster in `clusters` becomes an Argo CD cluster Secret, written by
# VSO from this Argo CD's own Vault path with the chart's VaultAuth.
VAULT=(--set-json 'global.ohmlab.vault={"mount":"ohmlab","pathPrefix":"platforms/production/core"}')
CLUSTER=(--set clusters.nonprod.server=https://nonprod.example.com:6443)
# vss <query> -> the query on the cluster's VaultStaticSecret
vss() { yq "select(.kind == \"VaultStaticSecret\" and .spec.destination.name == \"cluster-nonprod\") | $1" "$work/out.yaml"; }
render --set "rootManager.repoURL.sources=$SRC" --set vso.enabled=true "${VAULT[@]}" "${CLUSTER[@]}" || { cat "$work/err"; exit 1; }
check "the cluster Secret holds only the keys its template writes" 0 \
  "$("$(dirname "${BASH_SOURCE[0]}")/check-vso-isolation.sh" --stdin <"$work/out.yaml" >/dev/null 2>&1; echo $?)"
check "a cluster becomes a Secret Argo CD reads as a cluster" "cluster-nonprod true cluster" \
  "$(vss '.spec.destination.name + " " + (.spec.destination.create | tostring) + " " + .spec.destination.labels["argocd.argoproj.io/secret-type"]')"
check "the Secret holds the cluster's name and server" "nonprod https://nonprod.example.com:6443" \
  "$(vss '.spec.destination.transformation.templates.name.text + " " + .spec.destination.transformation.templates.server.text')"
check "its values come from this Argo CD's own path" "ohmlab platforms/production/core/argo-cd" "$(vss '.spec.mount + " " + .spec.path')"
check "it logs in with the chart's VaultAuth" \
  "$(yq 'select(.kind == "VaultAuth") | .metadata.name' "$work/out.yaml")" "$(vss '.spec.vaultAuthRef')"
config=$(vss '.spec.destination.transformation.templates.config.text')
check "the config reads clusters.nonprod.bearerToken and .caData" "true" \
  "$(grep -q '"clusters"' <<<"$config" && grep -q '"nonprod"' <<<"$config" && grep -q bearerToken <<<"$config" && grep -q caData <<<"$config" && echo true)"
# The config template, rendered the way VSO does (Go templates and Sprig) with
# fake values, is the JSON Argo CD expects.
mkdir -p "$work/tpl/templates"
printf 'apiVersion: v2\nname: tpl\nversion: 0.0.0\n' >"$work/tpl/Chart.yaml"
printf '%s\n' '{{ tpl .Values.text (dict "Secrets" .Values.secrets "Template" .Template) }}' >"$work/tpl/templates/out.yaml"
printf '%s' "$config" >"$work/config.txt"
check "the config renders to Argo CD's cluster JSON" '{"bearerToken":"t0k","tlsClientConfig":{"caData":"Y2E="}}' \
  "$(helm template tpl "$work/tpl" --set-file "text=$work/config.txt" --set-json 'secrets={"clusters":{"nonprod":{"bearerToken":"t0k","caData":"Y2E="}}}' 2>&1 | grep -v '^---\|^# Source' | jq -cS . 2>&1)"
# Values as an admin stores them: a token with its file's trailing newline,
# the CA wrapped by base64. Without the clusters key, empty values.
check "a token with a trailing newline and a wrapped CA still make valid JSON" '{"bearerToken":"t0k","tlsClientConfig":{"caData":"Y2E=\nY2E="}}' \
  "$(helm template tpl "$work/tpl" --set-file "text=$work/config.txt" --set-json 'secrets={"clusters":{"nonprod":{"bearerToken":"t0k\n","caData":"Y2E=\nY2E=\n"}}}' 2>&1 | grep -v '^---\|^# Source' | jq -cS . 2>&1)"
check "without the clusters key, the config holds empty values" '{"bearerToken":"","tlsClientConfig":{"caData":""}}' \
  "$(helm template tpl "$work/tpl" --set-file "text=$work/config.txt" --set-json 'secrets={"other":"x"}' 2>&1 | grep -v '^---\|^# Source' | jq -cS . 2>&1)"
render --set "rootManager.repoURL.sources=$SRC" "${VAULT[@]}" "${CLUSTER[@]}" || { cat "$work/err"; exit 1; }
check "without VSO, no cluster Secret" "" "$(yq 'select(.kind == "VaultStaticSecret") | .kind' "$work/out.yaml")"

# A project that inherits a tier takes its sources rule, whitelists and
# blacklists, under its own name, destinations and admin role.
# appproject <name> <query> -> the query on that AppProject, as JSON
appproject() { yq -o=json -I=0 "select(.kind == \"AppProject\" and .metadata.name == \"$1\") | $2" "$work/out.yaml"; }
# The tier's blacklists are set so that an inherited one is seen, not null.
BLACKLISTS=(--set-json 'projects.tenant.clusterResourceBlacklist=[{"group":"storage.k8s.io","kind":"StorageClass"}]'
  --set-json 'projects.tenant.namespaceResourceBlacklist=[{"group":"","kind":"ResourceQuota"}]')
render --set "rootManager.repoURL.sources=$SRC" "${BLACKLISTS[@]}" || { cat "$work/err"; exit 1; }
tiers=$(appproject admin-core . && appproject admin-tenant .)
DEV='{"enabled":true,"inherit":"tenant","name":"dev-tenant","destinations":[{"server":"https://nonprod.example.com:6443","namespace":"dev-*"}]}'
render --set "rootManager.repoURL.sources=$SRC" "${BLACKLISTS[@]}" --set-json "projects.dev=$DEV" || { cat "$work/err"; exit 1; }
check "the tier's blacklists are set for this test" '[{"group":"","kind":"ResourceQuota"}]' "$(appproject admin-tenant .spec.namespaceResourceBlacklist)"
for field in sourceRepos clusterResourceWhitelist clusterResourceBlacklist namespaceResourceWhitelist namespaceResourceBlacklist; do
  check "an inheriting project takes the tier's $field" "$(appproject admin-tenant ".spec.$field")" "$(appproject dev-tenant ".spec.$field")"
done
check "an inheriting project keeps its own destinations" '[{"namespace":"dev-*","server":"https://nonprod.example.com:6443"}]' \
  "$(appproject dev-tenant .spec.destinations | jq -cS .)"
check "an inheriting project gets an admin role on itself" '["admin"] ["p, proj:dev-tenant:admin, applications, *, dev-tenant/*, allow"]' \
  "$(appproject dev-tenant '.spec.roles[0].groups') $(appproject dev-tenant '.spec.roles[0].policies')"
check "an inheriting project carries its tier's label" '"tenant"' "$(appproject dev-tenant '.metadata.labels["ohmlab.fr/instance-scope"]')"
check "the tier projects are unchanged" "$tiers" "$(appproject admin-core . && appproject admin-tenant .)"
refuses "a project that inherits no tier fails" "projects.dev.inherit" \
  --set "rootManager.repoURL.sources=$SRC" --set-json "projects.dev=$(jq -c '.inherit = "dev"' <<<"$DEV")"

exit "$fail"
