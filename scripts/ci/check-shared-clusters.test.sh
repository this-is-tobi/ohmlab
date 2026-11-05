#!/usr/bin/env bash
# Decision table for scripts/ci/check-shared-clusters.sh on hand-written render
# directories, laid out as render-instance.sh writes them: per app
# <instance>/<tier>/<app>.destination ("<server> <namespace>") and
# .unnamespaced ("<Kind>/<name>" of the objects without a namespace), per
# instance crd-scopes ("<Kind> Cluster|Namespaced").
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-shared-clusters.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

S=https://nonprod.example.com:6443
P=https://prod.example.com:6443

# app <dir> <instance/tier/app> <server> <namespace> [<Kind/name>...]
app() {
  local d="$1/$2"
  mkdir -p "$(dirname "$d")"
  printf '%s %s\n' "$3" "$4" >"$d.destination"
  shift 4
  : >"$d.unnamespaced"
  [ $# -eq 0 ] || printf '%s\n' "$@" >>"$d.unnamespaced"
}
# check <label> <expected exit> <text the output must hold> <render dir>
check() {
  local out rc
  out=$("$SCRIPT" "$4" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1 (exit $rc, want $2, looking for '$3')"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  fi
}

r="$work/prefixes"
app "$r" dev/tenant/gitea "$S" dev-gitea ClusterRole/dev-gitea
app "$r" preprod/tenant/gitea "$S" preprod-gitea ClusterRole/preprod-gitea
check "two instances with distinct prefixes share a cluster" 0 "ok   $S: 2 app(s)" "$r"

r="$work/namespace"
app "$r" dev/tenant/gitea "$S" gitea
app "$r" preprod/tenant/gitea "$S" gitea
check "a namespace two Applications deploy to is refused" 1 "FAIL $S: namespace gitea is the destination of dev/tenant/gitea and preprod/tenant/gitea" "$r"

r="$work/clusterrole"
app "$r" dev/tenant/homepage "$S" dev-homepage ClusterRole/homepage
app "$r" preprod/tenant/homepage "$S" preprod-homepage ClusterRole/homepage
check "a cluster-scoped object two Applications render is refused" 1 "FAIL $S: ClusterRole/homepage is rendered by dev/tenant/homepage and preprod/tenant/homepage" "$r"

r="$work/servers"
app "$r" nonprod/core/traefik "$S" traefik ClusterRole/traefik
app "$r" prod/core/traefik "$P" traefik ClusterRole/traefik
check "the same names on two clusters are allowed" 0 "ok   $P: 1 app(s)" "$r"

r="$work/crd"
app "$r" dev/tenant/a "$S" dev-a Widget/w
app "$r" preprod/tenant/a "$S" preprod-a Widget/w
check "a kind no CRD declares is namespaced" 0 "ok   $S: 2 app(s)" "$r"
mkdir -p "$r/nonprod"
printf 'Widget Cluster\n' >"$r/nonprod/crd-scopes"
check "a CRD another instance renders makes its kind cluster-scoped" 1 "FAIL $S: Widget/w is rendered by dev/tenant/a and preprod/tenant/a" "$r"

r="$work/gatewayclass"
app "$r" nonprod/core/traefik "$S" traefik GatewayClass/traefik
app "$r" nonprod/core/apisix "$S" apisix GatewayClass/traefik
check "GatewayClass is cluster-scoped without its CRD" 1 "FAIL $S: GatewayClass/traefik is rendered by" "$r"

r="$work/strings"
app "$r" hub/core/a https://kubernetes.default.svc a
app "$r" hub/tenant/b https://10.0.0.1:6443 a
check "two server strings are two clusters" 0 "ok   https://10.0.0.1:6443: 1 app(s)" "$r"

check "a directory without renders has nothing to check" 0 "no rendered app, nothing to check" "$work/empty"

# An exit status is taken modulo 256: 256 failures must not read as success.
r="$work/many"
for i in $(seq 1 256); do
  app "$r" a/tenant/x$i "$S" ns$i
  app "$r" b/tenant/x$i "$S" ns$i
done
check "256 failures exit 255" 255 "FAIL $S: namespace ns256 is the destination of" "$r"

exit "$fail"
