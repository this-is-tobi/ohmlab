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
# CD) and, unless KEEP_PARAMS=true, without its Helm parameters (render_params
# checks those). The helm arguments set the catalogs of the instance.
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
    | jq -c --argjson keep "${KEEP_PARAMS:-false}" '.[] | (if $keep then . else del(.helm.parameters) end) | to_entries | map(select(.value != "")) | sort_by(.key) | from_entries' 2>/dev/null
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

# render_params <scope> <instance> [<helm args>...] -> the Helm parameters of
# the scope's ApplicationSet template, compact JSON. They are literal, the same
# for every Application of the scope.
render_params() {
  local scope="$1" inst="$2"
  shift 2
  helm template t "$CHART" --set "instance.name=$inst" --set "repoURL=$GIT" "$@" \
    | yq ea -o=json -I=0 "select(.kind == \"ApplicationSet\" and .metadata.name == \"$scope-$inst\") | .spec.template.spec.sources[0].helm.parameters" -
}
# render_names and render_vault: render_params, only the names, or only the Vault
# parameters (global.ohmlab.names, global.ohmlab.vault).
render_names() { render_params "$@" | jq -c 'map(select(.name | startswith("global.ohmlab.names.")))'; }
render_vault() { render_params "$@" | jq -c 'map(select(.name | startswith("global.ohmlab.vault.")))'; }

# names_json <gateway> <keycloakRealm> <keycloakRealmDisplayName> <vaultMount>
# -> the parameters the manager gives for these names, sorted by key.
names_json() {
  jq -nc --arg g "$1" --arg r "$2" --arg d "$3" --arg m "$4" '[
    {name: "global.ohmlab.names.gateway", value: $g, forceString: true},
    {name: "global.ohmlab.names.keycloakRealm", value: $r, forceString: true},
    {name: "global.ohmlab.names.keycloakRealmDisplayName", value: $d, forceString: true},
    {name: "global.ohmlab.names.vaultMount", value: $m, forceString: true}]'
}

# The names of the instance reach every Application, from git or a registry
# catalog alike, as string Helm parameters the charts read under
# global.ohmlab.names. Each derives from the instance's name unless the
# instance sets it.
for scope in core tenant; do
  out=$(KEEP_PARAMS=true render_sources "$scope" '[{"app":"plain"},{"app":"ohmlab","catalog":"ohmlab","targetRevision":"0.1.3"}]' \
    --set-json "catalogs={\"ohmlab\":\"$REGISTRY\"}" --set-json 'names={"vaultMount":"homelab","gateway":"homelab-gateway","keycloakRealmDisplayName":"Home, lab"}')
  params=$(names_json homelab-gateway t "Home, lab" homelab)
  check "$scope: the names of the instance become Helm parameters" "$params" "$(printf '%s\n' "$out" | sed -n 1p | jq -c '.helm.parameters | map(select(.name | startswith("global.ohmlab.names.")))')"
  check "$scope: a registry chart gets the same parameters" "$params" "$(printf '%s\n' "$out" | sed -n 2p | jq -c '.helm.parameters | map(select(.name | startswith("global.ohmlab.names.")))')"
  check "$scope: a registry chart gets the same Vault parameters" \
    "$(printf '%s\n' "$out" | sed -n 1p | jq -c '.helm.parameters | map(select(.name | startswith("global.ohmlab.vault.")))')" \
    "$(printf '%s\n' "$out" | sed -n 2p | jq -c '.helm.parameters | map(select(.name | startswith("global.ohmlab.vault.")))')"
  check "$scope: the names derive from the instance name" \
    "$(names_json my-lab-gateway my-lab My-Lab my-lab)" "$(render_names "$scope" my-lab)"
  check "$scope: a name of the instance overrides only its key" \
    "$(names_json edge t T t)" "$(render_names "$scope" t --set-json 'names={"gateway":"edge"}')"
done

# vault_json <mount> [<path prefix>] -> the Vault parameters the manager gives.
vault_json() {
  jq -nc --arg m "$1" --arg p "${2:-}" '[{name: "global.ohmlab.vault.mount", value: $m, forceString: true}]
    + (if $p == "" then [] else [{name: "global.ohmlab.vault.pathPrefix", value: $p, forceString: true}] end)'
}

# Each app reads its secrets from its tier's KV mount: the core apps from the
# ohmlab system mount, the tenant apps from the instance's, under
# platforms/<instance.env>/<scope>.
check "core: the core apps read the system mount, under platforms/<env>/core" \
  "$(vault_json ohmlab platforms/production/core)" "$(render_vault core t --set instance.env=production)"
