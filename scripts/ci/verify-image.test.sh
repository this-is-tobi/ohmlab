#!/usr/bin/env bash
# Decision table for scripts/ci/verify-image.sh against stubbed oras and cosign.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/verify-image.sh"
fail=0
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin" "$dir/credentials"
echo '{"auths": {}}' >"$dir/credentials/config.json"

DIGEST=$(printf 'a%.0s' $(seq 64))
REF="ghcr.io/this-is-tobi/ohmlab/controller@sha256:$DIGEST"

cat >"$dir/bin/oras" <<'STUB'
#!/usr/bin/env bash
if [ -n "$(ls -A "$DOCKER_CONFIG" 2>/dev/null)" ]; then echo "oras: credentials present" >&2; exit 1; fi
[ "$1 $2" = "manifest fetch" ] || exit 1
[ "${ORAS_FAIL:-}" = fetch ] && { echo "unauthorized: authentication required" >&2; exit 1; }
cat "$FIXTURES/manifest.json"
STUB
cat >"$dir/bin/cosign" <<'STUB'
#!/usr/bin/env bash
if [ -n "$(ls -A "$DOCKER_CONFIG" 2>/dev/null)" ]; then echo "cosign: credentials present" >&2; exit 1; fi
case "$1" in
  verify)
    [ "${COSIGN_FAIL:-}" = signature ] && { echo "no matching signatures" >&2; exit 1; }
    echo "$@" >"$FIXTURES/cosign.args"
    echo '[]' ;;
  verify-attestation)
    [ "${COSIGN_FAIL:-}" = provenance ] && { echo "no matching attestations" >&2; exit 1; }
    printf '{"payload":"%s"}\n' "$(jq -nc --arg d "${STATEMENT_DIGEST:-$FIXTURE_DIGEST}" --arg r "${STATEMENT_REPO:-https://github.com/this-is-tobi/homelab}" \
      '{subject:[{digest:{sha256:$d}}],predicate:{buildDefinition:{externalParameters:{workflow:{repository:$r}}}}}' | base64 | tr -d '\n')" ;;
esac
STUB
chmod +x "$dir/bin/"*

# index <platforms...>, each os/arch
index() {
  local m=() p
  for p in "$@"; do m+=("{\"platform\":{\"os\":\"${p%/*}\",\"architecture\":\"${p#*/}\"}}"); done
  printf '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[%s]}\n' "$(IFS=,; echo "${m[*]}")" >"$dir/manifest.json"
}
reset() {
  index linux/amd64 linux/arm64 unknown/unknown
  rm -f "$dir/cosign.args"
  unset ORAS_FAIL COSIGN_FAIL STATEMENT_REPO STATEMENT_DIGEST
}
run() { # run <ref> <version> -> $out, $rc
  out=$(PATH="$dir/bin:$PATH" FIXTURES="$dir" FIXTURE_DIGEST="$DIGEST" REPO=this-is-tobi/homelab DOCKER_CONFIG="$dir/credentials" "$SCRIPT" "$1" "$2" 2>&1)
  rc=$?
}
expect() { # expect <label> <exit> [pattern in the output]
  if [ "$rc" != "$2" ]; then
    echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  elif [ -n "${3:-}" ] && ! grep -qF -- "$3" <<<"$out"; then
    echo "FAIL $1: no '$3' in:"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  else
    echo "ok   $1"
  fi
}

reset; run "$REF" 0.2.0
expect "a signed, attested, multi-platform image passes" 0 "build provenance names this-is-tobi/homelab"

reset; run "ghcr.io/this-is-tobi/ohmlab/controller:0.2.0" 0.2.0
expect "a tag instead of a digest is refused" 1 "pinned by digest"

reset; run "$REF" v0.2.0
expect "a version that is not X.Y.Z is refused" 1 "X.Y.Z"

reset; ORAS_FAIL=fetch run "$REF" 0.2.0
expect "an image that cannot be pulled anonymously is refused" 1 "make it public"

reset; index linux/amd64; run "$REF" 0.2.0
expect "an image without an arm64 manifest is refused" 1 "no linux/arm64 manifest"

reset; index linux/arm64; run "$REF" 0.2.0
expect "an image without an amd64 manifest is refused" 1 "no linux/amd64 manifest"

reset; COSIGN_FAIL=signature run "$REF" 0.2.0
expect "an unsigned image is refused" 1 "signature does not verify"

reset; run "$REF" 0.2.0
if grep -q -- 'attest-docker\\.yml@refs/tags/' "$dir/cosign.args" 2>/dev/null; then
  echo "ok   the signature must come from the attest-docker workflow"
else
  echo "FAIL the signature must come from the attest-docker workflow: $(cat "$dir/cosign.args" 2>/dev/null)"; fail=1
fi

reset; COSIGN_FAIL=provenance run "$REF" 0.2.0
expect "an image without provenance is refused" 1 "provenance does not verify"

reset; STATEMENT_REPO=https://github.com/someone/else run "$REF" 0.2.0
expect "a provenance naming another repository is refused" 1 "must name this-is-tobi/homelab"

reset; STATEMENT_DIGEST=$(printf 'b%.0s' $(seq 64)) run "$REF" 0.2.0
expect "a provenance for another digest is refused" 1 "must name this-is-tobi/homelab"

exit "$fail"
