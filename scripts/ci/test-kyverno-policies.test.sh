#!/usr/bin/env bash
# Exercises test-kyverno-policies.sh against a throwaway chart: a policy whose
# fixtures behave must pass, one whose fixture expects the wrong result must
# fail, and a chart without fixtures is a pass.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/test-kyverno-policies.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

make_chart() { # $1 = chart dir, $2 = expected result for the hostNetwork pod
  # optional: $3 = spec lines, $4 = matchConstraints lines, $5 = expression prefix
  local chart="$1" spec="${3:-}" constraints="${4:-}" prefix="${5:-}"
  mkdir -p "$chart/templates" "$chart/tests/policies/fixture"
  cat >"$chart/Chart.yaml" <<'EOF'
apiVersion: v2
name: fixture
version: 0.1.0
EOF
  cat >"$chart/templates/policy.yaml" <<EOF
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: no-host-network
spec:
  validationActions: [Audit]
${spec}
  matchConstraints:
${constraints}
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["pods"]
  validations:
  - expression: "${prefix}!object.spec.?hostNetwork.orValue(false)"
    message: hostNetwork is not allowed
EOF
  cat >"$chart/tests/policies/fixture/resources.yaml" <<'EOF'
apiVersion: v1
kind: Pod
metadata: {name: clean, namespace: demo}
spec: {containers: [{name: a, image: busybox:1}]}
---
apiVersion: v1
kind: Pod
metadata: {name: host, namespace: demo}
spec: {hostNetwork: true, containers: [{name: a, image: busybox:1}]}
EOF
  cat >"$chart/tests/policies/fixture/kyverno-test.yaml" <<EOF
apiVersion: cli.kyverno.io/v1alpha1
kind: Test
metadata: {name: fixture}
policies: [../../rendered/policies.yaml]
resources: [resources.yaml]
results:
- {policy: no-host-network, isValidatingPolicy: true, kind: Pod, resources: [clean], result: pass}
- {policy: no-host-network, isValidatingPolicy: true, kind: Pod, resources: [host], result: $2}
EOF
}

check() { # $1 = name, $2 = expected exit (0 or nonzero), $3 = chart dir
  local rc=0
  "$script" "$3" >/dev/null 2>&1 || rc=$?
  if { [ "$2" = 0 ] && [ "$rc" -eq 0 ]; } || { [ "$2" = nonzero ] && [ "$rc" -ne 0 ]; }; then
    echo "ok   $1"
  else
    echo "FAIL $1 (exit $rc, wanted $2)"; failures=$((failures + 1))
  fi
}

make_chart "$work/pass" fail
check "matching fixtures pass" 0 "$work/pass"
make_chart "$work/fail" pass
check "a fixture expecting the wrong result fails" nonzero "$work/fail"
mkdir -p "$work/empty/templates"
printf 'apiVersion: v2\nname: empty\nversion: 0.1.0\n' >"$work/empty/Chart.yaml"
check "no fixtures is a pass" 0 "$work/empty"
# The CLI exits 0 on a directory where it finds no test file, which is also
# what it prints when it cannot read the directory.
make_chart "$work/notest" fail
mv "$work/notest/tests/policies/fixture/kyverno-test.yaml" "$work/notest/tests/policies/fixture/test.yaml"
check "a suite where no test runs fails" nonzero "$work/notest"

# The CLI cannot evaluate namespaceObject, so the tenant scoping variable is
# overridden to false before testing. A policy must not scope with a
# namespaceSelector, matchConditions or webhookConfiguration: any of them gives
# it its own Kyverno webhook, one more admission call per pod.
scope=$(cat <<'EOF'
  variables:
  - name: outOfScope
    expression: >-
      namespaceObject.metadata.?labels[?'team'].orValue('') != 'tenant'
EOF
)
make_chart "$work/tenant" fail "$scope" "" 'variables.outOfScope || '
check "a tenant-scoped policy is evaluated" 0 "$work/tenant"
make_chart "$work/tenant-wrong" pass "$scope" "" 'variables.outOfScope || '
check "a tenant-scoped policy is not vacuously passed" nonzero "$work/tenant-wrong"
make_chart "$work/selector" fail "" '    namespaceSelector: {matchLabels: {team: tenant}}'
check "a policy with its own namespaceSelector fails" nonzero "$work/selector"
make_chart "$work/webhook" fail '  webhookConfiguration: {timeoutSeconds: 5}'
check "a policy with its own webhookConfiguration fails" nonzero "$work/webhook"
make_chart "$work/notmatched" fail
sed -i.bak 's/resources: \["pods"\]/resources: ["configmaps"]/' "$work/notmatched/templates/policy.yaml" && rm "$work/notmatched/templates/policy.yaml.bak"
check "fixtures the policy does not match fail the run" nonzero "$work/notmatched"

exit "$failures"
