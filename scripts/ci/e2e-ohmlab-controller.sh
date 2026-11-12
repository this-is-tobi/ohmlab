#!/usr/bin/env bash
# End to end test of the ohmlab controller on a kind cluster, against a Vault dev
# server with Kubernetes auth: the chart installs, the controller logs in as its
# own ServiceAccount under the policy the platform grants it, objects become
# Ready, drift is corrected, a resync writes nothing, rotation happens once,
# deletion policies behave, and a rollout never runs the target twice. Beside
# it, against cert-manager, the certificates target rolls a workload onto its
# renewed certificate, and may touch nothing else. Against a real RustFS with
# TLS, the storage target gives each S3Bucket its bucket, a key confined to it
# and the key's record in Vault: observe-only writes nothing, a rotation keeps
# the old key for its grace, a store rebuilt empty gets everything back from
# Vault, and deletion keeps a bucket that holds data.
#
#   e2e-ohmlab-controller.sh <split|single>
#
# Environment:
#   IMAGE          the controller image, already loaded into the cluster
#                  (default ohmlab-controller:e2e)
#   VAULT_IMAGE    the Vault image, preloaded when possible (default:
#                  mirror.gcr.io/hashicorp/vault at the VAULT_VERSION of
#                  cli/Dockerfile)
#   CERT_MANAGER_VERSION
#                  the cert-manager chart (default: the catalog's, from
#                  argo-cd/apps/cert-manager/Chart.yaml)
#   RustFS         the chart and version of argo-cd/apps/rustfs/Chart.yaml, with
#                  the catalog's security contexts and region
#   KIND_CLUSTER   the kind cluster to use; it is created when it does not exist
#   KEEP           set to keep the cluster afterwards
#   E2E_EXISTING_CLUSTER
#                  set when the cluster already exists and already holds the
#                  controller image (CI): nothing is created, loaded or deleted
# Needs kind, kubectl, helm, jq, yq, openssl and docker.
set -euo pipefail

MODE="${1:?usage: e2e-ohmlab-controller.sh <split|single>}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE="${IMAGE:-ohmlab-controller:e2e}"
# The Vault the toolbox image is built with, so there is one version to bump,
# from the Docker Hub mirror the Dockerfile already pulls its base images from:
# an anonymous pull straight from Docker Hub is rate limited on shared runners.
VAULT_IMAGE="${VAULT_IMAGE:-mirror.gcr.io/hashicorp/vault:$(sed -n 's/^ARG VAULT_VERSION=//p' "$ROOT/cli/Dockerfile")}"
CLUSTER="${KIND_CLUSTER:-ohmlab-e2e}"
CTX="kind-$CLUSTER"
NS_VAULT=vault-operator-system
NS_CTL=ohmlab-system
SA=ohmlab-controller-vault
POLICY="$ROOT/cli/internal/seed/testdata/controller-policy.hcl"
POLICY_DELETE="$ROOT/cli/internal/seed/testdata/controller-policy-allow-delete.hcl"
VAULT_TOKEN_ROOT=root
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-$(yq '.dependencies[] | select(.name == "cert-manager") | .version' "$ROOT/argo-cd/apps/cert-manager/Chart.yaml")}"
NS_CERTS=e2e-certs
BUSYBOX_IMAGE=mirror.gcr.io/library/busybox:1.37.0
NS_STORE=e2e-store
STORAGE_SA=ohmlab-controller-storage
STORAGE_POLICY="$ROOT/cli/internal/controller/storage/testdata/storage-policy.hcl"
RUSTFS_CHART="$ROOT/argo-cd/apps/rustfs"
RUSTFS_REPO=$(yq '.dependencies[] | select(.name == "rustfs") | .repository' "$RUSTFS_CHART/Chart.yaml")
RUSTFS_VERSION=$(yq '.dependencies[] | select(.name == "rustfs") | .version' "$RUSTFS_CHART/Chart.yaml")
RUSTFS_REGION=$(yq '.rustfs.config.rustfs.region' "$RUSTFS_CHART/values.yaml")
STORE_ENDPOINT="https://rustfs-svc.$NS_STORE.svc:9000"
RCLONE_IMAGE=mirror.gcr.io/rclone/rclone:1.75.2@sha256:2687085f718d3c628f7fdfb77c52a1d344332aed543110bb88088f2c70d43eb5

