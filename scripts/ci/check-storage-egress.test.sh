#!/usr/bin/env bash
# Decision table for scripts/ci/check-storage-egress.sh on hand-written render
# directories: a claim's egress policy passes where another egress policy of
# its namespace already selects its pods, and fails where it would be their
# only one.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-storage-egress.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# app <render> <tier/app> <namespace> <manifest>: one app of instance lab, its
# objects and its destination (an empty namespace is left out, as for the
# claims).
app() {
  mkdir -p "$1/lab/${2%/*}"
  printf '%s\n' "$4" >"$1/lab/$2.yaml"
  printf 'https://kubernetes.default.svc%s\n' "${3:+ $3}" >"$1/lab/$2.destination"
}
# check <description> <rc> <text> <render>
check() {
  local out rc
  out=$("$SCRIPT" "$4" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1 (rc $rc, want $2, looking for '$3')"; sed 's/^/     /' <<<"$out"; fail=1
  fi
}
# policy <name> <podSelector> <policyTypes, or empty> [<namespace>]
policy() {
  printf 'apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n  name: %s\n%sspec:\n  podSelector: %s\n' \
    "$1" "${4:+  namespace: $4
}" "$2"
  [ -n "$3" ] && printf '  policyTypes: %s\n' "$3"
  printf '  egress:\n  - to:\n    - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: store}}\n'
}

PG='{matchLabels: {cnpg.io/cluster: db-pg-cluster}}'
CLAIM=$(policy ohmlab-storage-pg "$PG" '[Egress]' db)
OK="ok   lab: db/ohmlab-storage-pg adds to its pods' egress policies"
ONLY="FAIL lab: db/ohmlab-storage-pg would be the only egress policy of its pods"

r="$work/deny"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy default-deny '{}' '[Ingress, Egress]')"
check "a namespace that denies egress by default passes" 0 "$OK" "$r"

r="$work/open"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db 'apiVersion: v1
kind: ConfigMap
metadata: {name: cm}'
check "a namespace without an egress policy fails" 1 "$ONLY" "$r"

r="$work/same-pods"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy allow-apiserver "$PG" '[Egress]')"
check "another egress policy of the same pods passes" 0 "$OK" "$r"

r="$work/fewer-labels"; app "$r" core/storage-claims "" "$(policy ohmlab-storage-pg '{matchLabels: {cnpg.io/cluster: db-pg-cluster, cnpg.io/podRole: instance}}' '[Egress]' db)"
app "$r" core/db db "$(policy allow-apiserver "$PG" '[Egress]')"
check "a policy selecting a superset of the pods passes" 0 "$OK" "$r"

r="$work/other-pods"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy allow-web '{matchLabels: {app: web}}' '[Egress]')"
check "an egress policy of other pods fails" 1 "$ONLY" "$r"

r="$work/expressions"; app "$r" core/storage-claims "" "$CLAIM"
app "$r" core/db db "$(policy allow-pg '{matchExpressions: [{key: cnpg.io/cluster, operator: Exists}]}' '[Egress]')"
check "a selector with expressions the claim's lacks fails" 1 "$ONLY" "$r"

r="$work/ingress-only"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy deny-in '{}' '[Ingress]')"
check "an ingress-only policy fails" 1 "$ONLY" "$r"

r="$work/implied"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy allow-out '{}' '')"
check "egress rules without policyTypes count as egress" 0 "$OK" "$r"

r="$work/two-claims"; app "$r" core/storage-claims "" "$CLAIM
---
$(policy ohmlab-storage-data "$PG" '[Egress]' db)"
check "two claims' policies do not count for each other" 2 "FAIL lab: db/ohmlab-storage-data would be the only egress policy of its pods" "$r"

r="$work/elsewhere"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/db db "$(policy default-deny '{}' '[Ingress, Egress]')"
app "$r" tenant/web web "$(policy default-deny '{}' '[Ingress, Egress]' other)"
check "a policy of another namespace does not count" 0 "$OK" "$r"
r="$work/elsewhere-only"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" tenant/web web "$(policy default-deny '{}' '[Ingress, Egress]')"
check "a policy only in another namespace fails" 1 "$ONLY" "$r"

r="$work/named"; app "$r" core/storage-claims "" "$CLAIM"; app "$r" core/other other "$(policy default-deny '{}' '[Ingress, Egress]' db)"
check "a policy that names the claim's namespace counts" 0 "$OK" "$r"

r="$work/none"; app "$r" core/db db "$(policy default-deny '{}' '[Ingress, Egress]')"
check "an instance without claims passes" 0 "no claim egress policy" "$r"

exit "$fail"
