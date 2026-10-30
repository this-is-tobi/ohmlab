#!/usr/bin/env bash
# Tests render-instance.sh on throwaway instances and charts (APPS_DIR): what
# it renders, with which release name, namespace and values file, and what it
# refuses. No registry is contacted.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT/scripts/ci/render-instance.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# probe renders only when the release name and namespace are the ones its
# values expect; strict needs a value.
mkdir -p "$work/apps/probe/templates" "$work/apps/strict/templates"
printf 'apiVersion: v2\nname: probe\nversion: 0.1.0\n' >"$work/apps/probe/Chart.yaml"
cat >"$work/apps/probe/templates/cm.yaml" <<'YAML'
{{- if ne .Release.Name .Values.expectRelease }}{{ fail (printf "release %s" .Release.Name) }}{{ end }}
{{- if ne .Release.Namespace .Values.expectNamespace }}{{ fail (printf "namespace %s" .Release.Namespace) }}{{ end }}
apiVersion: v1
kind: ConfigMap
metadata: {name: probe}
YAML
printf 'apiVersion: v2\nname: strict\nversion: 0.1.0\n' >"$work/apps/strict/Chart.yaml"
printf 'apiVersion: v1\nkind: ConfigMap\nmetadata: {name: {{ required "name is required" .Values.name }}}\n' >"$work/apps/strict/templates/cm.yaml"

# instance <name> <instance.yaml extra> <core.yaml apps>: a fresh instance folder.
instance() {
  local d="$work/repo-$1/argo-cd/instances/$1"
  rm -rf "$work/repo-$1"; mkdir -p "$d/values/core" "$d/values/tenant"
  printf 'instance: {name: %s}\n%s\n' "$1" "$2" >"$d/instance.yaml"
  printf 'apps:\n%s\n' "$3" >"$d/core.yaml"
  printf 'apps: []\n' >"$d/tenant.yaml"
  echo "$d"
}
run() { APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$1")" "$SCRIPT" 2>&1; }
expect() { # expect <description> <rc> <text> <output> <rc got>
  if [ "$5" = "$2" ] && grep -qF -- "$3" <<<"$4"; then echo "ok   $1"; else
    echo "FAIL $1 (rc $5, want $2, looking for '$3')"; sed 's/^/     /' <<<"$4"; fail=1
  fi
}

d=$(instance a "" '- {app: probe, enabled: "true"}
- {app: strict, enabled: "false"}')
printf 'expectRelease: probe\nexpectNamespace: probe\n' >"$d/values/core/probe.yaml"
out=$(run "$d"); expect "an enabled app renders with its values; a disabled one is skipped" 0 "1 app(s) rendered, 0 failure(s)" "$out" $?

d=$(instance b "" '- {app: probe, enabled: "true"}')
out=$(run "$d"); expect "a missing values file is refused, as Argo CD does" 1 "FAIL b/core/probe: no values file argo-cd/instances/b/values/core/probe.yaml" "$out" $?

d=$(instance c "" '- {app: strict, enabled: "true"}')
printf 'other: x\n' >"$d/values/core/strict.yaml"
out=$(run "$d"); expect "a chart that fails with the instance's values is reported" 1 "name is required" "$out" $?

d=$(instance e "namespacePrefix: pre-
namespaceSuffix: -suf" '- {app: probe, enabled: "true"}')
printf 'expectRelease: probe\nexpectNamespace: pre-probe-suf\n' >"$d/values/core/probe.yaml"
out=$(run "$d"); expect "the namespace is <prefix><app><suffix> by default" 0 "ok   e/core/probe" "$out" $?

d=$(instance f "" '- {app: other, chart: probe, enabled: "true", releaseName: rel, namespace: ns, valuesPath: argo-cd/instances/f/custom.yaml}')
printf 'expectRelease: rel\nexpectNamespace: ns\n' >"$d/custom.yaml"
out=$(run "$d"); expect "chart, releaseName, namespace and valuesPath are those of the entry" 0 "ok   f/core/other" "$out" $?

d=$(instance g "catalogs:
  reg: registry.example/charts" '- {app: probe, enabled: "true", catalog: reg}')
printf 'expectRelease: probe\nexpectNamespace: probe\n' >"$d/values/core/probe.yaml"
out=$(run "$d"); expect "a registry entry without its chart version is refused" 1 "needs its exact chart version in targetRevision" "$out" $?

d=$(instance h "catalogs:
  bundle: {repoURL: https://example.com/x.git, version: main}
defaultCatalog: bundle" '- {app: probe, enabled: true}')
printf 'expectRelease: probe\nexpectNamespace: probe\n' >"$d/values/core/probe.yaml"
out=$(run "$d"); expect "a bundle catalog renders from APPS_DIR; a boolean enabled counts" 0 "1 app(s) rendered, 0 failure(s)" "$out" $?

exit "$fail"
