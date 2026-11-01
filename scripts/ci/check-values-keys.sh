#!/usr/bin/env bash
# Every top-level key of a catalog chart's values, and of each instance's
# values file, reaches the chart it is given to: it names a dependency (its
# alias, or its name when it has none), is `global`, or is read by the chart's
# own templates. Helm drops any other key without a word, so a misspelled
# subchart key, or a block for a dependency the chart lacks, configures nothing.
#
#   check-values-keys.sh [<root>]   (default: the repository root)
#
# The instances are INSTANCES_DIR's and the example. An entry is checked
# against the chart its Application renders: its chartPath, else
# argo-cd/apps/<chart or app>, utils/helm for the ohmlab app. Needs yq
# (mikefarah v4).
set -uo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")/../lib" && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$LIB/instances.sh"

bad=0 files=0

# reached <chart dir>: the top-level keys a values file can give the chart.
reached() {
  {
    yq -r '.dependencies[] | .alias // .name' "$1/Chart.yaml"
    echo global
    if [ -d "$1/templates" ]; then
      grep -rhoE '\.Values\.[A-Za-z_][A-Za-z0-9_]*|index \(?\$?\.Values\)? "[^"]+"' "$1/templates" \
        | sed -E 's/^\.Values\.//; s/^index \(?\$?\.Values\)? "([^"]+)"$/\1/'
    fi
  } | sort -u
}

# check_file <values file> <chart dir>
check_file() {
  local file="$1" dir="$2" allowed deps key
  if [ ! -f "$dir/Chart.yaml" ]; then
    echo "FAIL ${file#"$ROOT"/}: no chart at ${dir#"$ROOT"/}"
    bad=$((bad + 1))
    return
  fi
  files=$((files + 1))
  allowed=$(reached "$dir")
  deps=$(yq -r '.dependencies[] | .alias // .name' "$dir/Chart.yaml" | paste -sd, - | sed 's/,/, /g')
  while IFS= read -r key; do
    [ -n "$key" ] || continue
    grep -qxF -- "$key" <<<"$allowed" && continue
    echo "FAIL ${file#"$ROOT"/}: '$key' reaches no chart: neither a dependency of ${dir#"$ROOT"/} (${deps:-none}), nor global, nor read by its templates"
    bad=$((bad + 1))
  done < <(yq -r '(. // {}) | keys | .[]' "$file")
}

for dir in "$ROOT"/argo-cd/apps/*/ "$ROOT/utils/helm/"; do
  dir="${dir%/}"
  [ -f "$dir/Chart.yaml" ] && [ -f "$dir/values.yaml" ] && check_file "$dir/values.yaml" "$dir"
done

while IFS= read -r inst; do
  for tier in core tenant; do
    [ -f "$inst/$tier.yaml" ] || continue
    while IFS=$'\t' read -r app chart path; do
      values="$inst/values/$tier/$app.yaml"
      # Argo CD refuses an entry without its values file: nothing to deploy.
      [ -f "$values" ] || continue
      if [ -n "$path" ]; then dir="$ROOT/$path"
      elif [ "$chart" = ohmlab ]; then dir="$ROOT/utils/helm"
      else dir="$ROOT/argo-cd/apps/$chart"
      fi
      check_file "$values" "$dir"
    done < <(yq -r '.apps[] | [.app, (.chart // .app), (.chartPath // "")] | @tsv' "$inst/$tier.yaml")
  done
done < <(instance_dirs; printf '%s\n' "$EXAMPLE_DIR")

if [ "$bad" -gt 0 ]; then
  echo "check-values-keys: $bad key(s) reach no chart: Helm drops them, so they configure nothing" >&2
  exit 1
fi
echo "ok   $files values file(s), every key reaches its chart"
