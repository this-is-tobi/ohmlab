#!/usr/bin/env bash
# Exercises test-prometheus-rules.sh against a throwaway chart: a rule
# file whose test matches must pass, one whose test expects an alert the
# chart does not define must fail.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/test-prometheus-rules.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

make_chart() { # $1 = apps root, $2 = expected alert name in the test file
  local chart="$1/fixture"
  mkdir -p "$chart/templates" "$chart/tests"
  cat >"$chart/Chart.yaml" <<'EOF'
apiVersion: v2
name: fixture
version: 0.1.0
EOF
  cat >"$chart/templates/rule.yaml" <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: fixture
  namespace: {{ .Release.Namespace }}
spec:
  groups:
  - name: fixture
    rules:
    - alert: FixtureUp
      expr: up{namespace="{{ .Release.Namespace }}"} == 0
      labels:
        severity: warning
      annotations:
        summary: fixture down
EOF
  cat >"$chart/tests/prometheusrules.test.yaml" <<EOF
rule_files: [rules.yaml]
evaluation_interval: 1m
tests:
- interval: 1m
  input_series:
  - series: 'up{namespace="test", job="x"}'
    values: '0x5'
  alert_rule_test:
  - eval_time: 2m
    alertname: $2
    exp_alerts:
    - exp_labels: {severity: warning, namespace: test, job: x}
      exp_annotations: {summary: fixture down}
EOF
}

# A chart whose rule renders only when a value is on: without values of its own
# the test has nothing to run on, with tests/values.yaml it does.
make_gated_chart() { # $1 = apps root, $2 = "with" to add tests/values.yaml
  local rule="$1/fixture/templates/rule.yaml"
  make_chart "$1" FixtureUp
  { echo '{{- if .Values.enabled }}'; cat "$rule"; echo '{{- end }}'; } >"$rule.gated"
  mv "$rule.gated" "$rule"
  printf 'enabled: false\n' >"$1/fixture/values.yaml"
  if [ "$2" = with ]; then printf 'enabled: true\n' >"$1/fixture/tests/values.yaml"; fi
}

check() { # $1 = name, $2 = expected exit (0 or nonzero), $3 = apps root
  local rc=0
  (cd "$work" && "$script" "$3" >/dev/null 2>&1) || rc=$?
  if { [ "$2" = 0 ] && [ "$rc" -eq 0 ]; } || { [ "$2" = nonzero ] && [ "$rc" -ne 0 ]; }; then
    echo "ok   $1"
  else
    echo "FAIL $1 (exit $rc, wanted $2)"; failures=$((failures + 1))
  fi
}

make_chart "$work/pass" FixtureUp
check "matching test passes" 0 "$work/pass"
make_chart "$work/fail" FixtureMissing
check "test for an undefined alert fails" nonzero "$work/fail"
make_gated_chart "$work/gated-without" without
check "a chart that renders nothing by default and has no values of its own fails" nonzero "$work/gated-without"
make_gated_chart "$work/gated-with" with
check "a chart's own tests/values.yaml are used" 0 "$work/gated-with"
mkdir -p "$work/empty"
check "no test files is a pass" 0 "$work/empty"

exit "$failures"
