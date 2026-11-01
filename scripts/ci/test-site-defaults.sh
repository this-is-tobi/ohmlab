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
# dependencies built first when they are not vendored).
render() {
  local chart="$1" vf="$2"
  shift 2
  if [ -f "$chart/Chart.lock" ] && [ ! -d "$chart/charts" ]; then
    helm dependency build "$chart" >/dev/null 2>&1 || { echo "FAIL cannot build the dependencies of $chart"; fail=1; }
  fi
  args=()
  app_args "$vf" || { echo "FAIL no manager parameters for $vf"; fail=1; return; }
  helm template t "$chart" "${args[@]}" "$@" >"$work/out.yaml" 2>"$work/err" \
    || { echo "FAIL $chart does not render: $(grep -m1 Error "$work/err" | cut -c1-200)"; fail=1; }
}
field() { yq ea -r "$1" "$work/out.yaml" | grep -v '^$' | grep -v '^---$' | paste -sd, -; }

# The ApplicationSet webhook (port "webhook" of the controller): no chart opens
# it on its own; an instance that wants Git webhooks routes to it and admits
# its gateway's namespace with an extraIngress rule.
webhook_ports='select(.kind == "NetworkPolicy") | .spec.ingress[]?.ports[]?.port | select(. == "webhook" or . == 7000)'
server_route='[{"name":"argocd-server","hostnames":["argo-cd.example.com"],"rules":[{"backendService":"argocd-server","backendPort":80}]}]'
render "$ROOT/argo-cd/apps/argo-cd" "$EXAMPLE/tenant/argo-cd.yaml" --set gateway.enabled=true --set-json "gateway.routes=$server_route"
check "the tenant Argo CD opens no webhook port without a route to it" "" "$(field "$webhook_ports")"
# The Service carries the release's prefix (argo-cd-argocd-... for a release
# named argo-cd).
appset_route='[{"name":"argocd-appset-webhook","hostnames":["appset.example.com"],"rules":[{"backendService":"argo-cd-argocd-applicationset-controller","backendPort":7000}]}]'
render "$ROOT/argo-cd/apps/argo-cd" "$EXAMPLE/tenant/argo-cd.yaml" --set gateway.enabled=true --set-json "gateway.routes=$appset_route"
check "a route to the controller, whatever its release prefix, opens the tenant webhook port" "webhook" "$(field "$webhook_ports")"

extra='[{"name":"appset-webhook","podSelector":{"matchLabels":{"app.kubernetes.io/name":"argocd-applicationset-controller"}},"rules":[{"from":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"gw"}}}],"ports":[{"port":"webhook","protocol":"TCP"}]}]}]'
admits='select(.kind == "NetworkPolicy" and .metadata.name == "allow-extra-ingress-appset-webhook") | .spec.ingress[] | .from[].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] + ":" + (.ports[].port | tostring)'
render "$ROOT/argo-cd/apps/argo-cd" "$EXAMPLE/tenant/argo-cd.yaml" --set networkPolicy.enabled=true --set-json "networkPolicy.extraIngress=$extra"
check "an instance opens the tenant webhook port with extraIngress" "gw:webhook" "$(field "$admits")"
render "$ROOT/utils/helm" "$EXAMPLE/core/ohmlab.yaml" --set networkPolicy.enabled=true --set-json "networkPolicy.extraIngress=$extra"
check "an instance opens the core webhook port with extraIngress" "gw:webhook" "$(field "$admits")"

exit "$fail"
