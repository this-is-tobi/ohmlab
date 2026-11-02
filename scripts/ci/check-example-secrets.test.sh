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

exit "$fail"
