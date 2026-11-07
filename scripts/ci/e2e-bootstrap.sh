#!/usr/bin/env bash
# End to end test of `ohmlab bootstrap` on an empty kind cluster, with a copy
# of the _example instance: a dry run changes nothing; the first run installs
# the Gateway API CRDs, then Argo CD alone, then the whole chart, and
# `ohmlab argocd password` prints the password Argo CD generated; a second run
# with ARGOCD_ADMIN_PASSWORD skips phase 1 and sets a password Argo CD then
# accepts. No password is printed, and none is left on disk.
#
#   e2e-bootstrap.sh
#
# Environment:
#   KIND_CLUSTER          the kind cluster to use (default ohmlab-bootstrap). It
#                         is created when it does not exist, with a kubeconfig
#                         of its own, so yours is never touched
#   E2E_EXISTING_CLUSTER  set when the cluster already exists and is empty (CI):
#                         nothing is created or deleted
#   KEEP                  set to keep the cluster (and its kubeconfig) afterwards
# Needs kind, kubectl, helm (4), go, jq and curl.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLUSTER="${KIND_CLUSTER:-ohmlab-bootstrap}"
CTX="kind-$CLUSTER"
NS=argocd-system
work=$(mktemp -d)
failures=0
created=""
pf=""

cleanup() {
  [ -n "$pf" ] && kill "$pf" 2>/dev/null
  if [ -n "$created" ] && [ -n "${KEEP:-}" ]; then
    echo "kept: kind cluster $CLUSTER, KUBECONFIG=$work/kubeconfig"
    return
  fi
  [ -n "$created" ] && kind delete cluster --name "$CLUSTER" --kubeconfig "$work/kubeconfig" >/dev/null 2>&1
  rm -rf "$work"
}
trap cleanup EXIT

pass() { echo "ok   $1"; }
fail() { echo "FAIL $1"; failures=$((failures + 1)); }
# expect <label> <command...>: ok when the command succeeds, its output unseen.
expect() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then pass "$label"; else fail "$label"; fi
}
k() { kubectl --context "$CTX" "$@"; }
# messages <log>: the msg of every JSON record the CLI wrote.
messages() { jq -rR 'fromjson? | .msg // empty' "$1"; }
# errors <log>: the CLI's error records, for a failed run.
errors() { jq -rR 'fromjson? | select(.level == "ERROR") | "\(.step // .msg): \(.error // "")"' "$1" | tail -5; }

(cd "$ROOT/cli" && CGO_ENABLED=0 go build -o "$work/ohmlab" .) || { echo "cannot build the CLI" >&2; exit 1; }
cli="$work/ohmlab"

if [ -z "${E2E_EXISTING_CLUSTER:-}" ]; then
  if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    echo "kind cluster $CLUSTER exists: delete it (kind delete cluster --name $CLUSTER), or set E2E_EXISTING_CLUSTER for an empty one" >&2
    exit 1
  fi
  export KUBECONFIG="$work/kubeconfig"
  created=1
  kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" --wait 120s >/dev/null 2>&1 || { echo "cannot create kind cluster $CLUSTER" >&2; exit 1; }
fi

# The workspace: an instance repository holding a copy of _example, bootstrapped
# from this checkout's chart. The CLI's temporary files go to a directory of
# its own, so a file it leaves behind is found.
ws="$work/ws"
mkdir -p "$ws/argo-cd/instances" "$work/tmp"
cp -R "$ROOT/argo-cd/instances/_example" "$ws/argo-cd/instances/ci"
printf 'product: %s\ncontextName: %s\n' "$ROOT" "$CTX" >"$ws/ohmlab.yaml"
cd "$ws" || exit 1
export TMPDIR="$work/tmp"
export ARGOCD_ADMIN_PASSWORD=""

# 1. The dry run plans the three helm commands and changes nothing.
if "$cli" bootstrap --dry-run >"$work/dry.json" 2>&1; then pass "the dry run exits 0"; else fail "the dry run exits 0"; errors "$work/dry.json"; fi
n=$(messages "$work/dry.json" | grep -c '^Would run: helm')
if [ "$n" = 3 ]; then pass "the dry run plans the dependencies, phase 1 and phase 2"; else fail "the dry run plans 3 helm commands, not $n"; fi
if k get namespace "$NS" >/dev/null 2>&1 || k get crd tlsroutes.gateway.networking.k8s.io >/dev/null 2>&1; then
  fail "the dry run changed nothing"