k() { kubectl --context "$CTX" "$@"; }
vexec() { k -n "$NS_VAULT" exec deploy/vault -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN="$VAULT_TOKEN_ROOT" vault "$@"; }
# Same, with standard input forwarded.
vexec_in() { k -n "$NS_VAULT" exec -i deploy/vault -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN="$VAULT_TOKEN_ROOT" vault "$@"; }
step() { printf '\n== %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; dump; exit 1; }
dump() {
  k -n "$NS_CTL" get pods,deploy,leases 2>&1 | head -20 || true
  k -n "$NS_VAULT" get vaultseeds -o wide 2>&1 || true
  k -n "$NS_CERTS" get certificates,deploy -o wide 2>&1 || true
  k -n "$NS_CERTS" get events --sort-by=.lastTimestamp 2>&1 | tail -10 || true
  k -n "$NS_CTL" get s3buckets -o wide 2>&1 || true
  k -n "$NS_CTL" get events --field-selector involvedObject.kind=S3Bucket --sort-by=.lastTimestamp 2>&1 | tail -10 || true
  k -n "$NS_STORE" get pods,pvc 2>&1 || true
  k -n "$NS_CTL" get pods -o custom-columns='NAME:.metadata.name,RESTARTS:.status.containerStatuses[0].restartCount,LAST:.status.containerStatuses[0].lastState.terminated.reason,EXIT:.status.containerStatuses[0].lastState.terminated.exitCode' 2>&1 || true
  k -n "$NS_CTL" logs -l app.kubernetes.io/name=ohmlab-controller --tail=40 2>&1 || true
  k -n "$NS_CTL" logs -l app.kubernetes.io/component=storage --previous --tail=20 2>&1 || true
}
cleanup() { [ -n "${KEEP:-}" ] || [ -n "${E2E_EXISTING_CLUSTER:-}" ] || kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# eventually <seconds> <description> <command...>
eventually() {
  local deadline=$((SECONDS + $1)) what="$2"; shift 2
  until "$@" >/dev/null 2>&1; do
    [ "$SECONDS" -lt "$deadline" ] || fail "timed out waiting for $what"
    sleep 2
  done
}
ready() { [ "$(k -n "$NS_VAULT" get vaultseed "$1" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')" = True ]; }
kvget() { vexec kv get -format=json -mount=ohmlab "platforms/production/core/$1" | jq -c '.data'; }
kvfield() { kvget "$1" | jq -r ".data.$2"; }
kvversion() { kvget "$1" | jq -r '.metadata.version'; }
# Conditions for `eventually`, evaluated each time it polls.
field_is() { [ "$(kvfield "$1" "$2")" = "$3" ]; }
field_is_not() { [ "$(kvfield "$1" "$2")" != "$3" ]; }

# The storage target. A key never reaches an argument list or the output: the
# script reads it into a variable and hands it over on standard input.
record() { vexec kv get -format=json -mount=ohmlab "platforms/e2e/core/$1/s3" | jq -c '.data'; }
# claim_keys <app> <claim>: the claim's access and secret key, on one line, for `read`.
claim_keys() { record "$1" | jq -r --arg c "$2" '.data[$c] | "\(.accessKey) \(.secretKey)"'; }
# rclone_as <access key> <secret key> <rclone args...>: rclone in the rclone pod,
# as that key, from a config file it reads from standard input. An app's key
# may not create buckets: rclone must not try to before it writes.
rclone_as() {
  local ak="$1" sk="$2"; shift 2
  printf '[s3]\ntype = s3\nprovider = Other\naccess_key_id = %s\nsecret_access_key = %s\nendpoint = %s\nregion = %s\nforce_path_style = true\nno_check_bucket = true\n' \
    "$ak" "$sk" "$STORE_ENDPOINT" "$RUSTFS_REGION" \
    | k -n "$NS_CTL" exec -i rclone -- sh -c 'umask 077; cat >/tmp/rclone.conf && exec rclone --config /tmp/rclone.conf --ca-cert /ca/ca.crt --retries 1 --low-level-retries 1 "$@"' rclone "$@"
}
as_root() { rclone_as "$ROOT_USER" "$ROOT_PASSWORD" "$@"; }
# key_works <bucket> <access key> <secret key>: the key lists the bucket.
key_works() { local b="$1"; shift; rclone_as "$1" "$2" lsf "s3:$b" >/dev/null 2>&1; }
# key_refused <bucket> <code> <access key> <secret key>: listing fails with that S3 error code.
key_refused() {
  local b="$1" code="$2" out; shift 2
  out=$(rclone_as "$1" "$2" lsf "s3:$b" 2>&1) && return 1
  grep -q "$code" <<<"$out"
}
s3cond() { k -n "$NS_CTL" get s3bucket "$1" -o jsonpath="{.status.conditions[?(@.type==\"$2\")].status}"; }
s3ready() { [ "$(s3cond "$1" Ready)" = True ]; }
s3drifted() { [ "$(s3cond "$1" Drifted)" = True ]; }
digest() { printf '%s' "$1" | openssl dgst -sha256 -r | cut -d' ' -f1; }

step "cluster"
if [ -z "${E2E_EXISTING_CLUSTER:-}" ]; then
  if ! kind get clusters | grep -qx "$CLUSTER"; then kind create cluster --name "$CLUSTER" --wait 120s >/dev/null; fi
  kind load docker-image "$IMAGE" --name "$CLUSTER" >/dev/null
fi
# The Vault image is a multi-platform index: Docker's containerd image store
# cannot always export one for `kind load`. The node then pulls it itself.
kind load docker-image "$VAULT_IMAGE" --name "$CLUSTER" >/dev/null 2>&1 \
  || echo "could not preload $VAULT_IMAGE: the cluster will pull it"
k create namespace "$NS_VAULT" --dry-run=client -o yaml | k apply -f - >/dev/null
k create namespace "$NS_CTL" --dry-run=client -o yaml | k apply -f - >/dev/null

step "vault dev server with Kubernetes auth"
k apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata: {name: vault, namespace: $NS_VAULT}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: e2e-vault-auth-delegator}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: system:auth-delegator}
subjects: [{kind: ServiceAccount, name: vault, namespace: $NS_VAULT}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: vault, namespace: $NS_VAULT}
spec:
  replicas: 1
  selector: {matchLabels: {app: vault}}
  template:
    metadata: {labels: {app: vault}}
    spec:
      serviceAccountName: vault
      containers:
      - name: vault
        image: $VAULT_IMAGE
        imagePullPolicy: IfNotPresent
        args: ["server", "-dev", "-dev-root-token-id=$VAULT_TOKEN_ROOT", "-dev-listen-address=0.0.0.0:8200"]
        ports: [{containerPort: 8200}]
        readinessProbe: {httpGet: {path: /v1/sys/health, port: 8200}}
