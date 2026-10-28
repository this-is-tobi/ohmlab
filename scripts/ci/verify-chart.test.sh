#!/usr/bin/env bash
# Decision table for scripts/ci/verify-chart.sh against stubbed oras, cosign
# and helm, with a hand-made chart metadata in place of a published chart.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/verify-chart.sh"
ROOT="$(cd "$(dirname "$SCRIPT")/../.." && pwd)"
fail=0
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin" "$dir/credentials"
echo '{"auths": {}}' >"$dir/credentials/config.json"

DIGEST=$(printf 'a%.0s' $(seq 64))
LAYER_DIGEST="sha256:$(printf 'c%.0s' $(seq 64))"
REF="ghcr.io/this-is-tobi/charts/ohmlab@sha256:$DIGEST"
CONFIG=application/vnd.cncf.helm.config.v1+json
LAYER=application/vnd.cncf.helm.chart.content.v1.tar+gzip

# The registry: the manifest, the chart layer as a blob, and what the caller's
# credentials look like.
cat >"$dir/bin/oras" <<'EOF'
#!/usr/bin/env bash
if [ -n "$(ls -A "$DOCKER_CONFIG" 2>/dev/null)" ]; then echo "oras: credentials present" >&2; exit 1; fi
case "$1 $2" in
  "manifest fetch")
    [ "${ORAS_FAIL:-}" = fetch ] && { echo "unauthorized: authentication required" >&2; exit 1; }
    cat "$FIXTURES/manifest.json" ;;
  "blob fetch")
    [ "${ORAS_FAIL:-}" = blob ] && { echo "blob unknown" >&2; exit 1; }
    echo "$3" >"$FIXTURES/blob.ref"
    while [ "$#" -gt 0 ]; do [ "$1" = --output ] && out=$2; shift; done
    echo chart >"$out" ;;
esac
EOF

# cosign: the signature and the provenance of the digest it is given.
cat >"$dir/bin/cosign" <<'EOF'
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
EOF

# helm: `show chart` answers with metadata in helm's own key order (the
# dependencies, with indented name:/version: lines, come first); `template`
# records what it renders and with which repository configuration.
cat >"$dir/bin/helm" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  show)
    [ "${HELM_FAIL:-}" = show ] && { echo "Error: not a chart" >&2; exit 1; }
    printf 'apiVersion: v2\ndependencies:\n- condition: argo-cd.enabled\n  name: argo-cd\n  repository: https://argoproj.github.io/argo-helm\n  version: 10.9.6\nname: %s\nversion: %s\n' \
      "${CHART_NAME:-ohmlab}" "${CHART_VERSION:-0.2.0}" ;;
  template)
    echo "template $3 $5 repos=$(wc -c <"$HELM_REPOSITORY_CONFIG" | tr -d ' ')" >>"$FIXTURES/helm.log"
    [ "${HELM_FAIL:-}" = template ] && { echo "Error: render failed" >&2; exit 1; }
    echo '---' ;;
esac
EOF
chmod +x "$dir/bin/"*

# manifest <config type> <layers json>
manifest() {
  jq -n --arg c "$1" --argjson l "$2" '{schemaVersion: 2, config: {mediaType: $c}, layers: $l}' >"$dir/manifest.json"
}
one_layer="[{\"mediaType\": \"$LAYER\", \"digest\": \"$LAYER_DIGEST\", \"size\": 1}]"

reset() {
  manifest "$CONFIG" "$one_layer"
  : >"$dir/helm.log"
  rm -f "$dir/blob.ref" "$dir/cosign.args"
  unset ORAS_FAIL COSIGN_FAIL HELM_FAIL STATEMENT_REPO STATEMENT_DIGEST CHART_NAME CHART_VERSION
}

