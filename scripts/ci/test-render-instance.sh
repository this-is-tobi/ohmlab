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
  printf 'instance: {name: %s}\nrepoURL: https://git.example/x.git\n%s\n' "$1" "$2" >"$d/instance.yaml"
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

# chartPath is a path of the catalog, whose argo-cd/apps APPS_DIR is, as for
# Argo CD (charts/ohmlab in this repository).
mkdir -p "$work/cat/argo-cd/apps" "$work/cat/charts/boot/templates"
printf 'apiVersion: v2\nname: boot\nversion: 0.1.0\n' >"$work/cat/charts/boot/Chart.yaml"
printf 'apiVersion: v1\nkind: ConfigMap\nmetadata: {name: boot}\n' >"$work/cat/charts/boot/templates/cm.yaml"
d=$(instance p "" '- {app: ohmlab, enabled: "true", chartPath: charts/boot}')
printf '{}\n' >"$d/values/core/ohmlab.yaml"
out=$(APPS_DIR="$work/cat/argo-cd/apps" INSTANCES_DIR="$(dirname "$d")" "$SCRIPT" 2>&1)
expect "an entry's chartPath renders that path of the catalog" 0 "ok   p/core/ohmlab" "$out" $?
d=$(instance q "" '- {app: ohmlab, enabled: "true", chartPath: charts/none}')
printf '{}\n' >"$d/values/core/ohmlab.yaml"
out=$(APPS_DIR="$work/cat/argo-cd/apps" INSTANCES_DIR="$(dirname "$d")" "$SCRIPT" 2>&1)
expect "a chartPath the catalog lacks is reported" 1 "FAIL q/core/ohmlab" "$out" $?

mkdir -p "$work/apps/named/templates"
printf 'apiVersion: v2\nname: named\nversion: 0.1.0\n' >"$work/apps/named/Chart.yaml"
cat >"$work/apps/named/templates/cm.yaml" <<'YAML'
{{- $names := dig "ohmlab" "names" dict (.Values.global | default dict) }}
{{- if ne (toJson $names) .Values.expectNames }}{{ fail (printf "names %s" (toJson $names)) }}{{ end }}
apiVersion: v1
kind: ConfigMap
metadata: {name: named}
YAML
d=$(instance n "" '- {app: named, enabled: "true"}')
printf '%s\n' "expectNames: '{\"gateway\":\"n-gateway\",\"keycloakRealm\":\"n\",\"keycloakRealmDisplayName\":\"N\",\"vaultMount\":\"n\"}'" >"$d/values/core/named.yaml"
out=$(run "$d"); expect "every app gets the names its manager derives from the instance name" 0 "ok   n/core/named" "$out" $?