---
apiVersion: v1
kind: Service
metadata: {name: vault, namespace: $NS_VAULT}
spec: {selector: {app: vault}, ports: [{port: 8200}]}
YAML
k -n "$NS_VAULT" rollout status deploy/vault --timeout=120s >/dev/null
vexec secrets enable -path=ohmlab -version=2 kv >/dev/null
vexec auth enable kubernetes >/dev/null
vexec write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc >/dev/null
vexec_in policy write ohmlab-controller-vault - <"$POLICY" >/dev/null
# The audience is the chart's default: the role takes the controller's projected
# token and nothing else.
vexec write auth/kubernetes/role/ohmlab-controller-vault \
  bound_service_account_names="$SA" bound_service_account_namespaces="$NS_CTL" \
  policies=ohmlab-controller-vault ttl=15m audience=vault >/dev/null

step "cert-manager, and a workload that reads its certificate only at start"
helm --kube-context "$CTX" upgrade --install cert-manager cert-manager --repo https://charts.jetstack.io \
  --version "$CERT_MANAGER_VERSION" -n cert-manager --create-namespace --set crds.enabled=true --wait --timeout 5m >/dev/null
k create namespace "$NS_CERTS" --dry-run=client -o yaml | k apply -f - >/dev/null
workload() { # <name> <mount the certificate: yes|no>
  cat <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: $1, namespace: $NS_CERTS}
spec:
  selector: {matchLabels: {app: $1}}
  template:
    metadata: {labels: {app: $1}}
    spec:
      terminationGracePeriodSeconds: 1
      containers:
      - name: app
        image: $BUSYBOX_IMAGE
        command: [sleep, "3600"]
$( [ "$2" = yes ] && printf '        volumeMounts: [{name: tls, mountPath: /tls}]\n      volumes: [{name: tls, secret: {secretName: app-tls}}]' )
YAML
}
{
  cat <<YAML
apiVersion: cert-manager.io/v1
kind: Issuer
metadata: {name: selfsigned, namespace: $NS_CERTS}
spec: {selfSigned: {}}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: app-tls, namespace: $NS_CERTS}
spec:
  secretName: app-tls
  dnsNames: [app.$NS_CERTS.svc]
  issuerRef: {kind: Issuer, name: selfsigned}
---
YAML
  workload app yes
  echo ---
  workload bystander no
} | k apply -f - >/dev/null

step "a RustFS store with TLS from a CA of its own, its root keys in Vault"
k create namespace "$NS_STORE" --dry-run=client -o yaml | k apply -f - >/dev/null
k apply -f - >/dev/null <<YAML
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata: {name: e2e-selfsigned}
spec: {selfSigned: {}}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: e2e-ca, namespace: cert-manager}
spec:
  isCA: true
  commonName: e2e-ca
  secretName: e2e-ca
  privateKey: {algorithm: ECDSA, size: 256}
  issuerRef: {kind: ClusterIssuer, name: e2e-selfsigned}
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata: {name: e2e-ca}
spec: {ca: {secretName: e2e-ca}}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: rustfs-tls, namespace: $NS_STORE}
spec:
  secretName: rustfs-tls
  dnsNames: [rustfs-svc.$NS_STORE.svc, rustfs-svc.$NS_STORE.svc.cluster.local]
  issuerRef: {kind: ClusterIssuer, name: e2e-ca}
YAML
k -n "$NS_STORE" wait --for=condition=Ready certificate/rustfs-tls --timeout=120s >/dev/null || fail "the store's certificate was not issued"
# The stores' CA, where trust-manager puts it on a platform.
k -n "$NS_CTL" create configmap ohmlab-internal-ca --from-file=ca.crt=<(k -n cert-manager get secret e2e-ca -o jsonpath='{.data.tls\.crt}' | base64 -d) \
  --dry-run=client -o yaml | k apply -f - >/dev/null
ROOT_USER=$(openssl rand -hex 10)
ROOT_PASSWORD=$(openssl rand -hex 20)
k -n "$NS_STORE" create secret generic rustfs-admin --from-env-file=/dev/stdin --dry-run=client -o yaml \
  <<<"$(printf 'RUSTFS_ROOT_USER=%s\nRUSTFS_ROOT_PASSWORD=%s\n' "$ROOT_USER" "$ROOT_PASSWORD")" | k apply -f - >/dev/null
A="$ROOT_USER" S="$ROOT_PASSWORD" jq -n '{admin: {accessKey: env.A, secretKey: env.S}}' | vexec_in kv put -mount=ohmlab platforms/e2e/core/rustfs - >/dev/null
vexec_in policy write ohmlab-controller-storage - <"$STORAGE_POLICY" >/dev/null
vexec write auth/kubernetes/role/ohmlab-controller-storage \
  bound_service_account_names="$STORAGE_SA" bound_service_account_namespaces="$NS_CTL" \
  policies=ohmlab-controller-storage ttl=15m audience=vault >/dev/null
