#!/usr/bin/env bash
# Decision table for scripts/ci/check-example-secrets.sh on hand-written
# manifests.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-example-secrets.sh"
fail=0

# check <label> <expected exit> <manifests> [<ignoreDifferences JSON> [<app>]]
check() {
  local out rc
  out=$(printf '%s\n' "$3" | "$SCRIPT" --stdin "${5:-tenant/app}" "${4:-[]}" 2>&1); rc=$?
  if [ "$rc" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1; fi
}

reads() { # reads <Secret> <key> [optional]: a Deployment reading one key of a Secret
  cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: reader}
spec:
  template:
    spec:
      containers:
      - name: c
        env:
        - name: X
          valueFrom:
            secretKeyRef: {name: $1, key: $2${3:+, optional: true}}
EOF
}
loads() { # loads <Secret>: a Deployment loading a whole Secret into its environment
  cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: loader}
spec:
  template:
    spec:
      containers:
      - name: c
        envFrom:
        - secretRef: {name: $1}
EOF
}
mounts() { # mounts <Secret>: a Deployment mounting a Secret
  cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: mounter}
spec:
  template:
    spec:
      volumes:
      - name: v
        secret: {secretName: $1}
      containers: [{name: c}]
EOF
}
secret() { # secret <name> <key>
  printf -- '---\napiVersion: v1\nkind: Secret\nmetadata: {name: %s}\nstringData: {%s: x}\n' "$1" "$2"
}
vss() { # vss <destination> <key>...: a VaultStaticSecret writing those keys only
  local d=$1 k
  shift
  printf -- '---\napiVersion: secrets.hashicorp.com/v1beta1\nkind: VaultStaticSecret\nmetadata: {name: vss-%s}\nspec:\n  destination:\n    name: %s\n    create: true\n    transformation:\n      excludeRaw: true\n      excludes: [".*"]\n      templates:\n' "${VSS_NAME:-$d}" "$d"
  for k in "$@"; do printf '        %s: {text: x}\n' "$k"; done
}
cluster() { # cluster <name>: a CNPG Cluster
  printf -- '---\napiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nmetadata: {name: %s}\nspec: {instances: 1}\n' "$1"
}
objectstore() { # objectstore <Secret>: a barman ObjectStore reading its S3 keys from a Secret
  cat <<EOF
---
apiVersion: barmancloud.cnpg.io/v1
kind: ObjectStore
metadata: {name: backups}
spec:
  configuration:
    s3Credentials:
      accessKeyId: {name: $1, key: ACCESS_KEY_ID}
      secretAccessKey: {name: $1, key: ACCESS_SECRET_KEY}
EOF
}
certificate() { # certificate <Secret>: a cert-manager Certificate writing that Secret
  printf -- '---\napiVersion: cert-manager.io/v1\nkind: Certificate\nmetadata: {name: cert}\nspec: {secretName: %s}\n' "$1"
}
leaves_vso='[{"group":"","kind":"Secret","name":"s","jqPathExpressions":[".data"],"managedFieldsManagers":["vault-secrets-operator"]}]'
data_only='[{"group":"","kind":"Secret","name":"s","jqPathExpressions":[".data"]}]'

check "no Secret reference at all passes" 0 "kind: ConfigMap"
check "a key of a Secret the app renders passes" 0 "$(reads s k; secret s k)"
check "a key a VaultStaticSecret writes passes" 0 "$(reads s k; vss s k)"
check "a Secret nothing in the app writes is refused" 1 "$(reads s k)"
check "an optional reference to a missing Secret passes" 0 "$(reads s k optional)"
check "a key no writer of the Secret sets is refused" 1 "$(reads s other; vss s k)"
check "a Secret loaded whole that nothing writes is refused" 1 "$(loads s)"
check "a mounted Secret that a Certificate writes passes" 0 "$(mounts tls; certificate tls)"
check "the Secrets CNPG generates for a Cluster count as written" 0 "$(reads db-app password; cluster db)"
check "an ObjectStore reading a Secret nothing writes is refused" 1 "$(objectstore creds)"
check "an ObjectStore reading the keys a VaultStaticSecret writes passes" 0 "$(objectstore creds; vss creds ACCESS_KEY_ID ACCESS_SECRET_KEY)"
check "a Secret the chart renders and VSO writes is refused" 1 "$(secret s k; vss s k)"
check "it passes once Argo CD leaves VSO its fields and the data" 0 "$(secret s k; vss s k)" "$leaves_vso"
check "it is still refused with only the data ignored, as VSO's labels drift" 1 "$(secret s k; vss s k)" "$data_only"
check "two VaultStaticSecrets writing one Secret are refused" 1 "$(VSS_NAME=one vss s k; VSS_NAME=two vss s j)"
check "a Secret its app creates at runtime is known by name" 0 "$(reads prometheus-stack-kube-prom-admission cert)" "[]" core/prometheus-stack
check "that name is known to its own app only" 1 "$(reads prometheus-stack-kube-prom-admission cert)" "[]" tenant/other
check "it is known under an instance's label too" 0 "$(reads prometheus-stack-kube-prom-admission cert)" "[]" lab/core/prometheus-stack

# --instances: each enabled entry of every instance of a directory, labelled
# with its instance, its chart from APPS_DIR.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/apps/reader/templates" "$work/apps/writer/templates"
printf 'apiVersion: v2\nname: reader\nversion: 0.1.0\n' >"$work/apps/reader/Chart.yaml"
reads missing k >"$work/apps/reader/templates/reader.yaml"
printf 'apiVersion: v2\nname: writer\nversion: 0.1.0\n' >"$work/apps/writer/Chart.yaml"
{ reads s k; secret s k; } >"$work/apps/writer/templates/writer.yaml"
d="$work/repo/argo-cd/instances/lab"
mkdir -p "$d/values/tenant"
printf 'instance: {name: lab}\nrepoURL: https://git.example/x.git\n' >"$d/instance.yaml"
printf 'apps: []\n' >"$d/core.yaml"
printf 'apps:\n- {app: reader, enabled: "true"}\n- {app: writer, enabled: "true"}\n- {app: idle, chart: reader, enabled: "false"}\n' >"$d/tenant.yaml"
for a in reader writer idle; do printf '{}\n' >"$d/values/tenant/$a.yaml"; done
out=$(APPS_DIR="$work/apps" "$SCRIPT" --instances "$work/repo/argo-cd/instances" 2>&1); rc=$?
if [ "$rc" = 1 ] && grep -qF 'FAIL lab/tenant/reader' <<<"$out" && grep -qF 'ok   lab/tenant/writer' <<<"$out" && ! grep -qF 'lab/tenant/idle' <<<"$out"; then
  echo "ok   --instances checks each enabled app of every instance, labelled with its instance"
else
  echo "FAIL --instances: exit $rc"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
fi

exit "$fail"
