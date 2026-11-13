#!/usr/bin/env bash
# A claim's egress policy (storage.networkPolicies) adds the store to what its
# pods may reach only where other egress policies already select those pods.
# In a namespace whose egress is open it would be their only egress policy:
# they would reach the store and nothing else, not the API server or DNS, as a
# Postgres cluster whose instances lose the API server. Per instance, each
# egress NetworkPolicy of storage-claims needs another egress policy in its
# namespace, not a claim's, whose podSelector selects at least the same pods:
# an empty one, or one whose labels and expressions the claim's selector all
# carries.
#
# Reads a render directory of render-instance.sh (RENDER_OUT): per app
# <instance>/<tier>/<app>.yaml (its objects) and .destination ("<server>
# <namespace>", the namespace of the objects that name none).
#
#   check-storage-egress.sh <render dir>
#
# Prints `ok   <instance>: ...` or `FAIL <instance>: ...`; exits with the
# number of failures, at most 255. Needs yq (mikefarah v4) and jq.
set -uo pipefail

dir="${1:?usage: check-storage-egress.sh <render dir>}"
dir="${dir%/}"
shopt -s nullglob
failures=0
checked=0
for inst in "$dir"/*/; do
  inst="${inst%/}"
  instance="${inst##*/}"
  claims=("$inst"/*/storage-claims.yaml)
  [ ${#claims[@]} -gt 0 ] || continue
  # Every NetworkPolicy the instance renders: its app, namespace, selector, and
  # whether it restricts egress (policyTypes, else the presence of rules).
  policies=$(for f in "$inst"/*/*.yaml; do
    app="${f#"$inst"/}"
    app="${app%.yaml}"
    ns=""
    [ -f "${f%.yaml}.destination" ] && read -r _ ns _ <"${f%.yaml}.destination"
    yq ea -o=json -I=0 '[.]' "$f" 2>/dev/null | jq -c --arg app "$app" --arg ns "$ns" '
      .[] | select(type == "object" and .kind == "NetworkPolicy")
      | {app: $app, ns: (.metadata.namespace // $ns), name: .metadata.name,
         selector: (.spec.podSelector // {}),
         egress: (if .spec.policyTypes then (.spec.policyTypes | index("Egress")) != null else .spec.egress != null end)}'
  done | jq -sc .)
  lines=$(jq -r --arg instance "$instance" '
    def claim: .app | endswith("/storage-claims");
    # Does this selector select every pod $s selects?
    def covers($s): ((.matchLabels // {}) | to_entries | all(. as $e | ($s.matchLabels // {})[$e.key] == $e.value))
      and ((.matchExpressions // []) | all(. as $e | any(($s.matchExpressions // [])[]; . == $e)));
    . as $all
    | .[] | select(claim and .egress) | . as $c
    | if any($all[]; (claim | not) and .egress and .ns == $c.ns and (.selector | covers($c.selector)))
      then "ok\tok   \($instance): \($c.ns)/\($c.name) adds to its pods'"'"' egress policies"
      else "fail\tFAIL \($instance): \($c.ns)/\($c.name) would be the only egress policy of its pods, which would reach the store and nothing else (not the API server or DNS): deny egress by default in \($c.ns), or turn storage.networkPolicies off" end' <<<"$policies")
  while IFS=$'\t' read -r verdict line; do
    [ -n "$line" ] || continue
    echo "$line"
    checked=$((checked + 1))
    [ "$verdict" = ok ] || failures=$((failures + 1))
  done <<<"$lines"
done
[ "$checked" -gt 0 ] || echo "ok   $dir: no claim egress policy to check"
exit $((failures > 255 ? 255 : failures))