d=$(instance o "names:
  keycloakRealmDisplayName: My lab, Inc" '- {app: named, enabled: "true"}')
printf '%s\n' "expectNames: '{\"gateway\":\"o-gateway\",\"keycloakRealm\":\"o\",\"keycloakRealmDisplayName\":\"My lab, Inc\",\"vaultMount\":\"o\"}'" >"$d/values/core/named.yaml"
out=$(run "$d"); expect "a name the instance sets reaches every app, commas included" 0 "ok   o/core/named" "$out" $?

d=$(instance p "names:
  nope: x" '- {app: named, enabled: "true"}')
printf 'expectNames: x\n' >"$d/values/core/named.yaml"
out=$(run "$d"); expect "an instance whose manager does not render is reported with the reason" 1 "the instance-manager does not render: names.nope is not a name the product reads" "$out" $?

mkdir -p "$work/apps/gen/templates"
printf 'apiVersion: v2\nname: gen\nversion: 0.1.0\n' >"$work/apps/gen/Chart.yaml"
cat >"$work/apps/gen/templates/all.yaml" <<'YAML'
---
apiVersion: v1
kind: Secret
metadata: {name: gen}
data: {password: {{ randAlphaNum 16 | b64enc | quote }}}
---
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: gen}
spec:
  selector: {matchLabels: {app: gen}}
  template:
    metadata:
      labels: {app: gen}
      annotations: {checksum/secret: {{ randAlphaNum 16 | quote }}}
    spec: {containers: [{name: gen, image: busybox}]}
YAML
d=$(instance r "" '- {app: gen, enabled: "true"}')
printf 'x: 1\n' >"$d/values/core/gen.yaml"
APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$d")" RENDER_OUT="$work/out1" "$SCRIPT" >/dev/null 2>&1
APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$d")" RENDER_OUT="$work/out2" "$SCRIPT" >/dev/null 2>&1
rec="$work/out1/r/core/gen.objects"
if [ -f "$rec" ] && [ "$(wc -l <"$rec" | tr -d ' ')" = 2 ] && grep -q '^Secret/gen/gen ' "$rec" && grep -q '^Deployment/gen/gen ' "$rec" \
    && ! grep -q '^null' "$rec" && cmp -s "$rec" "$work/out2/r/core/gen.objects"; then
  echo "ok   RENDER_OUT records each object once (empty documents skipped), stable across renders of generated values"
else
  echo "FAIL RENDER_OUT recording"; sed 's/^/     /' "$rec" "$work/out2/r/core/gen.objects" 2>&1; fail=1
fi

# crdapp installs example.com/v1 Widget from a template (its v1beta1 is not
# served) and example.com/v1 Gadget from its crds/ directory; gated renders
# one object per API only when that API is known; broken never renders.
mkdir -p "$work/apps/crdapp/templates" "$work/apps/crdapp/crds" "$work/apps/gated/templates" "$work/apps/broken/templates"
printf 'apiVersion: v2\nname: crdapp\nversion: 0.1.0\n' >"$work/apps/crdapp/Chart.yaml"
cat >"$work/apps/crdapp/templates/widgets.yaml" <<'YAML'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.example.com}
spec:
  group: example.com
  scope: Namespaced
  names: {plural: widgets, kind: Widget}
  versions:
  - {name: v1, served: true, storage: true, schema: {openAPIV3Schema: {type: object}}}
  - {name: v1beta1, served: false, storage: false, schema: {openAPIV3Schema: {type: object}}}
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: things.other.io}
spec:
  group: other.io
  scope: Namespaced
  names: {plural: things, kind: Thing}
  versions:
  - {name: v2, served: true, storage: true, schema: {openAPIV3Schema: {type: object}}}
YAML
cat >"$work/apps/crdapp/crds/gadgets.yaml" <<'YAML'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: gadgets.example.com}
spec:
  group: example.com
  scope: Namespaced
  names: {plural: gadgets, kind: Gadget}
  versions:
  - {name: v1, served: true, storage: true, schema: {openAPIV3Schema: {type: object}}}
YAML
printf 'apiVersion: v2\nname: gated\nversion: 0.1.0\n' >"$work/apps/gated/Chart.yaml"
cat >"$work/apps/gated/templates/cm.yaml" <<'YAML'
apiVersion: v1
kind: ConfigMap
metadata: {name: gated}
{{- range $name, $api := dict "widget" "example.com/v1/Widget" "gadget" "example.com/v1/Gadget" "beta" "example.com/v1beta1/Widget" "thing" "other.io/v2/Thing" "cross" "example.com/v1/Thing" }}
{{- if $.Capabilities.APIVersions.Has $api }}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: gated-{{ $name }}}
{{- end }}
{{- end }}
YAML
printf 'apiVersion: v2\nname: broken\nversion: 0.1.0\n' >"$work/apps/broken/Chart.yaml"
printf '{{ fail "broken" }}\n' >"$work/apps/broken/templates/cm.yaml"
lists() { grep -q "^$2 " "$1" 2>/dev/null; } # lists <objects file> <kind/namespace/name>

d=$(instance s "" '- {app: crdapp, enabled: "true"}')
printf 'apps:\n- {app: gated, enabled: "true"}\n' >"$d/tenant.yaml"
printf 'x: 1\n' >"$d/values/core/crdapp.yaml"; printf 'x: 1\n' >"$d/values/tenant/gated.yaml"
APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$d")" RENDER_OUT="$work/out-s" "$SCRIPT" >"$work/s.log" 2>&1
gated="$work/out-s/s/tenant/gated.objects"
if lists "$gated" ConfigMap/gated/gated-widget; then echo "ok   an API a core app installs is known to the tenant apps"; else
  echo "FAIL an API a core app installs is known to the tenant apps"; sed 's/^/     /' "$work/s.log" "$gated" 2>&1; fail=1