# The catalog's settings for what the e2e does not need to change; images from
# the Docker Hub mirror, and kind's storage.
rustfs_values() {
  yq -o=json '.rustfs | {"mode": .mode, "secret": .secret, "config": .config, "podSecurityContext": .podSecurityContext, "containerSecurityContext": .containerSecurityContext, "extraEnv": .extraEnv}' "$RUSTFS_CHART/values.yaml" \
    | jq '. + {
        "tls": {"enabled": true, "existingSecret": "rustfs-tls"},
        "ingress": {"enabled": false}, "gatewayApi": {"enabled": false},
        "image": {"rustfs": {"repository": "mirror.gcr.io/rustfs/rustfs"}, "initImage": {"repository": "mirror.gcr.io/library/busybox", "tag": "1.37.0"}},
        "storageclass": {"name": "standard", "dataStorageSize": "1Gi", "logStorageSize": "256Mi"},
        "resources": {"requests": {"cpu": "100m", "memory": "256Mi"}, "limits": {"memory": "1Gi"}}
      }'
}
install_store() {
  helm --kube-context "$CTX" upgrade --install rustfs rustfs --repo "$RUSTFS_REPO" --version "$RUSTFS_VERSION" \
    -n "$NS_STORE" -f <(rustfs_values) --wait --timeout 5m >/dev/null || fail "RustFS did not install"
}
install_store
# A client in the controller's namespace, which reads the stores' CA.
k apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata: {name: rclone, namespace: $NS_CTL}
spec:
  terminationGracePeriodSeconds: 1
  securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, seccompProfile: {type: RuntimeDefault}}
  containers:
  - name: rclone
    image: $RCLONE_IMAGE
    command: [sleep, "86400"]
    env: [{name: HOME, value: /tmp}]
    securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
    volumeMounts: [{name: tmp, mountPath: /tmp}, {name: ca, mountPath: /ca, readOnly: true}]
  volumes: [{name: tmp, emptyDir: {}}, {name: ca, configMap: {name: ohmlab-internal-ca}}]
YAML
k -n "$NS_CTL" wait --for=condition=Ready pod/rclone --timeout=120s >/dev/null || fail "the rclone pod did not start"
as_root lsd s3: >/dev/null 2>&1 || fail "the root keys cannot reach the store over TLS"

step "install the chart ($MODE)"
# The chart refuses an image that is not pinned by digest. The e2e runs one it
# just built, which the node knows by name only: hence the knob, and the Never
# pull policy.
args=(--set "image=$IMAGE" --set unsafeAllowUnpinnedImage=true --set imagePullPolicy=Never
  --set targets.vault.enabled=true --set resync=15s --set monitoring.enabled=false
  --set "targets.vault.backend.address=http://vault.$NS_VAULT.svc:8200" --set targets.vault.backend.caSecret=
  --set targets.vault.allowDelete=true
  --set targets.certificates.enabled=true
  --set-json "targets.certificates.restarts=[{\"namespace\":\"$NS_CERTS\",\"certificate\":\"app-tls\",\"workloads\":[{\"kind\":\"Deployment\",\"name\":\"app\"}]}]"
  --set targets.storage.enabled=true --set targets.storage.observeOnly=true --set targets.storage.allowDelete=true
  --set targets.storage.rotationGrace=1m
  --set "targets.storage.backend.address=http://vault.$NS_VAULT.svc:8200" --set targets.storage.backend.caSecret=
  --set-json "targets.storage.stores=$(jq -nc --arg ep "$STORE_ENDPOINT" --arg region "$RUSTFS_REGION" --arg ns "$NS_STORE" \
    '[{name: "e2e", type: "rustfs", endpoint: $ep, region: $region, pathStyle: true, capacity: "1Gi", namespace: $ns, rootCredentials: {path: "ohmlab/platforms/e2e/core/rustfs", key: "admin"}}]')")
# The storage target holds the stores' root keys: grouped, it still runs alone.
[ "$MODE" = single ] && args+=(--set "deployments.all={certificates,vault}" --set "deployments.storage={storage}")
helm --kube-context "$CTX" upgrade --install ohmlab-controller "$ROOT/argo-cd/apps/ohmlab-controller" -n "$NS_CTL" "${args[@]}" >/dev/null
DEPLOY=ohmlab-controller-vault
CERTS_DEPLOY=ohmlab-controller-certificates
[ "$MODE" = single ] && DEPLOY=ohmlab-controller-all && CERTS_DEPLOY=ohmlab-controller-all
SA_EXPECT=$DEPLOY
if [ "$MODE" = single ]; then
  # The Vault role is bound to the ServiceAccount of the deployment.
  vexec write auth/kubernetes/role/ohmlab-controller-vault bound_service_account_names="$SA_EXPECT" \
    bound_service_account_namespaces="$NS_CTL" policies=ohmlab-controller-vault ttl=15m audience=vault >/dev/null
fi
k -n "$NS_CTL" rollout status "deploy/$DEPLOY" --timeout=120s >/dev/null || fail "the controller did not become ready"
k -n "$NS_CTL" rollout status "deploy/$CERTS_DEPLOY" --timeout=120s >/dev/null || fail "the certificates target did not become ready"
k -n "$NS_CTL" rollout status "deploy/$STORAGE_SA" --timeout=120s >/dev/null || fail "the storage target did not become ready"

step "first sight rolls the listed workload"
revision_of() { k -n "$NS_CERTS" get deploy "$1" -o jsonpath='{.spec.template.metadata.annotations.ohmlab\.fr/certificate-revision}'; }
carries() { [ "$(revision_of "$1")" = "$2" ]; }
generation() { k -n "$NS_CERTS" get deploy "$1" -o jsonpath='{.metadata.generation}'; }
eventually 120 "app to carry revision 1" carries app 1
[ -z "$(revision_of bystander)" ] || fail "bystander carries a revision"

step "the controller may touch only what it lists"
can() { k auth can-i "$@" -n "$NS_CERTS" --as="system:serviceaccount:$NS_CTL:$CERTS_DEPLOY" >/dev/null 2>&1; }
can patch deployment/app || fail "the target may not patch the workload it lists"
! can patch deployment/bystander || fail "the target may patch a workload it does not list"
! can list deployments || fail "the target may list Deployments"
! can get secrets || fail "the target may read Secrets"

step "the API server lets the target change its annotation and nothing else"
as_target() { k --as="system:serviceaccount:$NS_CTL:$CERTS_DEPLOY" -n "$NS_CERTS" "$@"; }
# What the controller sends: its annotation, here at its current value.
as_target patch deploy app --type=merge -p '{"spec":{"template":{"metadata":{"annotations":{"ohmlab.fr/certificate-revision":"1"}}}}}' >/dev/null \
  || fail "the target may not set its own annotation"
