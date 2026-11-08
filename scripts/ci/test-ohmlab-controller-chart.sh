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
IMAGE=ghcr.io/this-is-tobi/ohmlab/controller:0.2.0@$DIGEST
BASE=(--namespace ohmlab-system --set "image=$IMAGE" --set targets.vault.enabled=true --api-versions monitoring.coreos.com/v1)

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
out=$(render --namespace ohmlab-system --set targets.vault.enabled=true --set image=ghcr.io/this-is-tobi/ohmlab/controller:latest)
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
# Without the Prometheus Operator CRDs the monitoring objects stay out (Argo CD
# would refuse the whole sync over them) and everything else renders.
bare=$(render --namespace ohmlab-system --set "image=$IMAGE" --set targets.vault.enabled=true)
[ -z "$(yq 'select(.apiVersion == "monitoring.coreos.com/v1") | .kind' <<<"$bare")" ] || fail "a PodMonitor or PrometheusRule renders on a cluster without the Prometheus Operator"
[ "$(yq 'select(.kind == "Deployment") | .metadata.name' <<<"$bare")" = "ohmlab-controller-vault" ] || fail "without the Prometheus Operator the controller must still render"
[ "$(yq 'select(.kind == "PodMonitor") | .kind' <<<"$out")" = PodMonitor ] || fail "with the Prometheus Operator the PodMonitor must render"
out=$(render "${BASE[@]}" --set resync=soon)
grep -q 'write it as' <<<"$out" || fail "a bad resync must fail"

# --- the certificates target -------------------------------------------------
RESTARTS='[{"namespace":"gitea","certificate":"gitea-tls","workloads":[{"kind":"Deployment","name":"gitea"},{"kind":"StatefulSet","name":"gitea-valkey"}]},{"namespace":"kube-system","certificate":"hubble-oauth2-proxy-tls","workloads":[{"kind":"Deployment","name":"hubble-oauth2-proxy"}]}]'
CERTS=(--set targets.certificates.enabled=true --set-json "targets.certificates.restarts=$RESTARTS")
both=$(render "${BASE[@]}" "${CERTS[@]}")
[ "$(yq -N 'select(.kind == "Deployment") | .metadata.name' <<<"$both" | sort | tr '\n' ' ')" = "ohmlab-controller-certificates ohmlab-controller-vault " ] \
  || fail "certificates: want its own Deployment beside vault's"
[ "$(yq 'select(.kind == "Deployment" and .metadata.name == "ohmlab-controller-certificates") | .spec.template.spec.containers[0].args[] | select(test("^--target="))' <<<"$both")" = "--target=certificates" ] \
  || fail "certificates: the process runs its own target only"
