#!/usr/bin/env bash
# Check a published container image the way an adopter's cluster would meet it.
#
#   verify-image.sh <registry/path/name@sha256:digest> <version>
#
# Everything runs anonymously, with no registry credentials. It checks that the
# image:
#   - is a multi-platform index with linux/amd64 and linux/arm64, and
#   - is signed by the shared attest-docker workflow and carries a build
#     provenance naming this repository and this digest.
# The first is what the nodes need (the homelab is arm64, CI is amd64); the
# second is what Kyverno's verify-images policy checks before it admits a pod.
# Needs oras, cosign and jq; REPO (default $GITHUB_REPOSITORY) is the repository
# that must have built it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/verify-oci.sh
source "$ROOT/scripts/lib/verify-oci.sh"

SIGNER='^https://github\.com/this-is-tobi/github-workflows/\.github/workflows/attest-docker\.yml@refs/tags/v[0-9]+(\.[0-9]+)*$'

fail() { echo "FAIL $*" >&2; exit 1; }
ok() { echo "ok   $*"; }

[ "$#" = 2 ] || fail "usage: verify-image.sh <registry/path/name@sha256:digest> <version>"
verify_args "$1" "$2"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
verify_anonymous "$work"

manifest=$(verify_manifest "$ref" "$work")
for arch in amd64 arm64; do
  jq -e --arg a "$arch" '[(.manifests // [])[] | select(.platform.os == "linux" and .platform.architecture == $a)] | length > 0' <<<"$manifest" >/dev/null \
    || fail "the image has no linux/$arch manifest: a node of that architecture could not run it"
done
ok "an index with linux/amd64 and linux/arm64"

verify_signed "$ref" "$SIGNER" attest-docker "$repo" "$digest" "$work"