# Anything else is refused at admission (a server-side dry run is admitted
# like a write, and changes nothing if it were let through).
is_refused() { # <patch type> <patch>
  local out
  out=$(as_target patch deploy app --dry-run=server --type="$1" -p "$2" 2>&1) && return 1
  grep -q 'may change only' <<<"$out"
}
refused() { # <description> <patch type> <patch>
  is_refused "$2" "$3" || fail "the target may $1, or was refused for another reason"
}
IMAGE_PATCH='[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":"mirror.gcr.io/library/busybox:1.36.1"}]'
# The API server loads a new policy within seconds.
eventually 30 "the admission policy to refuse an image change" is_refused json "$IMAGE_PATCH"
refused "add another pod annotation" merge '{"spec":{"template":{"metadata":{"annotations":{"other":"x"}}}}}'
refused "label the workload" merge '{"metadata":{"labels":{"other":"x"}}}'
refused "change the replicas" merge '{"spec":{"replicas":2}}'

step "a renewal rolls the workload onto the new certificate"
secret_serial() { k -n "$NS_CERTS" get secret app-tls -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -serial; }
# The certificate a running pod of the workload mounts, not one terminating.
pod_serial() {
  local pod
  pod=$(k -n "$NS_CERTS" get pods -l app=app -o json | jq -r '[.items[] | select(.metadata.deletionTimestamp == null and .status.phase == "Running")] | sort_by(.metadata.creationTimestamp) | last | .metadata.name')
  k -n "$NS_CERTS" exec "$pod" -- cat /tls/tls.crt | openssl x509 -noout -serial
}
serial_before=$(secret_serial)
# What `cmctl renew` does: ask cert-manager to issue again.
k -n "$NS_CERTS" patch certificate app-tls --subresource=status --type=json -p "[{\"op\":\"add\",\"path\":\"/status/conditions/-\",\"value\":{\"type\":\"Issuing\",\"status\":\"True\",\"reason\":\"ManuallyTriggered\",\"message\":\"e2e renewal\",\"lastTransitionTime\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}]" >/dev/null
revision_is() { [ "$(k -n "$NS_CERTS" get certificate app-tls -o jsonpath='{.status.revision}')" = "$1" ]; }
eventually 120 "the Certificate to reach revision 2" revision_is 2
eventually 60 "app to carry revision 2" carries app 2
k -n "$NS_CERTS" rollout status deploy/app --timeout=120s >/dev/null || fail "app did not roll"
serial_after=$(secret_serial)
[ "$serial_after" != "$serial_before" ] || fail "cert-manager did not issue a new certificate"
pod_has_new() { [ "$(pod_serial)" = "$serial_after" ]; }
eventually 60 "the new pod to serve the renewed certificate" pod_has_new

step "a resync rolls nothing"
gen=$(generation app)
sleep 50
[ "$(generation app)" = "$gen" ] || fail "a resync rolled app again"

step "objects become Ready under the controller's policy"
k apply -f "$ROOT/scripts/ci/fixtures/ohmlab-controller-e2e.yaml" >/dev/null
eventually 90 "keycloak Ready" ready keycloak
eventually 90 "grafana Ready" ready grafana
[ "$(kvfield keycloak admin.username)" = admin ] || fail "admin.username"
[ "$(kvfield keycloak admin.password | wc -c)" -eq 25 ] || fail "admin.password length"
[ "$(kvfield grafana oidc.clientSecret)" = "$(kvfield keycloak clients.grafana.clientSecret)" ] || fail "the reference differs from its source"
[ "$(kvfield grafana age.secret | cut -c1-15)" = AGE-SECRET-KEY- ] || fail "age secret"
OWNER=$(vexec kv metadata get -format=json -mount=ohmlab platforms/production/core/keycloak | jq -r '.data.custom_metadata["ohmlab.fr/owner"]')
[ "$OWNER" = "$NS_VAULT/keycloak" ] || fail "owner marker is '$OWNER'"

step "the role takes a token meant for Vault and refuses the API token"
# The controller became Ready under this role, so it logs in with its projected
# token; this shows the role would refuse the token every pod is given.
login() { vexec_in write -field=token_policies auth/kubernetes/login role=ohmlab-controller-vault jwt=- >/dev/null 2>&1; }
for_vault=$(k -n "$NS_CTL" create token "$SA_EXPECT" --audience vault) || fail "could not create a Vault-audience token"
for_api=$(k -n "$NS_CTL" create token "$SA_EXPECT") || fail "could not create an API token"
login <<<"$for_vault" || fail "a Vault-audience token of the controller's ServiceAccount must log in"
if login <<<"$for_api"; then
  fail "the role must refuse the ServiceAccount's API token"
fi

step "a resync that finds nothing to do writes nothing"
v1=$(kvversion keycloak); v2=$(kvversion grafana)
rv=$(k -n "$NS_VAULT" get vaultseed keycloak -o jsonpath='{.metadata.resourceVersion}')
sleep 50   # more than three resyncs at 15s
[ "$(kvversion keycloak)" = "$v1" ] && [ "$(kvversion grafana)" = "$v2" ] || fail "a resync moved a KV version"
[ "$(k -n "$NS_VAULT" get vaultseed keycloak -o jsonpath='{.metadata.resourceVersion}')" = "$rv" ] || fail "a resync wrote to the API"

