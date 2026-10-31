#!/usr/bin/env bash
# Evaluates the child ApplicationSet templates of the instance-manager chart
# for hand-written catalogue entries and checks the chart source each
# generated Application gets. Argo CD renders these templates with Go
# templates plus the sprig functions, the same engine Helm's `tpl` uses, so
# the placeholders can be evaluated offline. Needs helm, yq and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="${1:-$ROOT/argo-cd/apps/instance-manager}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

GIT=https://git.example/repo.git
REGISTRY=ghcr.io/example/charts

# render_sources <scope> <elements-json> [<helm args>...] -> one compact JSON
# object per element: the first source of the Application it generates,
# without the empty fields (an empty field is the same as an unset one to Argo
# CD). The helm arguments set the catalogs of the instance.
render_sources() {
  local scope="$1" elements="$2"
  shift 2
  helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" "$@" >"$work/appsets.yaml" || return 1
  yq ea "select(.kind == \"ApplicationSet\" and .metadata.name == \"$scope-t\")" "$work/appsets.yaml" >"$work/appset.yaml"
  rm -rf "$work/eval" && mkdir -p "$work/eval/templates"
  printf 'apiVersion: v2\nname: eval\nversion: 0.1.0\n' >"$work/eval/Chart.yaml"
  sed -n '/^  template:$/,/^  templatePatch:/p' "$work/appset.yaml" | sed '1d;$d' | sed 's/^    //' >"$work/eval/app.tpl"
  cat >"$work/eval/templates/out.yaml" <<'EOF'
{{- range .Values.elements }}
---
{{ tpl ($.Files.Get "app.tpl") . }}
{{- end }}
EOF
  # Helm's tpl evaluates a missing nested key (.destination.server) to an error
  # where Argo CD's renderer prints nothing, so every element gets an empty
  # destination, the one nested key the templates read.
  elements=$(jq -c 'map(. + {destination: (.destination // {})})' <<<"$elements")
  helm template eval "$work/eval" --set-json "elements=$elements" 2>"$work/eval.err" \
    | yq ea -o=json -I=0 '[.spec.sources[0]]' - 2>/dev/null \
    | jq -c '.[] | to_entries | map(select(.value != "")) | sort_by(.key) | from_entries' 2>/dev/null
  [ -s "$work/eval.err" ] && { head -c 400 "$work/eval.err"; echo; } >&2
  return 0
}

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    echo "ok   $label"
  else
    echo "FAIL $label"
    echo "     expected: $expected"
    echo "     actual:   $actual"
    fail=1
  fi
}


for scope in core tenant; do
  elements='[
    {"app":"plain"},
    {"app":"alias","chart":"other","releaseName":"rel"},
    {"app":"inrepo","chartPath":"utils/helm","targetRevision":"v1"},
    {"app":"ohmlab","catalog":"ohmlab","targetRevision":"0.1.3"},
    {"app":"renamed","catalog":"ohmlab","chart":"ohmlab","targetRevision":"0.1.3","releaseName":"ohmlab"},
    {"app":"unpinned","catalog":"ohmlab"},
    {"app":"stray","catalog":"nope","targetRevision":"1.0.0"}
  ]'
  out=$(render_sources "$scope" "$elements" --set-json "catalogs={\"ohmlab\":\"$REGISTRY\"}")
  line() { printf '%s\n' "$out" | sed -n "${1}p"; }

  check "$scope: plain entry keeps the git path source" \
    '{"helm":{"releaseName":"plain","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/plain.yaml"]},"path":"argo-cd/apps/plain","repoURL":"'"$GIT"'","targetRevision":"main"}' "$(line 1)"
  check "$scope: chart override keeps the git path source" \
    '{"helm":{"releaseName":"rel","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/alias.yaml"]},"path":"argo-cd/apps/other","repoURL":"'"$GIT"'","targetRevision":"main"}' "$(line 2)"
  check "$scope: chartPath and targetRevision overrides keep the git source" \
    '{"helm":{"releaseName":"inrepo","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/inrepo.yaml"]},"path":"utils/helm","repoURL":"'"$GIT"'","targetRevision":"v1"}' "$(line 3)"
  check "$scope: catalog entry becomes an OCI chart source" \
    '{"chart":"ohmlab","helm":{"releaseName":"ohmlab","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/ohmlab.yaml"]},"repoURL":"'"$REGISTRY"'","targetRevision":"0.1.3"}' "$(line 4)"
  check "$scope: catalog entry with an explicit chart name" \
    '{"chart":"ohmlab","helm":{"releaseName":"ohmlab","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/renamed.yaml"]},"repoURL":"'"$REGISTRY"'","targetRevision":"0.1.3"}' "$(line 5)"

  unpinned=$(line 6 | jq -r '.targetRevision // ""')
  case "$unpinned" in
    ""|main|latest|"*") echo "FAIL $scope: a catalog entry without targetRevision must not resolve to '$unpinned'"; fail=1 ;;
    *) echo "ok   $scope: catalog entry without targetRevision fails closed ('$unpinned')" ;;
  esac
  check "$scope: unknown catalog gives no repository" "" "$(line 7 | jq -r ".repoURL // \"\"")"