fi
if lists "$work/out-s/s/core/crdapp.objects" CustomResourceDefinition/crdapp/gadgets.example.com && lists "$gated" ConfigMap/gated/gated-gadget; then
  echo "ok   a chart's crds/ directory renders, as Argo CD includes it"
else echo "FAIL a chart's crds/ directory renders, as Argo CD includes it"; fail=1; fi
if lists "$gated" ConfigMap/gated/gated && ! lists "$gated" ConfigMap/gated/gated-beta; then echo "ok   a CRD version that is not served is no API"; else
  echo "FAIL a CRD version that is not served is no API"; fail=1
fi
if lists "$gated" ConfigMap/gated/gated-thing && ! lists "$gated" ConfigMap/gated/gated-cross; then echo "ok   each CRD's kind is an API of its own group only"; else
  echo "FAIL each CRD's kind is an API of its own group only"; fail=1
fi

d=$(instance t "" '- {app: gated, enabled: "true"}')
printf 'x: 1\n' >"$d/values/core/gated.yaml"
APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$d")" RENDER_OUT="$work/out-t" "$SCRIPT" >/dev/null 2>&1
if lists "$work/out-t/t/core/gated.objects" ConfigMap/gated/gated && ! lists "$work/out-t/t/core/gated.objects" ConfigMap/gated/gated-widget; then
  echo "ok   an API the instance does not install stays unknown"
else echo "FAIL an API the instance does not install stays unknown"; fail=1; fi

d=$(instance u "" '- {app: broken, enabled: "true"}')
printf 'x: 1\n' >"$d/values/core/broken.yaml"
out=$(run "$d")
if [ "$(grep -c '^FAIL u/core/broken' <<<"$out")" = 1 ]; then echo "ok   an app that fails is reported once"; else
  echo "FAIL an app that fails is reported once"; sed 's/^/     /' <<<"$out"; fail=1
fi

# What check-shared-clusters.sh reads: each app's destination (the entry's
# server, else the instance's, else in-cluster), the objects it renders without
# a namespace, and the scopes of the CRDs the instance renders.
d=$(instance w "destination: {server: https://w.example.com:6443}" '- {app: gen, enabled: "true"}
- {app: crdapp, enabled: "true", destination: {server: https://other.example.com:6443}}')
printf 'x: 1\n' >"$d/values/core/gen.yaml"; printf 'x: 1\n' >"$d/values/core/crdapp.yaml"
APPS_DIR="$work/apps" INSTANCES_DIR="$(dirname "$d")" RENDER_OUT="$work/out-w" "$SCRIPT" >/dev/null 2>&1
o="$work/out-w/w"
if [ "$(cat "$o/core/gen.destination" 2>/dev/null)" = "https://w.example.com:6443 gen" ] \
    && [ "$(cat "$o/core/crdapp.destination" 2>/dev/null)" = "https://other.example.com:6443 crdapp" ] \
    && [ "$(cat "$work/out1/r/core/gen.destination" 2>/dev/null)" = "https://kubernetes.default.svc gen" ] \
    && [ "$(cat "$o/core/gen.unnamespaced" 2>/dev/null)" = $'Deployment/gen\nSecret/gen' ] \
    && grep -qx 'Widget Namespaced' "$o/crd-scopes" 2>/dev/null && grep -qx 'Thing Namespaced' "$o/crd-scopes"; then
  echo "ok   RENDER_OUT records each app's destination, its objects without a namespace and the instance's CRD scopes"
else
  echo "FAIL RENDER_OUT destination, objects without a namespace or CRD scopes"
  for f in "$o"/core/*.destination "$o"/core/*.unnamespaced "$o/crd-scopes" "$work/out1/r/core/gen.destination"; do
    echo "     $f: $(tr '\n' ';' <"$f" 2>&1)"
  done
  fail=1
fi

exit "$fail"