step "drift: a hand-edited reference is put back, a placeholder survives, a deleted key returns"
good=$(kvfield grafana oidc.clientSecret)
edited=$(kvget grafana | jq -c '.data | .oidc.clientSecret="hand-edited"')
vexec_in kv put -mount=ohmlab platforms/production/core/grafana - <<<"$edited" >/dev/null
eventually 60 "the reference to be put back" field_is grafana oidc.clientSecret "$good"
kvdata=$(kvget keycloak | jq -c '.data | .webhook="https://hooks.example/x" | del(.clients.grafana.clientId)')
vexec_in kv put -mount=ohmlab platforms/production/core/keycloak - <<<"$kvdata" >/dev/null
eventually 60 "the deleted key to come back" field_is_not keycloak clients.grafana.clientId null
[ "$(kvfield keycloak webhook)" = "https://hooks.example/x" ] || fail "a hand-filled placeholder was overwritten"

step "rotation happens once"
before=$(kvfield keycloak admin.password)
hash_before=$(kvfield keycloak admin.passwordHashed)
k -n "$NS_VAULT" patch vaultseed keycloak --type merge -p '{"spec":{"rotate":{"token":"r1","keys":["admin.password"]}}}' >/dev/null
eventually 60 "the password to rotate" field_is_not keycloak admin.password "$before"
after=$(kvfield keycloak admin.password); ver=$(kvversion keycloak)
sleep 40
[ "$(kvversion keycloak)" = "$ver" ] || fail "a recorded rotation token rotated again"
[ "$(kvfield keycloak admin.password)" = "$after" ] || fail "the password moved"
[ "$(kvfield keycloak admin.passwordHashed)" != "$hash_before" ] || fail "the bcrypt hash did not follow the rotated password"

step "deletion policies"
# Without allowDelete in the platform's Vault policy, deleting is refused ...
k apply -f - >/dev/null <<YAML
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: refused, namespace: $NS_VAULT}
spec: {path: ohmlab/platforms/production/core/refused, deletionPolicy: Delete, data: {a: "<random:8>"}}
YAML
eventually 60 "refused Ready" ready refused
k -n "$NS_VAULT" delete vaultseed refused --wait=false >/dev/null
eventually 60 "the refusal to be reported" test "$(k -n "$NS_VAULT" get vaultseed refused -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}')" = Forbidden
# ... and works once the policy grants it. The finalizer was kept, so the same object finishes.
vexec_in policy write ohmlab-controller-vault - <"$POLICY_DELETE" >/dev/null
eventually 90 "the refused object to be removed" bash -c "! kubectl --context $CTX -n $NS_VAULT get vaultseed refused"
vexec kv get -mount=ohmlab platforms/production/core/refused >/dev/null 2>&1 && fail "the path outlived its object"
k apply -f - >/dev/null <<YAML
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: doomed, namespace: $NS_VAULT}
spec: {path: ohmlab/platforms/production/core/doomed, deletionPolicy: Delete, data: {a: "<random:8>"}}
---
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: kept, namespace: $NS_VAULT}
spec: {path: ohmlab/platforms/production/core/kept, data: {a: "<random:8>"}}
YAML
eventually 60 "doomed Ready" ready doomed
eventually 60 "kept Ready" ready kept
k -n "$NS_VAULT" delete vaultseed doomed kept --wait=true --timeout=60s >/dev/null || fail "deleting blocked"
vexec kv get -mount=ohmlab platforms/production/core/doomed >/dev/null 2>&1 && fail "deletionPolicy Delete left the path"
vexec kv get -mount=ohmlab platforms/production/core/kept >/dev/null 2>&1 || fail "Retain removed the path"

step "storage: observe-only plans every claim and writes nothing"
k apply -f "$ROOT/scripts/ci/fixtures/ohmlab-controller-e2e-storage.yaml" >/dev/null
for b in ohmlab-app1-pg ohmlab-app2-pg ohmlab-app1-files; do eventually 90 "$b to be Drifted" s3drifted "$b"; done
buckets=$(as_root lsd s3:) || fail "the root keys cannot list the store"
[ -z "$buckets" ] || fail "observe-only created a bucket"
for app in app1 app2; do
  if record "$app" >/dev/null 2>&1; then fail "observe-only wrote the record of $app"; fi
done
helm --kube-context "$CTX" upgrade ohmlab-controller "$ROOT/argo-cd/apps/ohmlab-controller" -n "$NS_CTL" --reuse-values \
  --set targets.storage.observeOnly=false >/dev/null
k -n "$NS_CTL" rollout status "deploy/$STORAGE_SA" --timeout=120s >/dev/null || fail "the storage target did not roll"
for b in ohmlab-app1-pg ohmlab-app2-pg ohmlab-app1-files; do eventually 120 "$b to be Ready" s3ready "$b"; done

step "storage: a claim's key works on its bucket only"
read -r AK1 SK1 < <(claim_keys app1 pg)
[ -n "$AK1" ] && [ -n "$SK1" ] || fail "app1's record holds no pg key"
eventually 30 "app1's key to write its bucket" rclone_as "$AK1" "$SK1" touch s3:ohmlab-app1-pg/probe
rclone_as "$AK1" "$SK1" cat s3:ohmlab-app1-pg/probe >/dev/null 2>&1 || fail "app1's key cannot read its bucket"
[ "$(rclone_as "$AK1" "$SK1" lsf s3:ohmlab-app1-pg 2>/dev/null)" = probe ] || fail "app1's key cannot list its bucket"
rclone_as "$AK1" "$SK1" deletefile s3:ohmlab-app1-pg/probe >/dev/null 2>&1 || fail "app1's key cannot delete in its bucket"
key_refused ohmlab-app2-pg AccessDenied "$AK1" "$SK1" || fail "app1's key must be refused app2's bucket with AccessDenied"