done

# source <scope> <app> <repoURL> <path> <revision> -> the expected first source
# of an Application of the scope for the app (release name = app).
source_json() {
  printf '{"helm":{"releaseName":"%s","valueFiles":["$values/argo-cd/instances/t/values/%s/%s.yaml"]},"path":"%s","repoURL":"%s","targetRevision":"%s"}' \
    "$2" "$1" "$2" "$4" "$3" "$5"
}

BUNDLE=oci://registry.example/catalog
bundles="{\"bundle\":{\"repoURL\":\"$BUNDLE\",\"version\":\"0.2.0\"},\"unversioned\":{\"repoURL\":\"$BUNDLE\"},\"legacy\":\"$REGISTRY\"}"

# A bundle is a repository URL and a revision with the layout of the git
# repository, so an entry only changes where it is fetched from.
for scope in core tenant; do
  elements='[
    {"app":"gitea","catalog":"bundle"},
    {"app":"alias","chart":"other","catalog":"bundle"},
    {"app":"ohmlab","chartPath":"utils/helm","catalog":"bundle"},
    {"app":"old","catalog":"bundle","targetRevision":"0.1.0"},
    {"app":"floating","catalog":"unversioned"},
    {"app":"pinned","catalog":"unversioned","targetRevision":"0.3.0"},
    {"app":"stray","catalog":"nope"},
    {"app":"legacy","catalog":"legacy","targetRevision":"1.2.3"}
  ]'
  out=$(render_sources "$scope" "$elements" --set-json "catalogs=$bundles")
  line() { printf '%s\n' "$out" | sed -n "${1}p"; }

  check "$scope: a bundle entry takes its repository, path and version from the catalog" \
    "$(source_json "$scope" gitea "$BUNDLE" argo-cd/apps/gitea 0.2.0)" "$(line 1)"
  check "$scope: a bundle entry with a chart override keeps the chart folder name" \
    '{"helm":{"releaseName":"alias","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/alias.yaml"]},"path":"argo-cd/apps/other","repoURL":"'"$BUNDLE"'","targetRevision":"0.2.0"}' "$(line 2)"
  check "$scope: a bundle entry with a chartPath takes that path in the bundle" \
    "$(source_json "$scope" ohmlab "$BUNDLE" utils/helm 0.2.0)" "$(line 3)"
  check "$scope: targetRevision of an entry overrides the version of its catalog" \
    "$(source_json "$scope" old "$BUNDLE" argo-cd/apps/old 0.1.0)" "$(line 4)"
  unversioned=$(line 5 | jq -r '.targetRevision // ""')
  case "$unversioned" in
    ""|main|latest|"*") echo "FAIL $scope: a bundle without version must not resolve to '$unversioned'"; fail=1 ;;
    *) echo "ok   $scope: a bundle without version and an entry without targetRevision fails closed ('$unversioned')" ;;
  esac
  check "$scope: a bundle without version takes the targetRevision of the entry" \
    "$(source_json "$scope" pinned "$BUNDLE" argo-cd/apps/pinned 0.3.0)" "$(line 6)"
  check "$scope: an unknown catalog gives no repository" "" "$(line 7 | jq -r ".repoURL // \"\"")"
  check "$scope: a registry catalog next to bundles is still an exact chart version" \
    '{"chart":"legacy","helm":{"releaseName":"legacy","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/legacy.yaml"]},"repoURL":"'"$REGISTRY"'","targetRevision":"1.2.3"}' "$(line 8)"
done

