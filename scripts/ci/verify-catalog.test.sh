#!/usr/bin/env bash
# Decision table for scripts/ci/verify-catalog.sh against stubbed oras, cosign
# and helm, with a hand-made bundle in place of a published one.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/verify-catalog.sh"
fail=0
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin" "$dir/credentials"
echo '{"auths": {}}' >"$dir/credentials/config.json"

DIGEST=$(printf 'a%.0s' $(seq 64))
REF="ghcr.io/this-is-tobi/ohmlab/catalog@sha256:$DIGEST"
LAYER=application/vnd.oci.image.layer.v1.tar+gzip
TYPE=application/vnd.ohmlab.catalog.v1

# The registry, as the stub oras sees it: a manifest, one layer, and what the
# credentials of the caller look like.
cat >"$dir/bin/oras" <<'EOF'
#!/usr/bin/env bash
if [ -n "$(ls -A "$DOCKER_CONFIG" 2>/dev/null)" ]; then echo "oras: credentials present" >&2; exit 1; fi
case "$1 $2" in
  "manifest fetch")
    [ "${ORAS_FAIL:-}" = fetch ] && { echo "unauthorized: authentication required" >&2; exit 1; }
    cat "$FIXTURES/manifest.json" ;;
  "pull "*)
    [ "${ORAS_FAIL:-}" = pull ] && { echo "pull failed" >&2; exit 1; }
    while [ "$#" -gt 0 ]; do [ "$1" = --output ] && out=$2; shift; done
    mkdir -p "$out/dist"
    cp "$FIXTURES/layer.tar.gz" "$out/dist/catalog.tar.gz"
    if [ "${EXTRA_FILE:-}" = 1 ]; then echo x >"$out/extra"; fi ;;
esac
EOF

# cosign: the signature and the provenance of the digest it is given.
cat >"$dir/bin/cosign" <<'EOF'
#!/usr/bin/env bash
if [ -n "$(ls -A "$DOCKER_CONFIG" 2>/dev/null)" ]; then echo "cosign: credentials present" >&2; exit 1; fi
case "$1" in
  verify)
    [ "${COSIGN_FAIL:-}" = signature ] && { echo "no matching signatures" >&2; exit 1; }
    echo '[]' ;;
  verify-attestation)
    [ "${COSIGN_FAIL:-}" = provenance ] && { echo "no matching attestations" >&2; exit 1; }
    [ "${COSIGN_FAIL:-}" = none-found ] && exit 0
    printf '{"payload":"%s"}\n' "$(jq -nc --arg d "${STATEMENT_DIGEST:-$FIXTURE_DIGEST}" --arg r "${STATEMENT_REPO:-https://github.com/this-is-tobi/ohmlab}" \
      '{subject:[{digest:{sha256:$d}}],predicate:{buildDefinition:{externalParameters:{workflow:{repository:$r}}}}}' | base64 | tr -d '\n')" ;;
esac
EOF

# helm: renders anything except the chart named in HELM_FAIL, and records the
# charts and the repository configuration it was given.
cat >"$dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  template)
    echo "template $3 repos=$(wc -c <"$HELM_REPOSITORY_CONFIG" | tr -d ' ')" >>"$FIXTURES/helm.log"
    case "$3" in *"/${HELM_FAIL:-none}") echo "Error: render failed" >&2; exit 1 ;; esac
    echo '---' ;;
  dependency)
    echo "dependency $*" >>"$FIXTURES/helm.log"
    echo "Error: no repository definition" >&2; exit 1 ;;
esac
EOF
printf '#!/usr/bin/env bash\ncat >/dev/null\n' >"$dir/bin/kubeconform"
chmod +x "$dir/bin/"*

# bundle <version-in-the-archive> [vendor|novendor]
bundle() {
  local root="$dir/tree"
  rm -rf "$root"
  mkdir -p "$root/argo-cd/apps/foo" "$root/argo-cd/apps/bar/charts" "$root/utils/helm" "$root/argo-cd/instances/_example"
  printf '%s\n' "$1" >"$root/argo-cd/apps/version.txt"
  printf 'apiVersion: v2\nname: foo\nversion: 0.1.0\n' >"$root/argo-cd/apps/foo/Chart.yaml"
  printf 'apiVersion: v2\nname: bar\nversion: 0.1.0\n' >"$root/argo-cd/apps/bar/Chart.yaml"
  printf 'dependencies: []\n' >"$root/argo-cd/apps/bar/Chart.lock"
  printf 'apiVersion: v2\nname: ohmlab\nversion: 0.1.0\n' >"$root/utils/helm/Chart.yaml"
  [ "${2:-vendor}" = vendor ] || rmdir "$root/argo-cd/apps/bar/charts"
  tar -czf "$dir/layer.tar.gz" -C "$root" .
}