step "storage: the record holds what the app needs"
rec=$(record app1)
for f in accessKey secretKey endpoint bucket region pathStyle ca; do
  [ -n "$(jq -r --arg f "$f" '.data.pg[$f] // empty' <<<"$rec")" ] || fail "the record lacks pg.$f"
done
[ "$(jq -r '.data.pg.bucket' <<<"$rec")" = ohmlab-app1-pg ] || fail "the record names another bucket"
[ "$(jq -r '.data.pg.endpoint' <<<"$rec")" = "$STORE_ENDPOINT" ] || fail "the record names another endpoint"
[ "$(digest "$(jq -r '.data.pg.ca' <<<"$rec")")" = "$(digest "$(k -n "$NS_CTL" get configmap ohmlab-internal-ca -o jsonpath='{.data.ca\.crt}')")" ] \
  || fail "the record's ca is not the stores' CA"
owner=$(vexec kv metadata get -format=json -mount=ohmlab platforms/e2e/core/app1/s3 | jq -c '.data.custom_metadata | {o: .["ohmlab.fr/owner"], pg: .["ohmlab.fr/claim.pg"], files: .["ohmlab.fr/claim.files"]}')
[ "$owner" = "{\"o\":\"$STORAGE_SA\",\"pg\":\"$NS_CTL/ohmlab-app1-pg\",\"files\":\"$NS_CTL/ohmlab-app1-files\"}" ] || fail "the record's markers are $owner"

step "storage: a rotation keeps the old key until its grace ends"
read -r OLD_AK OLD_SK < <(claim_keys app1 pg)
k -n "$NS_CTL" patch s3bucket ohmlab-app1-pg --type merge -p '{"spec":{"rotate":{"token":"r1"}}}' >/dev/null
rotated() { [ "$(record app1 | jq -r '.data.pg.accessKey')" != "$OLD_AK" ]; }
eventually 60 "the key to rotate" rotated
read -r AK1 SK1 < <(claim_keys app1 pg)
eventually 60 "the new key to work" key_works ohmlab-app1-pg "$AK1" "$SK1"
key_works ohmlab-app1-pg "$OLD_AK" "$OLD_SK" || fail "the old key stopped before its grace"
# The grace is 1m; the reconcile after it removes the old key.
eventually 150 "the old key to be refused" key_refused ohmlab-app1-pg InvalidAccessKeyId "$OLD_AK" "$OLD_SK"
key_works ohmlab-app1-pg "$AK1" "$SK1" || fail "the new key stopped with the old one"

step "storage: a store rebuilt empty gets everything back from Vault"
helm --kube-context "$CTX" uninstall rustfs -n "$NS_STORE" --wait >/dev/null
k -n "$NS_STORE" delete pvc --all --wait=true >/dev/null
install_store
# Objects that failed while the store was away are in backoff: a few resyncs.
eventually 240 "app1's key to read its bucket again" key_works ohmlab-app1-pg "$AK1" "$SK1"
[ -z "$(rclone_as "$AK1" "$SK1" lsf s3:ohmlab-app1-pg 2>/dev/null)" ] || fail "the rebuilt bucket holds objects"
read -r AK2 SK2 < <(claim_keys app2 pg)
eventually 60 "app2's key to come back" key_works ohmlab-app2-pg "$AK2" "$SK2"

step "storage: deletion keeps a bucket that holds data, and Retain keeps everything"
read -r FAK FSK < <(claim_keys app1 files)
eventually 60 "the files key to write its bucket" rclone_as "$FAK" "$FSK" touch s3:ohmlab-app1-files/kept
k -n "$NS_CTL" delete s3bucket ohmlab-app1-files --wait=true --timeout=90s >/dev/null || fail "deleting ohmlab-app1-files blocked"
eventually 30 "the files key to be refused" key_refused ohmlab-app1-files InvalidAccessKeyId "$FAK" "$FSK"
[ -z "$(record app1 | jq -r '.data.files // empty')" ] || fail "the record kept the files key"
[ "$(vexec kv metadata get -format=json -mount=ohmlab platforms/e2e/core/app1/s3 | jq -r '.data.custom_metadata["ohmlab.fr/claim.files"] // empty')" = "" ] \
  || fail "the record kept the files marker"
as_root lsd s3: 2>/dev/null | grep -q ohmlab-app1-files || fail "a bucket that holds data was removed"
k -n "$NS_CTL" delete s3bucket ohmlab-app2-pg --wait=true --timeout=60s >/dev/null || fail "deleting ohmlab-app2-pg blocked"
sleep 20   # a resync
key_works ohmlab-app2-pg "$AK2" "$SK2" || fail "Retain removed app2's key"
# Its writes derive 64 MiB keys: through all of them, it held in its memory limit.
restarts=$(k -n "$NS_CTL" get pods -l app.kubernetes.io/component=storage -o jsonpath='{range .items[*]}{.status.containerStatuses[0].restartCount}{"\n"}{end}' | sort -u)
[ "$restarts" = 0 ] || fail "the storage target restarted: $(k -n "$NS_CTL" get pods -l app.kubernetes.io/component=storage -o jsonpath='{.items[*].status.containerStatuses[0].lastState.terminated.reason}')"

