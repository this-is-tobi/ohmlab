#!/usr/bin/env bash
# Run the regex of renovate.json's catalog bundle manager over hand-written
# instance.yaml files: it must find the version of every `oci://` bundle and
# nothing else, because a manager that silently matches nothing leaves the
# instance on an old catalog for good. Also checks that the names Renovate
# tracks are the ones cd.yml publishes. Needs jq, yq and node.
set -uo pipefail

CONFIG="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/renovate.json"
fail=0

pattern=$(jq -r '.customManagers[] | select((.description // "") | startswith("Pinned catalog bundle")) | .matchStrings[0]' "$CONFIG")
files=$(jq -r '.customManagers[] | select((.description // "") | startswith("Pinned catalog bundle")) | .managerFilePatterns[0]' "$CONFIG")
[ -n "$pattern" ] || { echo "FAIL no manager for the catalog bundle in $CONFIG"; exit 1; }

# matches <yaml> -> "depName currentValue" per match, as Renovate extracts them
matches() {
  PATTERN=$pattern YAML=$1 node -e '
    const re = new RegExp(process.env.PATTERN, "g");
    for (const m of process.env.YAML.matchAll(re)) console.log(m.groups.depName + " " + m.groups.currentValue);'
}

check() { # check <label> <expected output> <yaml>
  local actual
  actual=$(matches "$3")
  if [ "$actual" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: expected '$2', got '$actual'"; fail=1; fi
}

check "a bundle on the registry is found" \
  "ghcr.io/this-is-tobi/ohmlab/catalog 0.2.0" \
  'catalogs:
  ohmlabCatalog:
    repoURL: oci://ghcr.io/this-is-tobi/ohmlab/catalog
    version: 0.2.0
defaultCatalog: ohmlabCatalog'

check "every bundle of the instance is found" \
  "ghcr.io/a/catalog 1.2.3
registry.example.com:5000/b/catalog 4.5.6" \
  'catalogs:
  one:
    repoURL: oci://ghcr.io/a/catalog
    version: 1.2.3
  two:
    repoURL: oci://registry.example.com:5000/b/catalog
    version: 4.5.6'

check "a bundle followed by other keys is found" \
  "ghcr.io/a/catalog 1.2.3" \
  'catalogs:
  one:
    repoURL: oci://ghcr.io/a/catalog
    version: 1.2.3
projects:
  core: admin-core'

check "a git bundle is left alone" "" \
  'catalogs:
  release:
    repoURL: https://github.com/this-is-tobi/homelab.git
    version: 0.1.0'

check "a registry catalog (a string) is left alone" "" \
  'catalogs:
  ohmlab: ghcr.io/this-is-tobi/charts'

check "the commented example is left alone" "" \
  '# catalogs:
#   release:
#     repoURL: oci://ghcr.io/example/catalog
#     version: 0.1.0'

check "a version that is not X.Y.Z is left alone" "" \
  'catalogs:
  one:
    repoURL: oci://ghcr.io/a/catalog
    version: latest'

check "any indentation is read" \
  "ghcr.io/a/catalog 1.2.3" \
  'catalogs:
    one:
        repoURL: oci://ghcr.io/a/catalog
        version: 1.2.3'

check "an oci URL under another key is left alone" "" \
  'something:
  mirror: oci://ghcr.io/a/catalog
  version: 1.2.3'

check "a pre-release pin is left alone instead of half matched" "" \
  'catalogs:
  one:
    repoURL: oci://ghcr.io/a/catalog
    version: 1.2.3-rc.1'

# What Renovate tracks must be what cd.yml publishes: a renamed package that
# Renovate still looks up under its old name never gets a bump again.
ROOT="$(dirname "$CONFIG")"
CD="$ROOT/.github/workflows/cd.yml"
chart="ghcr.io/$(yq '.jobs["publish-chart"].with.REPOSITORY' "$CD")/$(yq '.name' "$ROOT/utils/helm/Chart.yaml")"
tracked=$(jq -r '.customManagers[] | select((.description // "") | startswith("Pinned ohmlab bootstrap chart")) | .depNameTemplate' "$CONFIG")
if [ "$tracked" = "$chart" ]; then echo "ok   the chart pin manager follows the chart cd.yml publishes"; else
  echo "FAIL the chart pin manager tracks '$tracked', cd.yml publishes '$chart'"; fail=1
fi
for job in image image-controller; do
  image=$(yq ".jobs[\"$job\"].with.IMAGE_NAME" "$CD")
  if jq -e --arg i "$image" '.packageRules[] | select(.groupName == "ohmlab images") | .matchPackageNames | index($i)' "$CONFIG" >/dev/null; then
    echo "ok   the ohmlab images group lists $image"
  else
    echo "FAIL the ohmlab images group does not list $image (cd.yml job $job)"; fail=1
  fi
done

for path in argo-cd/instances/mylab/instance.yaml argo-cd/instances/_example/instance.yaml; do
  if FILES=$files P=$path node -e 'const [, body, flags] = /^\/(.*)\/([a-z]*)$/.exec(process.env.FILES); process.exit(new RegExp(body, flags).test(process.env.P) ? 0 : 1)'; then
    echo "ok   the manager reads $path"
  else
    echo "FAIL the manager does not read $path"; fail=1
  fi
done
if FILES=$files P=argo-cd/instances/mylab/values/core/ohmlab.yaml node -e 'const [, body, flags] = /^\/(.*)\/([a-z]*)$/.exec(process.env.FILES); process.exit(new RegExp(body, flags).test(process.env.P) ? 0 : 1)'; then
  echo "FAIL the manager reads the values files"; fail=1
else
  echo "ok   the manager leaves the values files alone"
fi

exit "$fail"
