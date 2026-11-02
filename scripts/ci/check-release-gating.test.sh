#!/usr/bin/env bash
# Decision table for scripts/ci/check-release-gating.sh on hand-written
# release configs and workflows.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-release-gating.sh"
fail=0
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

GATE_CLI="\${{ contains(fromJSON(needs.release.outputs.paths-released), 'cli') }}"
GATE_CATALOG="\${{ contains(fromJSON(needs.release.outputs.paths-released), 'argo-cd/apps') }}"
VERSION_CLI="\${{ fromJSON(needs.release.outputs.release-outputs)['cli--version'] }}"
VERSION_CATALOG="\${{ fromJSON(needs.release.outputs.release-outputs)['argo-cd/apps--version'] }}"

# config <path>... -> the packages of a config, and a manifest listing the same
# ones; argo-cd/apps is released as `simple`, with the version file that type
# updates
config() {
  local p out='' versions='' sep=''
  rm -rf "$dir/argo-cd"
  for p in "$@"; do
    if [ "$p" = argo-cd/apps ]; then
      out="$out$sep\"$p\": {\"release-type\": \"simple\"}"
      mkdir -p "$dir/$p"
      echo 0.0.0 >"$dir/$p/version.txt"
    else
      out="$out$sep\"$p\": {\"release-type\": \"go\"}"
    fi
    versions="$versions$sep\"$p\": \"0.0.0\""
    sep=','
  done
  printf '{"packages": {%s}}\n' "$out" >"$dir/config.json"
  printf '{%s}\n' "$versions" >"$dir/.release-please-manifest.json"
}

# workflow <job yaml>... -> a workflow with the release job plus the given jobs
workflow() {
  {
    printf 'jobs:\n  release:\n    uses: x/y/.github/workflows/release-app.yml@v0\n'
    printf '%s\n' "$@"
  } >"$dir/cd.yml"
}

