#!/usr/bin/env bash
# Compares two renders of the same instance (render-instance.sh with
# RENDER_OUT): the product at a pull request's base and at its head. An object
# of the base that the head no longer renders would be deleted, or recreated
# under another name, when the change reaches that instance's cluster: that
# fails. Objects whose content changes, and new ones, are listed for review
# (in the job summary on GitHub Actions); a name kept inside an object, such
# as a Vault mount, shows up there.
#
#   compare-instance-renders.sh <base render dir> <head render dir>
set -uo pipefail

base="${1:?usage: compare-instance-renders.sh <base dir> <head dir>}"
head="${2:?usage: compare-instance-renders.sh <base dir> <head dir>}"
removed=() changed=() added=()

while IFS= read -r file; do
  app="${file#"$base"/}"; app="${app%.objects}"
  if [ ! -f "$head/$app.objects" ]; then
    removed+=("$app: the app no longer renders")
    continue
  fi
  while IFS=' ' read -r id hash; do
    other=$(awk -v id="$id" '$1 == id {print $2}' "$head/$app.objects")
    if [ -z "$other" ]; then removed+=("$app: $id")
    elif [ "$other" != "$hash" ]; then changed+=("$app: $id"); fi
  done <"$file"
  while IFS=' ' read -r id _; do
    grep -q "^$id " "$file" || added+=("$app: $id")
  done <"$head/$app.objects"
done < <(cd / && command find "$base" -name '*.objects' | sort)

report() { # report <title> <items...>
  local title="$1"; shift
  [ "$#" -gt 0 ] || return 0
  echo "$title ($#):"; printf '  %s\n' "$@"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    { echo "### $title ($#)"; printf -- '- `%s`\n' "$@"; echo; } >>"$GITHUB_STEP_SUMMARY"
  fi
}
report "Deleted or renamed" "${removed[@]+"${removed[@]}"}"
report "Changed" "${changed[@]+"${changed[@]}"}"
report "Added" "${added[@]+"${added[@]}"}"

if [ "${#removed[@]}" -gt 0 ]; then
  echo "FAIL ${#removed[@]} object(s) of the base would be deleted or renamed on the instance's cluster"
  exit 1
fi
echo "ok   nothing deleted or renamed (${#changed[@]} changed, ${#added[@]} added)"