step "a rollout never runs the target twice and writes nothing"
lease_holder() { k -n "$NS_CTL" get lease ohmlab-controller-vault -o jsonpath='{.spec.holderIdentity}'; }
new_leader() { local h; h=$(lease_holder); [ -n "$h" ] && [ "$h" != "$holder_before" ]; }
holder_before=$(lease_holder)
v1=$(kvversion keycloak)
app_gen=$(generation app)
storage_holder() { k -n "$NS_CTL" get lease "$STORAGE_SA" -o jsonpath='{.spec.holderIdentity}'; }
storage_new_leader() { local h; h=$(storage_holder); [ -n "$h" ] && [ "$h" != "$storage_before" ]; }
storage_before=$(storage_holder)
record_v=$(record app1 | jq -r '.metadata.version')
k -n "$NS_CTL" rollout restart "deploy/$DEPLOY" >/dev/null
[ "$CERTS_DEPLOY" = "$DEPLOY" ] || k -n "$NS_CTL" rollout restart "deploy/$CERTS_DEPLOY" >/dev/null
k -n "$NS_CTL" rollout restart "deploy/$STORAGE_SA" >/dev/null
k -n "$NS_CTL" rollout status "deploy/$DEPLOY" --timeout=120s >/dev/null
k -n "$NS_CTL" rollout status "deploy/$CERTS_DEPLOY" --timeout=120s >/dev/null
k -n "$NS_CTL" rollout status "deploy/$STORAGE_SA" --timeout=120s >/dev/null
# The outgoing pod releases the Lease before the new one takes it: wait for a holder.
eventually 60 "the Lease to change hands" new_leader
holder_after=$(lease_holder)
eventually 60 "the storage Lease to change hands" storage_new_leader
eventually 60 "keycloak Ready after the rollout" ready keycloak
[ "$(kvversion keycloak)" = "$v1" ] || fail "the rollout wrote to Vault"
# The storage leader's counters start at zero: once it has reconciled, none
# of its writes may have moved.
smetrics() {
  local holder
  holder=$(storage_holder)
  [ -n "$holder" ] || return 1
  k -n "$NS_CTL" get --raw "/api/v1/namespaces/$NS_CTL/pods/${holder%%_*}:8080/proxy/metrics"
}
s3_reported_ready() { local m; m=$(smetrics) || return 1; grep -q '^ohmlab_object_ready{.*name="ohmlab-app1-pg".*target="storage"} 1$' <<<"$m"; }
eventually 90 "ohmlab-app1-pg reported ready after the rollout" s3_reported_ready
[ "$(record app1 | jq -r '.metadata.version')" = "$record_v" ] || fail "the rollout wrote a record"
if grep '^ohmlab_backend_writes_total{.*target="storage"' <<<"$(smetrics)" | grep -qv ' 0$'; then
  fail "the new storage process wrote to the store or to Vault"
fi

step "probes and metrics"
# The pod that leads (the identity is "<pod>_<uuid>"); an old one may still be terminating.
pod=${holder_after%%_*}
# The gauge is labelled: it exists once the new leader has reconciled for the first time.
# Read the whole body first: `grep -q` would close the pipe early and, with pipefail, fail the pipeline.
has_success_metric() {
  local metrics
  metrics=$(k -n "$NS_CTL" get --raw "/api/v1/namespaces/$NS_CTL/pods/$pod:8080/proxy/metrics") || return 1
  grep -q '^ohmlab_reconcile_last_success_timestamp_seconds' <<<"$metrics"
}
eventually 60 "the success metric" has_success_metric
k -n "$NS_CTL" get --raw "/api/v1/namespaces/$NS_CTL/pods/$pod:8081/proxy/readyz" >/dev/null || fail "readyz"
# The certificates target's leader, after its own rollout: its counters start
# at zero, and it has nothing to roll.
# Its Lease may still be changing hands: read the holder at each poll.
cmetrics() {
  local holder
  holder=$(k -n "$NS_CTL" get lease ohmlab-controller-certificates -o jsonpath='{.spec.holderIdentity}')
  [ -n "$holder" ] || return 1
  k -n "$NS_CTL" get --raw "/api/v1/namespaces/$NS_CTL/pods/${holder%%_*}:8080/proxy/metrics"
}
certificate_ready() { local m; m=$(cmetrics) || return 1; grep -q '^ohmlab_object_ready{.*name="app-tls".*target="certificates"} 1$' <<<"$m"; }
eventually 60 "the Certificate to be reported ready" certificate_ready
if grep '^ohmlab_workload_restarts_total{.*name="app"' <<<"$(cmetrics)" | grep -qv ' 0$'; then
  fail "the new process rolled app again"
fi
[ "$(generation app)" = "$app_gen" ] || fail "the rollout rolled app again"
# The storage target's gauges: the claim left, its quota, the store's capacity.
m=$(smetrics) || fail "the storage target's metrics"
grep -q '^ohmlab_storage_bucket_quota_bytes{bucket="ohmlab-app1-pg",store="e2e"} 6.7108864e+07$' <<<"$m" || fail "ohmlab_storage_bucket_quota_bytes of ohmlab-app1-pg"
grep -q '^ohmlab_storage_capacity_bytes{store="e2e"} 1.073741824e+09$' <<<"$m" || fail "ohmlab_storage_capacity_bytes of the store"
grep -q '^ohmlab_object_ready{.*name="ohmlab-app2-pg"' <<<"$m" && fail "a deleted S3Bucket is still reported"
# The PodMonitor drops a whole scrape above its sample limit.
series=$(grep -vc '^#' <<<"$m")
limit=$(yq '.monitoring.sampleLimit' "$ROOT/argo-cd/apps/ohmlab-controller/values.yaml")
echo "storage target: $series series (sample limit $limit)"
[ "$series" -lt "$limit" ] || fail "the storage target exposes $series series, the PodMonitor keeps at most $limit"
storage_errors() { smetrics | { grep '^ohmlab_backend_requests_total{.*result="error".*target="storage"' || true; } | awk '{s += $2} END {print s + 0}'; }
errors_before=$(storage_errors)
sleep 35   # two resyncs
[ "$(storage_errors)" = "$errors_before" ] || fail "the storage target's backend errors grew while all was settled"

echo
echo "e2e ($MODE): ok"