# manifest <layers-json> <artifactType> <version>
manifest() {
  jq -n --argjson l "$1" --arg t "$2" --arg v "$3" \
    '{schemaVersion: 2, artifactType: $t, layers: $l, annotations: {"org.opencontainers.image.version": $v}}' >"$dir/manifest.json"
}

one_layer="[{\"mediaType\": \"$LAYER\", \"digest\": \"sha256:$DIGEST\", \"size\": 1}]"

reset() {
  bundle 0.2.0
  manifest "$one_layer" "$TYPE" 0.2.0
  : >"$dir/helm.log"
  unset ORAS_FAIL COSIGN_FAIL HELM_FAIL STATEMENT_REPO STATEMENT_DIGEST EXTRA_FILE
}

run() { # run <ref> <version> -> $out, $rc
  out=$(PATH="$dir/bin:$PATH" FIXTURES="$dir" FIXTURE_DIGEST="$DIGEST" REPO=this-is-tobi/ohmlab \
    DOCKER_CONFIG="$dir/credentials" "$SCRIPT" "$1" "$2" 2>&1)
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
expect "a signed, attested, well-formed bundle passes" 0 "every chart renders from the bundle alone"

reset; run "ghcr.io/this-is-tobi/ohmlab/catalog:0.2.0" 0.2.0
expect "a tag instead of a digest is refused" 1 "pinned by digest"

reset; run "ghcr.io/this-is-tobi/ohmlab/catalog" 0.2.0
expect "a reference without a digest is refused" 1 "pinned by digest"

reset; run "$REF" v0.2.0
expect "a version that is not X.Y.Z is refused" 1 "X.Y.Z"

reset; ORAS_FAIL=fetch run "$REF" 0.2.0
expect "a package that cannot be pulled anonymously is refused" 1 "make it public"

reset; manifest "[{\"mediaType\": \"$LAYER\"},{\"mediaType\": \"$LAYER\"}]" "$TYPE" 0.2.0; run "$REF" 0.2.0
expect "two layers are refused" 1 "exactly one layer"

reset; manifest '[{"mediaType": "application/vnd.oci.image.layer.v1.tar"}]' "$TYPE" 0.2.0; run "$REF" 0.2.0
expect "a layer that is not tar+gzip is refused" 1 "exactly one layer"

reset; manifest "$one_layer" application/vnd.other.v1 0.2.0; run "$REF" 0.2.0
expect "another artifact type is refused" 1 "artifact type"

reset; manifest "$one_layer" "$TYPE" 0.1.9; run "$REF" 0.2.0
expect "a manifest annotated with another version is refused" 1 "annotated with version 0.2.0"

reset; COSIGN_FAIL=signature run "$REF" 0.2.0
expect "an unsigned artifact is refused" 1 "signature does not verify"

reset; COSIGN_FAIL=provenance run "$REF" 0.2.0
expect "an artifact without provenance is refused" 1 "provenance does not verify"

reset; COSIGN_FAIL=none-found run "$REF" 0.2.0
expect "a verification that finds no provenance is refused" 1 "must name this-is-tobi/ohmlab"

reset; STATEMENT_REPO=https://github.com/someone/else run "$REF" 0.2.0
expect "a provenance naming another repository is refused" 1 "must name this-is-tobi/ohmlab"

reset; STATEMENT_DIGEST=$(printf 'b%.0s' $(seq 64)) run "$REF" 0.2.0
expect "a provenance for another digest is refused" 1 "must name this-is-tobi/ohmlab"

reset; bundle 0.1.0; run "$REF" 0.2.0
expect "a bundle that holds another version is refused" 1 "version.txt must hold 0.2.0"

reset; echo "not an archive" >"$dir/layer.tar.gz"; run "$REF" 0.2.0
expect "a layer that is not an archive is refused" 1 "not a tar.gz archive"

reset; EXTRA_FILE=1 run "$REF" 0.2.0
expect "a pull that yields more than the layer is refused" 1 "exactly one file"

reset; HELM_FAIL=foo run "$REF" 0.2.0
expect "a chart that does not render is refused" 1 "does not render"

reset; bundle 0.2.0 novendor; run "$REF" 0.2.0
expect "a chart whose dependencies are not vendored is refused" 1 "does not render"

reset; run "$REF" 0.2.0
if grep -q '^template ' "$dir/helm.log" && ! grep '^template ' "$dir/helm.log" | grep -q "$(cd "$(dirname "$SCRIPT")/../.." && pwd)"; then
  echo "ok   the charts rendered are the ones of the bundle, not of the checkout"
else
  echo "FAIL the charts rendered are the ones of the bundle, not of the checkout:"; sed 's/^/     /' "$dir/helm.log"; fail=1
fi
if grep '^template ' "$dir/helm.log" | grep -vq 'repos=0'; then
  echo "FAIL rendering must run with an empty Helm repository configuration"; fail=1
else
  echo "ok   rendering runs with an empty Helm repository configuration"
fi

exit "$fail"