check() { # label expected-exit
  local out rc
  out=$("$SCRIPT" "$dir/config.json" "$dir/cd.yml" 2>&1); rc=$?
  if [ "$rc" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: exit $rc, expected $2"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1; fi
}

has() { # label pattern: the last run named the finding
  local out
  out=$("$SCRIPT" "$dir/config.json" "$dir/cd.yml" 2>&1)
  if printf '%s' "$out" | grep -qF -- "$2"; then echo "ok   $1"; else echo "FAIL $1: no '$2' in:"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1; fi
}

cli_image="  image:
    needs: release
    if: $GATE_CLI
    uses: x/y/.github/workflows/build-docker.yml@v0
    with:
      IMAGE_TAG: $VERSION_CLI"
cli_attest="  attest-image:
    needs: image
    uses: x/y/.github/workflows/attest-docker.yml@v0"
cli_binaries="  binaries:
    needs: release
    if: $GATE_CLI
    uses: x/y/.github/workflows/release-go.yml@v0"
catalog_build="  catalog:
    needs: release
    if: $GATE_CATALOG
    uses: x/y/.github/workflows/build-oci-artifact.yml@v0
    with:
      ARTIFACT_TAG: $VERSION_CATALOG"
catalog_attest="  attest-catalog:
    needs: catalog
    uses: x/y/.github/workflows/attest-docker.yml@v0
    with:
      TAG: \${{ fromJSON(needs.release.outputs.release-outputs)['argo-cd/apps--tag_name'] }}"

config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
check "each package runs only the jobs gated on its own release" 0

workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest" \
  "  attest-binaries:
    needs: [release, binaries]
    uses: x/y/.github/workflows/attest-go.yml@v0
    with:
      TAG: \${{ fromJSON(needs.release.outputs.release-outputs)['cli--tag_name'] }}"
check "a job reading outputs inherits the gate of a job it needs, as a string or in a list" 0

workflow "$cli_image" "$cli_attest" "$cli_binaries" "  catalog:
    needs: release
    if: \${{ needs.release.outputs.releases-created == 'true' && contains(fromJSON(needs.release.outputs.paths-released), 'argo-cd/apps') }}
    uses: x/y/.github/workflows/build-oci-artifact.yml@v0
    with:
      ARTIFACT_TAG: $VERSION_CATALOG" "$catalog_attest"
check "any release and the package's own gate can be combined" 0

workflow "  image:
    needs: release
    if: \${{ needs.release.outputs.releases-created == 'true' }}
    uses: x/y/.github/workflows/build-docker.yml@v0
    with:
      IMAGE_TAG: $VERSION_CLI" "$cli_binaries" "$catalog_build" "$catalog_attest"
check "a job reading a package's outputs without that package's gate is refused" 1
has "the finding names the job and the package" "image reads the release outputs of 'cli'"

workflow "$cli_image" "  binaries:
    needs: release
    if: \${{ needs.release.outputs.releases-created == 'true' }}
    uses: x/y/.github/workflows/release-go.yml@v0" "$catalog_build" "$catalog_attest"
check "a job gated on any release is refused even when it reads no output" 1
has "the finding names the job" "binaries is gated on any release"

workflow "$cli_image" "$cli_binaries" "  catalog:
    needs: release
    if: $GATE_CATALOG
    uses: x/y/.github/workflows/build-oci-artifact.yml@v0
    with:
      ARTIFACT_TAG: $VERSION_CLI"
check "a job gated on one package but reading another's outputs is refused" 1

workflow "$cli_image" "$cli_binaries" "$catalog_build" \
  "  attest-catalog:
    needs: release
    uses: x/y/.github/workflows/attest-docker.yml@v0
    with:
      DIGEST: $VERSION_CATALOG"
has "an attest job that skips the build job and is not gated itself is refused" "attest-catalog reads the release outputs of 'argo-cd/apps'"

workflow "$cli_image" "$cli_binaries" "  catalog:
    needs: release
    if: \${{ contains(fromJSON(needs.release.outputs.paths-released), 'argo-cd/app') }}
    uses: x/y/.github/workflows/build-oci-artifact.yml@v0
    with:
      ARTIFACT_TAG: $VERSION_CATALOG"
check "a gate naming something that is not a package is refused" 1
has "the finding names the unknown path" "'argo-cd/app', which is not a package"

workflow "$cli_image" "$cli_binaries"
check "a package whose release triggers no job is refused" 1
has "the finding names the package" "gated on the release of 'argo-cd/apps'"

config cli
workflow "$cli_image" "$cli_binaries"
check "a single package is enough" 0

# A condition naming two packages is an OR: the job also runs when only the
# other one was released, and then the outputs it reads are empty.
EITHER="\${{ contains(fromJSON(needs.release.outputs.paths-released), 'argo-cd/apps') || contains(fromJSON(needs.release.outputs.paths-released), 'cli') }}"
config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest" "  keep-latest:
    needs: release
    if: $EITHER
    runs-on: ubuntu-latest"
check "a job that reads no output may run for either of two packages" 0

workflow "$cli_image" "$cli_attest" "$cli_binaries" "  catalog:
    needs: release
    if: $EITHER
    uses: x/y/.github/workflows/build-oci-artifact.yml@v0
    with:
      ARTIFACT_TAG: $VERSION_CATALOG" "$catalog_attest"
check "a job reading a package's outputs but also running for another package is refused" 1
has "the finding says the job does not run only for that package" "catalog reads the release outputs of 'argo-cd/apps' but does not run only when it is released"
has "a job inheriting that OR through needs is refused too" "attest-catalog reads the release outputs of 'argo-cd/apps' but does not run only when it is released"

workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest" "  both:
    needs: [image, catalog]
    uses: x/y/.github/workflows/verify.yml@v0
    with:
      A: $VERSION_CLI
      B: $VERSION_CATALOG"
check "a job needing two gated jobs runs only when both were released, so it may read both" 0

workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest" "  notify:
    needs: image
    if: \${{ needs.release.outputs.releases-created == 'true' }}
    runs-on: ubuntu-latest"
check "testing releases-created is fine for a job that needs a gated job" 0

workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest" "  flag:
    needs: release
    uses: x/y/.github/workflows/notify.yml@v0
    with:
      IS_CLI: \${{ contains(fromJSON(needs.release.outputs.paths-released), 'cli') }}
      VERSION: $VERSION_CLI"
check "a package condition passed as an input does not gate the job" 1
has "the finding names that job" "flag reads the release outputs of 'cli'"

# release-please only updates a version.txt that exists: a package released as
# `simple` without one gets a release PR with a changelog and no version file.
config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
rm "$dir/argo-cd/apps/version.txt"
check "a simple package without its version file is refused" 1
has "the finding names the file" "argo-cd/apps/version.txt"

config cli argo-cd/apps
jq '.packages["argo-cd/apps"]["version-file"] = "VERSION"' "$dir/config.json" >"$dir/config.next" && mv "$dir/config.next" "$dir/config.json"
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
check "a version file named by version-file is the one that must exist" 1
has "the finding names the configured file" "argo-cd/apps/VERSION"
echo 0.0.0 >"$dir/argo-cd/apps/VERSION"
check "the configured version file is enough" 0

# release-please counts a package without a manifest entry as never released
# and opens its first release again; an entry without a package is the other
# half of a package renamed in one of the two files only.
config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
jq 'del(.cli)' "$dir/.release-please-manifest.json" >"$dir/manifest.next" && mv "$dir/manifest.next" "$dir/.release-please-manifest.json"
check "a package without a manifest entry is refused" 1
has "the finding names the package" "'cli' is a package of"

config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
jq '.docs = "0.0.0"' "$dir/.release-please-manifest.json" >"$dir/manifest.next" && mv "$dir/manifest.next" "$dir/.release-please-manifest.json"
check "a manifest entry that is no package is refused" 1
has "the finding names the path" "'docs' has an entry in"

config cli argo-cd/apps
workflow "$cli_image" "$cli_attest" "$cli_binaries" "$catalog_build" "$catalog_attest"
rm "$dir/.release-please-manifest.json"
check "a missing manifest is refused" 1
has "the finding names the manifest" "release-please-manifest.json does not exist"

exit "$fail"
