#!/usr/bin/env bash
# Check the release setup of cd.yml against release-please-config.json.
#
#   check-release-gating.sh [release-please-config.json [workflow.yml [manifest.json]]]
#
# The release job answers for every package of release-please-config.json at
# once, and `releases-created` is true when any one of them was released. A job
# that publishes package P must therefore be gated on P itself:
#
#   if: ${{ contains(fromJSON(needs.release.outputs.paths-released), 'P') }}
#
# Without it, releasing the catalog also builds the CLI image with a version
# that does not exist, and the other way round. A job counts as gated when it,
# or any job it needs, carries that condition. A condition naming several
# packages is an OR: such a job may run for any of them, so it must not read
# the outputs of one (they are empty when another one was released); it may
# only rely on a job it needs that is gated on that package alone.
#
# A package released as `simple` also needs its version file in the repository:
# release-please only updates one that exists, so without it the release PR
# carries a changelog and no version, and the published artifact cannot say
# which version it is.
#
# The manifest lists the same packages as the config. A package missing from it
# counts as never released: release-please opens its first release again
# instead of continuing from the last one, which is what a package renamed in
# the config only would get. An entry that names no package is the same
# mistake seen from the other file. Needs yq and jq.
set -euo pipefail

CONFIG=${1:-release-please-config.json}
WORKFLOW=${2:-.github/workflows/cd.yml}
MANIFEST=${3:-$(dirname "$CONFIG")/.release-please-manifest.json}

packages=$(jq -c '[.packages | keys[]]' "$CONFIG")
jobs=$(yq -o=json '.jobs' "$WORKFLOW")

# One TSV row per finding: <kind> <job> <path>.
#   reads  - the job reads the per-path outputs of <path>
#   gated  - the job (or a job it needs) is gated on <path>
#   sure   - the job (or a job it needs) is gated on <path> alone, so it runs
#            only when <path> was released
#   any    - the job's own condition tests `releases-created`, whichever
#            package was released
rows=$(jq -r --arg q "'" '
  def listed: if type == "string" then [.] elif type == "array" then . else [] end;
  def own($jobs; $name):
    [$jobs[$name].if // "" | tostring | scan("paths-released\\), \($q)([^\($q)]+)\($q)") | .[0]] | unique;
  def closure($jobs; $name):
    [$name] + ([($jobs[$name].needs | listed)[] | closure($jobs; .)] | add // []) | unique;
  . as $jobs
  | to_entries[]
  | .key as $name
  | (.value | tojson) as $text
  | closure($jobs; $name) as $up
  | ([$text | scan("release-outputs\\)\\[\($q)([^\($q)]+)--") | .[0]] | unique[] | ["reads", $name, .]),
    ([$up[] | own($jobs; .)[]] | unique[] | ["gated", $name, .]),
    ([$up[] | own($jobs; .) | select(length == 1) | .[0]] | unique[] | ["sure", $name, .]),
    (select(.value.if // "" | tostring | test("releases-created")) | ["any", $name, ""])
  | @tsv' <<<"$jobs")

fail=0
say() { printf '%s\n' "$*"; }

while IFS=$'\t' read -r kind job path; do
  [ "$kind" = reads ] || continue
  if ! grep -qxF "sure"$'\t'"$job"$'\t'"$path" <<<"$rows"; then
    say "FAIL $job reads the release outputs of '$path' but does not run only when it is released: gate it, or a job it needs, on '$path' alone"
    fail=1
  fi
done <<<"$rows"

while IFS=$'\t' read -r kind job _; do
  [ "$kind" = any ] || continue
  if ! awk -F'\t' -v j="$job" '$1 == "gated" && $2 == j { found = 1 } END { exit !found }' <<<"$rows"; then
    say "FAIL $job is gated on any release (releases-created); gate it on its package with paths-released"
    fail=1
  fi
done <<<"$rows"

while IFS=$'\t' read -r kind job path; do
  [ "$kind" = gated ] || continue
  if ! jq -e --arg p "$path" 'index($p) != null' <<<"$packages" >/dev/null; then
    say "FAIL $job is gated on '$path', which is not a package of $CONFIG"
    fail=1
  fi
done <<<"$rows"

for path in $(jq -r '.[]' <<<"$packages"); do
  if ! awk -F'\t' -v p="$path" '$1 == "gated" && $3 == p { found = 1 } END { exit !found }' <<<"$rows"; then
    say "FAIL no job of $WORKFLOW is gated on the release of '$path'"
    fail=1
  fi
done

while IFS=$'\t' read -r path file; do
  if [ ! -f "$(dirname "$CONFIG")/$path/$file" ]; then
    say "FAIL '$path' is released as simple but $path/$file does not exist: release-please only updates an existing file, so add it with the version before the first release (0.0.0)"
    fail=1
  fi
done < <(jq -r '.packages | to_entries[] | select(.value["release-type"] == "simple") | [.key, (.value["version-file"] // "version.txt")] | @tsv' "$CONFIG")

if [ ! -f "$MANIFEST" ]; then
  say "FAIL $MANIFEST does not exist: release-please needs the version each package was last released at"
  fail=1
else
  entries=$(jq -c 'keys' "$MANIFEST")
  for path in $(jq -r '.[]' <<<"$packages"); do
    if ! jq -e --arg p "$path" 'index($p) != null' <<<"$entries" >/dev/null; then
      say "FAIL '$path' is a package of $CONFIG but has no entry in $MANIFEST: release-please would release it as new instead of continuing from its last release"
      fail=1
    fi
  done
  for path in $(jq -r '.[]' <<<"$entries"); do
    if ! jq -e --arg p "$path" 'index($p) != null' <<<"$packages" >/dev/null; then
      say "FAIL '$path' has an entry in $MANIFEST but is not a package of $CONFIG: renamed in one of the two files only?"
      fail=1
    fi
  done
fi

[ "$fail" = 0 ] && say "ok   every package release runs only its own jobs, has a manifest entry and has what its release type updates"
exit "$fail"