run() { # run <ref> <version> -> $out, $rc
  out=$(PATH="$dir/bin:$PATH" FIXTURES="$dir" FIXTURE_DIGEST="$DIGEST" REPO=this-is-tobi/homelab \
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
expect "a signed, attested chart that renders passes" 0 "renders with the example instance without a Helm repository"

reset; run "ghcr.io/this-is-tobi/charts/ohmlab:0.2.0" 0.2.0
expect "a tag instead of a digest is refused" 1 "pinned by digest"

reset; run "$REF" v0.2.0
expect "a version that is not X.Y.Z is refused" 1 "X.Y.Z"

reset; ORAS_FAIL=fetch run "$REF" 0.2.0
expect "a chart that cannot be pulled anonymously is refused" 1 "make it public"

reset; manifest application/vnd.oci.image.config.v1+json "$one_layer"; run "$REF" 0.2.0
expect "another config type is refused" 1 "must be a Helm chart"

reset; manifest "$CONFIG" "[{\"mediaType\": \"$LAYER\", \"digest\": \"$LAYER_DIGEST\"},{\"mediaType\": \"$LAYER\", \"digest\": \"$LAYER_DIGEST\"}]"; run "$REF" 0.2.0
expect "two chart layers are refused" 1 "must be a Helm chart"

reset; manifest "$CONFIG" "[{\"mediaType\": \"application/vnd.oci.image.layer.v1.tar+gzip\", \"digest\": \"$LAYER_DIGEST\"}]"; run "$REF" 0.2.0
expect "a layer that is not a chart layer is refused" 1 "must be a Helm chart"

reset; COSIGN_FAIL=signature run "$REF" 0.2.0
expect "an unsigned chart is refused" 1 "signature does not verify"

reset; run "$REF" 0.2.0
if grep -q -- 'attest-helm\\.yml@refs/tags/' "$dir/cosign.args" 2>/dev/null; then
  echo "ok   the signature must come from the attest-helm workflow"
else
  echo "FAIL the signature must come from the attest-helm workflow: $(cat "$dir/cosign.args" 2>/dev/null)"; fail=1
fi

reset; COSIGN_FAIL=provenance run "$REF" 0.2.0
expect "a chart without provenance is refused" 1 "provenance does not verify"

reset; STATEMENT_REPO=https://github.com/someone/else run "$REF" 0.2.0
expect "a provenance naming another repository is refused" 1 "must name this-is-tobi/homelab"

reset; STATEMENT_DIGEST=$(printf 'b%.0s' $(seq 64)) run "$REF" 0.2.0
expect "a provenance for another digest is refused" 1 "must name this-is-tobi/homelab"

reset; manifest "$CONFIG" "[{\"mediaType\": \"$LAYER\", \"size\": 1}]"; run "$REF" 0.2.0
expect "a chart layer without a digest is refused" 1 "has no digest"

reset; run "$REF" 0.2.0
if [ "$(cat "$dir/blob.ref" 2>/dev/null)" = "ghcr.io/this-is-tobi/charts/ohmlab@$LAYER_DIGEST" ]; then
  echo "ok   the chart layer is fetched by the digest the manifest names"
else
  echo "FAIL the chart layer is fetched by the digest the manifest names: $(cat "$dir/blob.ref" 2>/dev/null)"; fail=1
fi

reset; ORAS_FAIL=blob run "$REF" 0.2.0
expect "a layer that cannot be fetched is refused" 1 "cannot fetch the chart layer"

reset; HELM_FAIL=show run "$REF" 0.2.0
expect "a layer that is not a chart is refused" 1 "is not a chart"

reset; CHART_NAME=other run "$REF" 0.2.0
expect "a chart with another name is refused" 1 "must be named ohmlab"

reset; CHART_VERSION=0.1.9 run "$REF" 0.2.0
expect "a chart with another version is refused (not a dependency's)" 1 "must be version 0.2.0"

reset; CHART_NAME=nochart run "ghcr.io/this-is-tobi/charts/nochart@sha256:$DIGEST" 0.2.0
expect "a chart the example instance has no values for is refused" 1 "no example values for nochart"

reset; HELM_FAIL=template run "$REF" 0.2.0
expect "a chart that does not render is refused" 1 "does not render"

reset; run "$REF" 0.2.0
if grep -q "^template .*/ohmlab-0.2.0.tgz $ROOT/argo-cd/instances/_example/values/core/ohmlab.yaml repos=0$" "$dir/helm.log"; then
  echo "ok   the fetched chart renders with the example values and no Helm repository"
else
  echo "FAIL the fetched chart renders with the example values and no Helm repository:"; sed 's/^/     /' "$dir/helm.log"; fail=1
fi

exit "$fail"