check "tenant: the tenant apps read the instance's mount, under platforms/<env>/tenant" \
  "$(vault_json t platforms/production/tenant)" "$(render_vault tenant t --set instance.env=production)"
check "tenant: the instance's mount follows names.vaultMount" \
  "$(vault_json lab platforms/production/tenant)" "$(render_vault tenant t --set instance.env=production --set-json 'names={"vaultMount":"lab"}')"
check "core: names.vaultMount leaves the core apps on the system mount" \
  "$(vault_json ohmlab platforms/production/core)" "$(render_vault core t --set instance.env=production --set-json 'names={"vaultMount":"lab"}')"
check "without instance.env, no path prefix" "$(vault_json ohmlab)" "$(render_vault core t)"
check "the Vault parameters come after the names" \
  "global.ohmlab.names.gateway,global.ohmlab.names.keycloakRealm,global.ohmlab.names.keycloakRealmDisplayName,global.ohmlab.names.vaultMount,global.ohmlab.vault.mount,global.ohmlab.vault.pathPrefix" \
  "$(render_params core t --set instance.env=production | jq -r 'map(.name) | join(",")')"

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
refuse "traefikGateway is no longer a name" --set-json 'names={"traefikGateway":"x"}'
refuse "an instance name that cannot name a Gateway is refused" --set instance.name=My_Lab
refuse "an instance name too long for its Gateway name is refused" --set instance.name=a2345678901234567890123456789012345678901234567890123456

# refuse_msg <label> <message> <helm args...>: the render fails and its error
# says why.
refuse_msg() {
  local label="$1" message="$2" err
  shift 2
  if err=$(helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" "$@" 2>&1 >/dev/null); then
    echo "FAIL $label: the chart rendered"; fail=1
  elif grep -qF -- "$message" <<<"$err"; then
    echo "ok   $label"
  else
    echo "FAIL $label: expected '$message' in: $(grep -m1 Error <<<"$err" | cut -c1-240)"; fail=1
  fi
}
# accept <label> <helm args...>: the render succeeds.
accept() {
  local label="$1"
  shift
  if helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" "$@" >/dev/null 2>&1; then echo "ok   $label"; else
    echo "FAIL $label: the chart did not render"; fail=1
  fi
}
refuse_msg "an instance name that cannot name a Gateway says which" 'instance.name "My_Lab"' --set instance.name=My_Lab
accept "an instance name of 55 characters is accepted" --set instance.name=a234567890123456789012345678901234567890123456789012345
check "a numeric instance name keeps its digits" "$(names_json 2024-gateway 2024 2024 2024)" "$(render_names core 2024)"
refuse_msg "an instance.env that cannot be a path segment is refused" 'instance.env "Prod/1" must match' --set instance.env=Prod/1
refuse_msg "an instance named after Keycloak's admin realm is refused" \
  'instance.name "master" gives names.keycloakRealm "master", which is reserved by Keycloak' --set instance.name=master
refuse_msg "an instance named after a Vault system mount is refused" \
  'instance.name "sys" gives names.vaultMount "sys", which is reserved by Vault' --set instance.name=sys
refuse_msg "a realm named master is refused" 'names.keycloakRealm "master" is reserved by Keycloak' --set-json 'names={"keycloakRealm":"master"}'
refuse_msg "a mount under a Vault system path is refused" 'names.vaultMount "auth/x" is reserved by Vault' --set-json 'names={"vaultMount":"auth/x"}'
refuse_msg "a gateway that is not a DNS label is refused" 'names.gateway "Edge_GW" must match' --set-json 'names={"gateway":"Edge_GW"}'
refuse_msg "a realm with a slash is refused" 'names.keycloakRealm "a/b" must match' --set-json 'names={"keycloakRealm":"a/b"}'
refuse_msg "a name with a control character is refused" 'names.keycloakRealmDisplayName cannot contain' --set-json 'names={"keycloakRealmDisplayName":"a\nb"}'
accept "a nested Vault mount is accepted" --set-json 'names={"vaultMount":"team/lab"}'
helm template t "$CHART" --set instance.name=t --set "repoURL=$GIT" --set-json "catalogs=$bundles" --set defaultCatalog=bundle >/dev/null 2>&1 \
  && echo "ok   a declared default catalog renders" || { echo "FAIL a declared default catalog renders"; fail=1; }

exit "$fail"