else
  pass "the dry run changed nothing"
fi

# 2. The first run installs everything, and Argo CD generates the password.
if "$cli" bootstrap --yes >"$work/run1.json" 2>&1; then
  pass "the first run exits 0"
else
  fail "the first run exits 0"
  errors "$work/run1.json"
  k -n "$NS" get pods 2>/dev/null
  exit "$failures"
fi
expect "the Gateway API CRDs are Established" \
  k wait --for=condition=Established crd/tlsroutes.gateway.networking.k8s.io crd/httproutes.gateway.networking.k8s.io --timeout=30s
status() { helm status ohmlab -n "$NS" --kube-context "$CTX" -o json | jq -r '"\(.info.status) \(.version)"'; }
if [ "$(status)" = "deployed 2" ]; then pass "the release is deployed at revision 2 (phase 1, then phase 2)"; else fail "the release after the first run: $(status)"; fi
expect "argocd-server is Available" k -n "$NS" wait --for=condition=Available deployment/argocd-server --timeout=120s
expect "the root manager ApplicationSet exists" k -n "$NS" get applicationset manager
expect "the core and tenant AppProjects exist" k -n "$NS" get appproject admin-core admin-tenant

# 3. The generated password: printed by `argocd password`, never by bootstrap's
# JSON output.
generated=$(k -n "$NS" get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 --decode)
printed=$("$cli" argocd password 2>/dev/null)
if [ -n "$generated" ] && [ "$printed" = "$generated" ]; then pass "ohmlab argocd password prints the generated password"; else fail "ohmlab argocd password prints the generated password"; fi
if [ -n "$generated" ] && grep -qF "$generated" "$work/run1.json"; then fail "the first run's output holds the generated password"; else pass "the first run's output holds no password"; fi

# 4. A second run with a password: phase 1 skipped, the password set.
pw="e2e-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
if ARGOCD_ADMIN_PASSWORD="$pw" "$cli" bootstrap --yes >"$work/run2.json" 2>&1; then pass "the second run exits 0"; else fail "the second run exits 0"; errors "$work/run2.json"; fi
if messages "$work/run2.json" | grep -q 'installed, phase 1 skipped'; then pass "the second run skips phase 1"; else fail "the second run skips phase 1"; fi
if [ "$(status)" = "deployed 3" ]; then pass "the second run upgrades the release to revision 3"; else fail "the release after the second run: $(status)"; fi

k -n "$NS" port-forward svc/argocd-server 18443:443 >/dev/null 2>&1 &
pf=$!
for _ in $(seq 1 30); do
  curl -sk -o /dev/null https://127.0.0.1:18443/healthz && break
  sleep 1
done
# session <password>: the HTTP status of an admin login. Argo CD locks an
# account out after five failed logins, so the retries stay below that.
session() {
  curl -sk -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"$1\"}" https://127.0.0.1:18443/api/v1/session
}
code=""
for _ in 1 2 3; do
  code=$(session "$pw")
  [ "$code" = 200 ] && break
  sleep 5
done
if [ "$code" = 200 ]; then pass "Argo CD accepts the password the second run set"; else fail "Argo CD answers $code to the password the second run set"; fi
code=$(session "not-$pw")
if [ "$code" = 401 ]; then pass "Argo CD refuses another password"; else fail "Argo CD answers $code to another password"; fi
if grep -qF "$pw" "$work/run1.json" "$work/run2.json" || { [ -n "$generated" ] && grep -qF "$generated" "$work/run2.json"; }; then
  fail "a run's output holds a password"
else
  pass "no run's output holds a password"
fi
if compgen -G "$work/tmp/ohmlab-bootstrap-*" >/dev/null; then fail "the password's values file outlived the run"; else pass "the password's values file is gone"; fi

[ "$failures" -eq 0 ] && echo "ok   ohmlab bootstrap on an empty kind cluster: dry run, two phases, the generated password, a given one"
exit "$failures"
