#!/usr/bin/env bash
# Decision table for scripts/ci/check-app-projects.sh: hand-written projects
# and Applications (--match), then a throwaway instances directory whose hub
# renders the projects through a fixture chart.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-app-projects.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

IN=https://kubernetes.default.svc
NONPROD=https://nonprod.example.com:6443

# match <label> <expected exit> <text the output must hold> <projects JSON> <apps JSON>
match() {
  local out rc
  printf '%s\n' "$4" >"$work/projects.json"
  printf '%s\n' "$5" >"$work/apps.json"
  out=$("$SCRIPT" --match "$work/projects.json" "$work/apps.json" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1 (exit $rc, want $2, looking for '$3')"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  fi
}
# project <name> <server> <namespace> -> one project with one destination
project() { jq -nc --arg n "$1" --arg s "$2" --arg ns "$3" '[{name: $n, destinations: [{server: $s, namespace: $ns}]}]'; }
# app <project> <server> <namespace> -> one Application, id i/tenant/a
app() { jq -nc --arg p "$1" --arg s "$2" --arg ns "$3" '[{id: "i/tenant/a", project: $p, server: $s, namespace: $ns}]'; }

match "an exact server and namespace are allowed" 0 "ok   i/tenant/a" "$(project p "$IN" a)" "$(app p "$IN" a)"
match "a * namespace allows any" 0 "ok   i/tenant/a" "$(project p "$IN" '*')" "$(app p "$IN" a)"
match "dev-* allows dev-gitea" 0 "ok   i/tenant/a" "$(project p "$NONPROD" 'dev-*')" "$(app p "$NONPROD" dev-gitea)"
match "dev-* refuses preprod-gitea" 1 "project p allows no destination $NONPROD preprod-gitea" \
  "$(project p "$NONPROD" 'dev-*')" "$(app p "$NONPROD" preprod-gitea)"
match "another server is refused" 1 "allows no destination $IN a" "$(project p "$NONPROD" '*')" "$(app p "$IN" a)"
match "a * server allows any" 0 "ok   i/tenant/a" "$(project p '*' '*')" "$(app p "$NONPROD" a)"
match "a project that does not exist is refused" 1 "project dev-tenant does not exist" "$(project p "$IN" '*')" "$(app dev-tenant "$IN" a)"
match "a ! destination is reported, as this check does not read it" 1 "starting with !" "$(project p "$IN" '!kube-system')" "$(app p "$IN" a)"
match "a dot in a pattern is a dot" 1 "allows no destination" "$(project p 'https://a.example.com' '*')" "$(app p 'https://aXexample.com' a)"
# An exit status is taken modulo 256: 256 failures must not read as success.
match "256 failures exit 255" 255 "FAIL i/tenant/a256" "$(project p "$IN" a)" \
  "$(jq -nc --arg s "$IN" '[range(1; 257) | {id: "i/tenant/a\(.)", project: "p", server: $s, namespace: "b"}]')"

# The loop: the projects come from the ohmlab entry of the instance that enables
# it (here a chartPath of a fixture catalog), the Applications from every
# instance's enabled entries.
mkdir -p "$work/cat/argo-cd/apps" "$work/cat/charts/boot/templates"
printf 'apiVersion: v2\nname: boot\nversion: 0.1.0\n' >"$work/cat/charts/boot/Chart.yaml"
cat >"$work/cat/charts/boot/templates/projects.yaml" <<YAML
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata: {name: admin-core}
spec:
  destinations: [{server: "$IN", namespace: "*"}]
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata: {name: dev-tenant}
spec:
  destinations: [{server: "$NONPROD", namespace: "dev-*"}]
YAML
inst() { # inst <name> <instance.yaml extra> <core apps> <tenant apps>
  local d="$work/repo/argo-cd/instances/$1"
  mkdir -p "$d/values/core" "$d/values/tenant"
  printf 'instance: {name: %s}\nrepoURL: https://git.example/x.git\n%s\n' "$1" "$2" >"$d/instance.yaml"
  printf 'apps:\n%s\n' "$3" >"$d/core.yaml"
  printf 'apps:\n%s\n' "$4" >"$d/tenant.yaml"
}
inst hub "" '- {app: ohmlab, enabled: "true", namespace: argocd-system, chartPath: charts/boot}' ' []'
printf '{}\n' >"$work/repo/argo-cd/instances/hub/values/core/ohmlab.yaml"
inst dev "destination: {server: $NONPROD}
namespacePrefix: dev-
projects: {tenant: dev-tenant}" ' []' '- {app: gitea, enabled: "true"}
- {app: idle, enabled: "false"}'
inst bad "destination: {server: $NONPROD}
namespacePrefix: preprod-
projects: {tenant: dev-tenant}" ' []' '- {app: gitea, enabled: "true"}'
out=$(APPS_DIR="$work/cat/argo-cd/apps" INSTANCES_DIR="$work/repo/argo-cd/instances" "$SCRIPT" 2>&1); rc=$?
if [ "$rc" = 1 ] && grep -qF 'ok   hub/core/ohmlab' <<<"$out" && grep -qF 'ok   dev/tenant/gitea' <<<"$out" \
  && grep -qF "FAIL bad/tenant/gitea: project dev-tenant allows no destination $NONPROD preprod-gitea" <<<"$out" \
  && ! grep -qF 'idle' <<<"$out"; then
  echo "ok   each enabled Application of every instance is checked against the projects the hub renders"
else
  echo "FAIL the loop: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi
rm -rf "$work/repo/argo-cd/instances/hub"
out=$(APPS_DIR="$work/cat/argo-cd/apps" INSTANCES_DIR="$work/repo/argo-cd/instances" "$SCRIPT" 2>&1); rc=$?
if [ "$rc" = 0 ] && grep -qF 'projects not checked: no instance enables the ohmlab app' <<<"$out"; then
  echo "ok   without an instance that enables ohmlab, the projects are not checked"
else
  echo "FAIL no ohmlab: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

exit "$fail"
