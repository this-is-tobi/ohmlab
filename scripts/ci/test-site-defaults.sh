#!/usr/bin/env bash
# A chart's defaults name no site: no LAN range, no API server address, no
# gateway namespace. An instance sets them. Each case renders one chart with
# the example values and the parameters the manager gives, and reads only the
# fields under test: no render is ever printed. Needs helm and yq (mikefarah
# v4).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
EXAMPLE="$ROOT/argo-cd/instances/_example/values"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # check <label> <expected> <actual>
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

# render <chart dir> <example values file> [<helm args>...]: renders the chart
# with that values file and the manager's parameters into $work/out.yaml (its
# dependencies built first when they are not vendored); fails, saying why,
# when it cannot.
render() {
  local chart="$1" vf="$2"
  shift 2
  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL cannot build the dependencies of $chart"; fail=1; return 1; }
  fi
  args=()
  app_args "$vf" || { echo "FAIL no manager parameters for $vf"; fail=1; return 1; }
  helm template t "$chart" "${args[@]}" "$@" >"$work/out.yaml" 2>"$work/err" \
    || { echo "FAIL $chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; return 1; }
}
# One document at a time (eval, not eval-all, which pairs documents once an
# expression binds a variable).
field() { yq e -r "$1" "$work/out.yaml" | grep -v '^$' | grep -v '^---$' | paste -sd, -; }

# The ApplicationSet webhook (port "webhook" of the controller): no chart opens
# it on its own; an instance that wants Git webhooks routes to it and admits
# its gateway's namespace with an extraIngress rule.
check "no chart template names a gateway namespace" "" \
  "$(grep -rln 'kubernetes.io/metadata.name: traefik' "$ROOT"/argo-cd/apps/*/templates "$ROOT/utils/helm/templates" 2>/dev/null | sed "s#$ROOT/##" | paste -sd, -)"

webhook_ports='select(.kind == "NetworkPolicy") | .spec.ingress[]?.ports[]?.port | select(. == "webhook" or . == 7000)'
appset_route='[{"name":"argocd-appset-webhook","hostnames":["appset.example.com"],"rules":[{"backendService":"argo-cd-argocd-applicationset-controller","backendPort":7000}]}]'
render "$ROOT/argo-cd/apps/argo-cd" "$EXAMPLE/tenant/argo-cd.yaml" --set gateway.enabled=true --set-json "gateway.routes=$appset_route"
check "the tenant Argo CD opens no webhook port, even with a route to it" "" "$(field "$webhook_ports")"

extra='[{"name":"appset-webhook","podSelector":{"matchLabels":{"app.kubernetes.io/name":"argocd-applicationset-controller"}},"rules":[{"from":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"gw"}}}],"ports":[{"port":"webhook","protocol":"TCP"}]}]}]'
admits='select(.kind == "NetworkPolicy" and .metadata.name == "allow-extra-ingress-appset-webhook") | .spec.ingress[] | .from[].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] + ":" + (.ports[].port | tostring)'
render "$ROOT/argo-cd/apps/argo-cd" "$EXAMPLE/tenant/argo-cd.yaml" --set networkPolicy.enabled=true --set-json "networkPolicy.extraIngress=$extra"
check "an instance opens the tenant webhook port with extraIngress" "gw:webhook" "$(field "$admits")"
render "$ROOT/utils/helm" "$EXAMPLE/core/ohmlab.yaml" --set networkPolicy.enabled=true --set-json "networkPolicy.extraIngress=$extra"
check "an instance opens the core webhook port with extraIngress" "gw:webhook" "$(field "$admits")"

# refuses <label> <message> <chart dir> <example values file> [<helm args>...]:
# the chart does not render, and says why.
refuses() {
  local label="$1" message="$2" chart="$3" vf="$4"
  shift 4
  args=()
  app_args "$vf" || { echo "FAIL no manager parameters for $vf"; fail=1; return; }
  if helm template t "$chart" "${args[@]}" "$@" >/dev/null 2>"$work/err"; then
    echo "FAIL $label: it renders"; fail=1
  elif grep -qF -- "$message" "$work/err"; then echo "ok   $label"; else
    echo "FAIL $label: expected '$message', got: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1
  fi
}
renders() { # renders <label> <chart dir> <example values file> [<helm args>...]
  local label="$1" out
  shift
  if out=$(render "$@"); then echo "ok   $label"; else echo "FAIL $label: ${out#FAIL }"; fail=1; fi
}

# Trust lists exempt a range from CrowdSec's decisions: by default they hold
# no LAN, only the cluster's own ranges.
render "$ROOT/argo-cd/apps/traefik" "$EXAMPLE/core/traefik.yaml"
check "the bouncer trusts no range by default" "none" \
  "$(field 'select(.kind == "Middleware" and .metadata.name == "crowdsec-bouncer") | .spec.plugin.bouncer.clientTrustedIPs // "none"')"
render "$ROOT/argo-cd/apps/crowdsec" "$EXAMPLE/core/crowdsec.yaml"
# The file is a multi-line string: read it whole, not through field.
check "the CrowdSec whitelist trusts only in-cluster ranges by default" "10.42.0.0/16,10.45.0.0/16,10.43.0.0/16" \
  "$(yq e -r 'select(.kind == "ConfigMap" and .metadata.name == "crowdsec-postoverflows-s01-whitelist") | .data["whitelist-local.yaml"]' "$work/out.yaml" \
    | grep -v '^---$' | yq '.whitelist.cidr | join(",")')"

# Allow lists and the API server address have no default: a render without
# them stops and says what to set.
for empty in '[]' 'null'; do
  refuses "Traefik does not render without lanOnly.sourceRange ($empty)" "lanOnly.sourceRange is required" \
    "$ROOT/argo-cd/apps/traefik" "$EXAMPLE/core/traefik.yaml" --set-json "lanOnly.sourceRange=$empty"
done
for app in longhorn cilium; do
  refuses "$app does not render its Gateway without lanOnly.sourceRange" "lanOnly.sourceRange is required" \
    "$ROOT/argo-cd/apps/$app" "$EXAMPLE/core/$app.yaml" --set gateway.enabled=true --set-json 'lanOnly.sourceRange=[]'
  renders "$app renders without lanOnly.sourceRange when its Gateway is off" \
    "$ROOT/argo-cd/apps/$app" "$EXAMPLE/core/$app.yaml" --set gateway.enabled=false --set-json 'lanOnly.sourceRange=[]'
done
refuses "Cilium does not render without its API server address" "cilium.k8sServiceHost is required" \
  "$ROOT/argo-cd/apps/cilium" "$EXAMPLE/core/cilium.yaml" --set cilium.k8sServiceHost=

exit "$fail"