# defaultCatalog: the catalog of every entry that names none; `git` is the
# repository of the instance itself and opts an entry out of it.
for scope in core tenant; do
  elements='[
    {"app":"plain"},
    {"app":"custom","chart":"other","chartPath":"x/y"},
    {"app":"bumped","targetRevision":"0.1.0"},
    {"app":"mine","catalog":"git"},
    {"app":"minebranch","catalog":"git","targetRevision":"dev"},
    {"app":"legacy","catalog":"legacy","targetRevision":"1.2.3"}
  ]'
  out=$(render_sources "$scope" "$elements" --set-json "catalogs=$bundles" --set defaultCatalog=bundle)
  line() { printf '%s\n' "$out" | sed -n "${1}p"; }

  check "$scope: with a default catalog an entry without catalog comes from it" \
    "$(source_json "$scope" plain "$BUNDLE" argo-cd/apps/plain 0.2.0)" "$(line 1)"
  check "$scope: with a default catalog an entry with chartPath still comes from it" \
    '{"helm":{"releaseName":"custom","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/custom.yaml"]},"path":"x/y","repoURL":"'"$BUNDLE"'","targetRevision":"0.2.0"}' "$(line 2)"
  check "$scope: with a default catalog targetRevision still overrides the version" \
    "$(source_json "$scope" bumped "$BUNDLE" argo-cd/apps/bumped 0.1.0)" "$(line 3)"
  check "$scope: catalog git takes the repository and revision of the instance" \
    "$(source_json "$scope" mine "$GIT" argo-cd/apps/mine main)" "$(line 4)"
  check "$scope: catalog git takes targetRevision of the entry as a git revision" \
    "$(source_json "$scope" minebranch "$GIT" argo-cd/apps/minebranch dev)" "$(line 5)"
  check "$scope: with a default catalog an entry can still name a registry catalog" \
    '{"chart":"legacy","helm":{"releaseName":"legacy","valueFiles":["$values/argo-cd/instances/t/values/'"$scope"'/legacy.yaml"]},"repoURL":"'"$REGISTRY"'","targetRevision":"1.2.3"}' "$(line 6)"

  out=$(render_sources "$scope" '[{"app":"plain"},{"app":"mine","catalog":"git"}]' --set-json "catalogs=$bundles" --set defaultCatalog=git)
  check "$scope: defaultCatalog git is the behaviour without any default" \
    "$(source_json "$scope" plain "$GIT" argo-cd/apps/plain main)" "$(line 1)"
done

# The revision of `git` is the one of the instance, not a fixed branch.
for scope in core tenant; do
  out=$(render_sources "$scope" '[{"app":"plain"},{"app":"mine","catalog":"git"}]' --set targetRevision=release)
  check "$scope: an entry from git follows the targetRevision of the instance" \
    "$(source_json "$scope" plain "$GIT" argo-cd/apps/plain release)" "$(printf '%s\n' "$out" | sed -n 1p)"
  check "$scope: catalog git follows the targetRevision of the instance" \
    "$(source_json "$scope" mine "$GIT" argo-cd/apps/mine release)" "$(printf '%s\n' "$out" | sed -n 2p)"
done

# The names of the instance reach every Application, from git or a registry
# catalog alike, as string Helm parameters the charts read under
# global.ohmlab.names.
for scope in core tenant; do
  out=$(render_sources "$scope" '[{"app":"plain"},{"app":"ohmlab","catalog":"ohmlab","targetRevision":"0.1.3"}]' \
    --set-json "catalogs={\"ohmlab\":\"$REGISTRY\"}" --set-json 'names={"vaultMount":"homelab","gateway":"homelab-gateway","keycloakRealmDisplayName":"Home, lab"}')
  params='[{"name":"global.ohmlab.names.gateway","value":"homelab-gateway","forceString":true},{"name":"global.ohmlab.names.keycloakRealmDisplayName","value":"Home, lab","forceString":true},{"name":"global.ohmlab.names.vaultMount","value":"homelab","forceString":true}]'
  check "$scope: the names of the instance become Helm parameters" "$params" "$(printf '%s\n' "$out" | sed -n 1p | jq -c '.helm.parameters')"
  check "$scope: a registry chart gets the same parameters" "$params" "$(printf '%s\n' "$out" | sed -n 2p | jq -c '.helm.parameters')"
done

# A catalog section that cannot work must fail the render of the manager, not
# produce Applications that quietly come from somewhere else.
refuse() { # label helm-args...
  local label="$1" only; shift
  for only in "" "scopes.tenant.enabled=false" "scopes.core.enabled=false"; do
    if helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" ${only:+--set "$only"} "$@" >/dev/null 2>&1; then
      echo "FAIL $label (${only:-both scopes}): the chart rendered"; fail=1
    else
      echo "ok   $label (${only:-both scopes})"
    fi
  done
}
refuse "a default catalog that is not declared is refused" --set defaultCatalog=nope
refuse "a catalog named git is refused (reserved)" --set-json "catalogs={\"git\":{\"repoURL\":\"$BUNDLE\"}}"
refuse "a bundle catalog without repoURL is refused" --set-json 'catalogs={"bundle":{"version":"0.2.0"}}'
refuse "a name the product does not read is refused" --set-json 'names={"vaultmount":"homelab"}'
refuse "an empty name is refused" --set-json 'names={"gateway":""}'
refuse "a name with a quote is refused" --set-json 'names={"keycloakRealmDisplayName":"My \"lab\""}'
refuse "a name with braces is refused" --set-json 'names={"keycloakRealmDisplayName":"{{ .app }}"}'
helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" --set-json "catalogs=$bundles" --set defaultCatalog=bundle >/dev/null 2>&1 \
  && echo "ok   a declared default catalog renders" || { echo "FAIL a declared default catalog renders"; fail=1; }

exit "$fail"
