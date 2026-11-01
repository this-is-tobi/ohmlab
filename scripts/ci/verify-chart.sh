#!/usr/bin/env bash
# Check a published Helm chart the way an adopter would use it.
#
#   verify-chart.sh <registry/path/name@sha256:digest> <version>
#
# Everything runs anonymously, with no registry credentials. It checks, in
# order, that the artifact:
#   - is a Helm chart (Helm config, one chart layer),
#   - is signed by the shared attest-helm workflow and carries a build
#     provenance naming this repository and this digest,
#   - is the chart <name> (the last path segment) at <version>, and
#   - renders with the example instance values without reaching a Helm
#     repository (its dependencies are packaged inside).
# Needs oras, cosign, helm and jq; REPO (default $GITHUB_REPOSITORY) is the
# repository that must have built it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
# shellcheck source=scripts/lib/verify-oci.sh
source "$ROOT/scripts/lib/verify-oci.sh"

SIGNER='^https://github\.com/this-is-tobi/github-workflows/\.github/workflows/attest-helm\.yml@refs/tags/v[0-9]+(\.[0-9]+)*$'
CONFIG_TYPE=application/vnd.cncf.helm.config.v1+json
LAYER_TYPE=application/vnd.cncf.helm.chart.content.v1.tar+gzip

fail() { echo "FAIL $*" >&2; exit 1; }
ok() { echo "ok   $*"; }

[ "$#" = 2 ] || fail "usage: verify-chart.sh <registry/path/name@sha256:digest> <version>"
verify_args "$1" "$2"
name=${ref%@*}
name=${name##*/}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
verify_anonymous "$work"

manifest=$(verify_manifest "$ref" "$work")
jq -e --arg c "$CONFIG_TYPE" --arg l "$LAYER_TYPE" \
  '.config.mediaType == $c and (.layers | length == 1 and .[0].mediaType == $l)' <<<"$manifest" >/dev/null \
  || fail "the artifact must be a Helm chart: config $CONFIG_TYPE and exactly one $LAYER_TYPE layer"
ok "a Helm chart with one chart layer"

verify_signed "$ref" "$SIGNER" attest-helm "$repo" "$digest" "$work"

# A chart layer has no title annotation, so `oras pull` would save nothing:
# fetch the layer as a blob, by the digest of the manifest checked above.
layer_digest=$(jq -r '.layers[0].digest' <<<"$manifest")
[[ "$layer_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "the chart layer has no digest"
package="$work/$name-$version.tgz"
oras blob fetch "${ref%@*}@$layer_digest" --output "$package" >/dev/null 2>"$work/err" \
  || fail "cannot fetch the chart layer: $(head -c 300 "$work/err")"
meta=$(helm show chart "$package" 2>"$work/err") || fail "the layer is not a chart: $(head -c 300 "$work/err")"
# Top-level keys only: `dependencies:` comes first and holds indented name: and
# version: lines of its own.
[ "$(awk '/^name:/ { print $2; exit }' <<<"$meta" | tr -d "\"'")" = "$name" ] \
  || fail "the chart must be named $name"
[ "$(awk '/^version:/ { print $2; exit }' <<<"$meta" | tr -d "\"'")" = "$version" ] \
  || fail "the chart must be version $version"
ok "the chart is $name $version"

# The packaged dependencies must be enough: an empty Helm configuration has no
# repository to fall back to.
: >"$work/repositories.yaml"
export HELM_REPOSITORY_CONFIG="$work/repositories.yaml" HELM_REPOSITORY_CACHE="$work/helm-cache"
values="$ROOT/argo-cd/instances/_example/values/core/$name.yaml"
[ -f "$values" ] || fail "no example values for $name ($values)"
app_args "$values" 2>"$work/err" || fail "the example's manager does not render: $(head -c 300 "$work/err")"
helm template example "$package" "${args[@]}" >/dev/null 2>"$work/err" \
  || fail "the chart does not render with the example instance: $(head -c 300 "$work/err")"
ok "renders with the example instance without a Helm repository"