ccfg=$(yq 'select(.kind == "ConfigMap" and .metadata.name == "ohmlab-controller-certificates") | .data["controller.yaml"]' <<<"$both")
vcfg=$(yq 'select(.kind == "ConfigMap" and .metadata.name == "ohmlab-controller-vault") | .data["controller.yaml"]' <<<"$both")
[ "$(yq -o=json -I=0 '.certificates.restarts' <<<"$ccfg" | jq -cS .)" = "$(jq -cS . <<<"$RESTARTS")" ] || fail "certificates: the config does not hold the restarts as listed"
[ "$(yq '.vault' <<<"$ccfg")" = null ] || fail "certificates: the process received the vault section"
[ "$(yq '.certificates' <<<"$vcfg")" = null ] || fail "vault: the process received the certificates section"
# shellcheck disable=SC2016 # $ns is yq syntax
crules=$(yq -o=json -I=0 'select(.kind == "Role" and .metadata.name == "ohmlab-controller-certificates") | .metadata.namespace as $ns | {"ns": $ns, "rules": .rules}' <<<"$both" | jq -s .)
[ "$(jq -c '[.[].ns] | sort' <<<"$crules")" = '["gitea","kube-system"]' ] || fail "certificates: want Roles in exactly the listed namespaces, got $(jq -c '[.[].ns]' <<<"$crules")"
rule() { jq -c --arg ns "$1" --arg res "$2" '[.[] | select(.ns == $ns) | .rules[] | select(.resources == [$res]) | {g: .apiGroups, n: (.resourceNames // null), v: .verbs}]' <<<"$crules"; }
[ "$(rule gitea certificates)" = '[{"g":["cert-manager.io"],"n":null,"v":["get","list","watch"]}]' ] || fail "certificates: gitea's Certificates must be readable: $(rule gitea certificates)"
[ "$(rule gitea deployments)" = '[{"g":["apps"],"n":["gitea"],"v":["get","patch"]}]' ] || fail "certificates: want get/patch on deployment gitea only: $(rule gitea deployments)"
[ "$(rule gitea statefulsets)" = '[{"g":["apps"],"n":["gitea-valkey"],"v":["get","patch"]}]' ] || fail "certificates: want get/patch on statefulset gitea-valkey only: $(rule gitea statefulsets)"
[ "$(rule gitea daemonsets)" = '[]' ] || fail "certificates: no DaemonSet is listed in gitea"
[ "$(rule gitea events)" = '[{"g":["events.k8s.io"],"n":null,"v":["create","patch"]}]' ] || fail "certificates: want events in gitea"
[ "$(rule kube-system deployments)" = '[{"g":["apps"],"n":["hubble-oauth2-proxy"],"v":["get","patch"]}]' ] || fail "certificates: want get/patch on deployment hubble-oauth2-proxy only"
jq -e '[.[].rules[] | select(.apiGroups == ["apps"]) | select((.resourceNames // []) == [] or (.verbs - ["get","patch"]) != [])] | length == 0' <<<"$crules" >/dev/null \
  || fail "certificates: a rule on workloads must name them and only get or patch"
allrules=$(yq -o=json -I=0 'select(.kind == "Role" or .kind == "ClusterRole") | .rules' <<<"$both" | jq -s 'add')
jq -e '[.[] | select((.resources // []) | index("secrets"))] | length == 0' <<<"$allrules" >/dev/null || fail "certificates: no Secrets"
jq -e '[.[] | select((.verbs | index("delete")) or (.verbs | index("*")))] | length == 0' <<<"$allrules" >/dev/null || fail "certificates: no delete"
[ "$(yq 'select(.kind == "ClusterRole") | .metadata.name' <<<"$both")" = "ohmlab-system:ohmlab-controller-vault" ] || fail "certificates: want vault's ClusterRole only"
alone=$(render --namespace ohmlab-system --set "image=$IMAGE" "${CERTS[@]}")
[ -z "$(yq 'select(.kind == "ClusterRole" or .kind == "ClusterRoleBinding" or .kind == "CustomResourceDefinition") | .kind' <<<"$alone")" ] \
  || fail "certificates alone: no CRD, no ClusterRole"
[ "$(yq 'select(.kind == "Deployment") | .metadata.name' <<<"$alone")" = "ohmlab-controller-certificates" ] || fail "certificates alone: want its Deployment"
grouped=$(render "${BASE[@]}" "${CERTS[@]}" --set 'deployments.all={certificates,vault}')
# One name per line: a grouped process holds both Leases in one rule.
# shellcheck disable=SC2016 # $ns is yq syntax
named_grants() { yq -o=json -I=0 'select(.kind == "Role") | .metadata.namespace as $ns | .rules[] | {"ns": $ns, "r": .}' <<<"$1" \
  | jq -c '.r as $r | if $r.resourceNames then $r.resourceNames[] as $n | {ns, r: ($r | .resourceNames = [$n])} else . end' | jq -cS . \
  | sed -e 's/ohmlab-controller-[a-z]*/LEASE/g' | sort -u; }
