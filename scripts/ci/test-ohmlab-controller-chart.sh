#!/usr/bin/env bash
# Tests of the ohmlab-controller chart that a plain `helm lint` cannot make:
# what renders when nothing is enabled, how targets are packaged (split by
# default, any grouping on request, a bad grouping fails), and that a grouped
# process is granted exactly what its targets would have been granted apart.
#
#   test-ohmlab-controller-chart.sh
#
# Needs helm, yq (mikefarah v4) and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/ohmlab-controller"
DIGEST=sha256:0000000000000000000000000000000000000000000000000000000000000000
IMAGE=ghcr.io/this-is-tobi/homelab/ohmlab-controller:0.2.0@$DIGEST
BASE=(--namespace ohmlab-system --set "image=$IMAGE" --set targets.vault.enabled=true)

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
render() { helm template ohmlab-controller "$CHART" "$@" 2>&1; }

# --- nothing enabled renders nothing --------------------------------------
if [ -n "$(render --namespace ohmlab-system | tr -d '[:space:]')" ]; then
  fail "the defaults render objects while no target is enabled"
fi

# --- an enabled target needs a pinned image -------------------------------
# (The chart's own default image is a pin once the first release exists, so ask for none.)
out=$(render --namespace ohmlab-system --set targets.vault.enabled=true --set image=)
grep -q 'image is required' <<<"$out" || fail "a target without a pinned image must fail the render"
out=$(render --namespace ohmlab-system --set targets.vault.enabled=true --set image=ghcr.io/this-is-tobi/homelab/ohmlab-controller:latest)
grep -q 'not pinned by digest' <<<"$out" || fail "an image without a digest must fail the render"
out=$(render --namespace ohmlab-system --set targets.vault.enabled=true --set image=ohmlab-controller:e2e --set unsafeAllowUnpinnedImage=true)
grep -q 'image: ohmlab-controller:e2e' <<<"$out" || fail "the e2e knob must accept an unpinned image"

# --- split: one Deployment per enabled target, with its own identity -------
out=$(render "${BASE[@]}")
[ "$(yq 'select(.kind == "Deployment") | .metadata.name' <<<"$out")" = "ohmlab-controller-vault" ] || fail "split: want one Deployment named ohmlab-controller-vault"
[ "$(yq 'select(.kind == "ServiceAccount") | .metadata.name' <<<"$out")" = "ohmlab-controller-vault" ] || fail "split: want its own ServiceAccount"
[ "$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].args[] | select(test("^--target="))' <<<"$out")" = "--target=vault" ] || fail "split: the process runs its own target only"

# --- the pod is as locked down as the policy engine demands ----------------
sc=$(yq -o=json 'select(.kind == "Deployment") | .spec.template.spec | {"pod": .securityContext, "c": .containers[0].securityContext}' <<<"$out")
jq -e '.pod.runAsNonRoot == true and .pod.runAsUser == 65532 and .pod.seccompProfile.type == "RuntimeDefault"
       and .c.allowPrivilegeEscalation == false and .c.readOnlyRootFilesystem == true and .c.capabilities.drop == ["ALL"]' <<<"$sc" >/dev/null \
  || fail "the pod is not restricted: $sc"

# --- RBAC: namespaced, no Secrets, CRDs by name -----------------------------
rules=$(yq -o=json -I=0 'select(.kind == "Role" or .kind == "ClusterRole") | {"kind": .kind, "ns": (.metadata.namespace // ""), "rules": .rules}' <<<"$out" | jq -s .)
jq -e '[.[].rules[] | select((.resources // []) | index("secrets"))] | length == 0' <<<"$rules" >/dev/null || fail "the controller must not be granted Secrets"
jq -e '[.[] | select(.kind == "ClusterRole")] | length == 1' <<<"$rules" >/dev/null || fail "want exactly one ClusterRole"
jq -e '[.[] | select(.kind == "ClusterRole") | .rules[] | select(.verbs != ["get"] or (.resourceNames // []) == [])] | length == 0' <<<"$rules" >/dev/null \
  || fail "the ClusterRole may only get CRDs by name"
jq -e '[.[] | select(.kind == "Role") | .rules[] | select((.verbs | index("delete")) or (.verbs | index("*")))] | length == 0' <<<"$rules" >/dev/null || fail "no Role may delete"

