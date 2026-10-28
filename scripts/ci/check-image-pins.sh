#!/usr/bin/env bash
# Every image of this owner pinned by digest in the charts' and instances'
# values must match a pattern of Kyverno's verify-images policy. The policy
# only looks at images that match its list: a pin under any other name runs
# without its signature or provenance being checked, and nothing reports it.
#
#   check-image-pins.sh [<root>]   (default: the repository root)
#
# Patterns are matched like the policy's own match condition: `*` is any run of
# characters, slashes included, and `?` is one character. Needs yq (mikefarah v4).
set -euo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
POLICY="$ROOT/argo-cd/apps/kyverno/values.yaml"
PIN='ghcr\.io/this-is-tobi/[A-Za-z0-9._/-]+:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}'

[ -f "$POLICY" ] || { echo "check-image-pins: no $POLICY" >&2; exit 1; }
mapfile -t patterns < <(yq '.policies.verifyImages.images[]' "$POLICY")
[ "${#patterns[@]}" -gt 0 ] || { echo "check-image-pins: policies.verifyImages.images is empty in $POLICY" >&2; exit 1; }

files=()
while IFS= read -r -d '' f; do files+=("$f"); done < <(
  find "$ROOT/argo-cd" \( -path "$ROOT/argo-cd/instances/*" -name '*.yaml' \) -print0 -o \
    \( -path "$ROOT/argo-cd/apps/*" -name 'values*.yaml' \) -print0 2>/dev/null
)

pins=0 bad=0
for f in ${files[@]+"${files[@]}"}; do
  while IFS= read -r ref; do
    pins=$((pins + 1))
    matched=0
    for p in "${patterns[@]}"; do
      # shellcheck disable=SC2053 # the pattern is a glob on purpose
      if [[ "$ref" == $p ]]; then matched=1; break; fi
    done
    if [ "$matched" = 0 ]; then
      echo "unverified pin: ${f#"$ROOT"/}: $ref"
      bad=$((bad + 1))
    fi
  done < <(grep -oE "$PIN" "$f" || true)
done

if [ "$bad" -gt 0 ]; then
  echo "check-image-pins: $bad pin(s) match no pattern of policies.verifyImages.images in argo-cd/apps/kyverno/values.yaml" >&2
  exit 1
fi
echo "ok   $pins pin(s), each matches policies.verifyImages.images"