if [ "$(named_grants "$both")" != "$(named_grants "$grouped")" ]; then
  fail "certificates: grouping changed the grants:"
  diff <(named_grants "$both") <(named_grants "$grouped") >&2 || true
fi
# The API server refuses the target any change but its annotation: RBAC grants
# patch on a workload by name, not on a field.
vap() { yq -N -o=json -I=0 'select(.kind == "ValidatingAdmissionPolicy")' <<<"$1"; }
vapb() { yq -N -o=json -I=0 'select(.kind == "ValidatingAdmissionPolicyBinding")' <<<"$1"; }
p=$(vap "$both")
[ -n "$p" ] || fail "certificates: no ValidatingAdmissionPolicy"
jq -e '.spec.failurePolicy == "Fail"
       and .spec.matchConstraints.resourceRules == [{"apiGroups": ["apps"], "apiVersions": ["*"], "operations": ["UPDATE"], "resources": ["daemonsets", "deployments", "statefulsets"]}]
       and ([.spec.matchConditions[].expression] == ["request.userInfo.username == \"system:serviceaccount:ohmlab-system:ohmlab-controller-certificates\""])
       and (.spec.validations | length) > 0' <<<"$p" >/dev/null || fail "certificates: the policy must match the target's ServiceAccount on workload updates, and fail closed: $p"
jq -e --argjson p "$p" '.spec.policyName == $p.metadata.name and .spec.validationActions == ["Deny"]' <<<"$(vapb "$both")" >/dev/null \
  || fail "certificates: the binding must deny through the policy"
[ "$(yq -N 'select(.kind == "ValidatingAdmissionPolicy") | .spec.matchConditions[0].expression' <<<"$grouped")" = 'request.userInfo.username == "system:serviceaccount:ohmlab-system:ohmlab-controller-all"' ] \
  || fail "certificates: grouped, the policy must name the shared ServiceAccount"
[ -z "$(vap "$out")$(vapb "$out")" ] || fail "vault alone renders an admission policy"
# Duplicates and unknown keys fail the render, not the process.
cexpect_fail() { # <message fragment> <restarts json>
  local out; out=$(render "${BASE[@]}" --set targets.certificates.enabled=true --set-json "targets.certificates.restarts=$2")
  grep -qF "$1" <<<"$out" || fail "certificates: want an error mentioning '$1', got: $(head -c 200 <<<"$out")"
}
cexpect_fail 'targets.certificates.restarts lists nothing' '[]'
cexpect_fail 'targets.certificates.restarts[0] needs namespace and certificate' '[{"certificate":"a","workloads":[{"kind":"Deployment","name":"a"}]}]'
cexpect_fail 'lists no workload' '[{"namespace":"a","certificate":"a","workloads":[]}]'
cexpect_fail 'kind "Pod" is not one of DaemonSet, Deployment, StatefulSet' '[{"namespace":"a","certificate":"a","workloads":[{"kind":"Pod","name":"a"}]}]'
cexpect_fail 'a/a is listed twice' '[{"namespace":"a","certificate":"a","workloads":[{"kind":"Deployment","name":"x"}]},{"namespace":"a","certificate":"a","workloads":[{"kind":"Deployment","name":"y"}]}]'
cexpect_fail 'Deployment a/x is listed under a/a and a/b' '[{"namespace":"a","certificate":"a","workloads":[{"kind":"Deployment","name":"x"}]},{"namespace":"a","certificate":"b","workloads":[{"kind":"Deployment","name":"x"}]}]'
cexpect_fail 'targets.certificates.restarts[0] has unknown keys: [secret]' '[{"namespace":"a","certificate":"a","secret":"s","workloads":[{"kind":"Deployment","name":"x"}]}]'
cexpect_fail 'workloads[0] has unknown keys: [namespace]' '[{"namespace":"a","certificate":"a","workloads":[{"kind":"Deployment","name":"x","namespace":"b"}]}]'

[ "$failures" -eq 0 ] && echo "ok: ohmlab-controller chart"
exit "$failures"