# --- CRDs: kept on prune and uninstall --------------------------------------
crd=$(yq 'select(.kind == "CustomResourceDefinition")' <<<"$out")
[ "$(yq '.metadata.annotations["argocd.argoproj.io/sync-options"]' <<<"$crd")" = "Prune=false" ] || fail "the CRD is not kept on prune"
[ "$(yq '.metadata.annotations["helm.sh/resource-policy"]' <<<"$crd")" = "keep" ] || fail "the CRD is not kept on uninstall"
yq -e '.metadata.annotations["ohmlab.fr/api-revision"]' <<<"$crd" >/dev/null || fail "the CRD lost its api-revision annotation"

# --- the ConfigMap holds the section of the targets the process runs --------
cfg=$(yq 'select(.kind == "ConfigMap" and .metadata.name == "ohmlab-controller-vault") | .data["controller.yaml"]' <<<"$out")
[ "$(yq '.vault.backend.auth.role' <<<"$cfg")" = "ohmlab-controller-vault" ] || fail "the config lacks the Vault role"
[ "$(yq '.leaseNamespace' <<<"$cfg")" = "ohmlab-system" ] || fail "the Lease namespace is the release's"

# --- Vault login: a projected token for Vault alone, not the API token --------
spec=$(yq -o=json 'select(.kind == "Deployment") | .spec.template.spec' <<<"$out")
jq -e '[.volumes[] | select(.name == "vault-token") | .projected.sources[0].serviceAccountToken] == [{"audience": "vault", "expirationSeconds": 600, "path": "token"}]
       and ([.containers[0].volumeMounts[] | select(.name == "vault-token") | .mountPath] == ["/var/run/secrets/ohmlab/vault"])' <<<"$spec" >/dev/null \
  || fail "the controller must log in with a projected token whose audience is vault"
[ "$(yq '.vault.backend.auth.tokenFile' <<<"$cfg")" = "/var/run/secrets/ohmlab/vault/token" ] || fail "the config must point the login at the projected token"
noaud=$(render "${BASE[@]}" --set targets.vault.backend.auth.audience=)
if grep -q 'vault-token\|tokenFile' <<<"$noaud"; then
  fail "an empty audience must log in with the pod's own token: no projected token, no tokenFile"
fi

# --- grouping ---------------------------------------------------------------
single=$(render "${BASE[@]}" --set 'deployments.all={vault}')
[ "$(yq 'select(.kind == "Deployment") | .metadata.name' <<<"$single")" = "ohmlab-controller-all" ] || fail "grouped: want a Deployment named after the group"
# Every grant of the split render is in the grouped render and the other way
# round: grouping may change the packaging, never what a target may do.
# shellcheck disable=SC2016 # $ns is yq syntax
grants() { yq -o=json -I=0 'select(.kind == "Role" and .metadata.name != "x") | .metadata.namespace as $ns | .rules[] | {"ns": $ns, "r": .}' <<<"$1" | jq -cS . | sed -e 's/ohmlab-controller-[a-z]*/LEASE/g' | sort -u; }
if [ "$(grants "$out")" != "$(grants "$single")" ]; then
  fail "grouping changed the grants:"
  diff <(grants "$out") <(grants "$single") >&2 || true
fi

# --- a bad grouping fails ---------------------------------------------------
expect_fail() { # <message fragment> <helm args...>
  local want="$1"; shift
  local out; out=$(render "${BASE[@]}" "$@")
  grep -q "$want" <<<"$out" || fail "want an error mentioning '$want', got: $(head -c 200 <<<"$out")"
}
expect_fail 'not an enabled target' --set 'deployments.all={vault,harbor}'
expect_fail 'lists no target' --set-json 'deployments={"empty":[]}'
expect_fail 'is enabled and in no deployment' --set 'targets.harbor.enabled=true' --set 'targets.harbor.watchNamespaces={harbor}' --set 'deployments.v={vault}'
expect_fail 'is in deployments' --set 'targets.harbor.enabled=true' --set 'targets.harbor.watchNamespaces={harbor}' --set 'deployments.a={vault,harbor}' --set 'deployments.b={harbor}'

# --- monitoring objects follow the targets, and the alerts follow the resync -
mon=$(render "${BASE[@]}" --set resync=5m)
[ "$(yq 'select(.kind == "PrometheusRule") | .spec.groups[0].rules[] | select(.alert == "OhmlabReconcileStale") | .expr' <<<"$mon")" = 'time() - max by (target) (ohmlab_reconcile_last_success_timestamp_seconds) > 900' ] \
  || fail "the stale alert is not three resyncs"
out=$(render "${BASE[@]}" --set resync=soon)
grep -q 'write it as' <<<"$out" || fail "a bad resync must fail"

[ "$failures" -eq 0 ] && echo "ok: ohmlab-controller chart"
exit "$failures"
