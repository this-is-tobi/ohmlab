#!/usr/bin/env bash
# Compares two renders of the same instance (render-instance.sh with
# RENDER_OUT): the product at a pull request's base and at its head. An object
# of the base that the head no longer renders would be deleted, or recreated
# under another name, when the change reaches that instance's cluster: that
# fails. Objects whose content changes, and new ones (a new app's included),
# are listed for review (in the job summary on GitHub Actions); a name kept
# inside an object, such as a Vault mount, shows up there.
#
# A removal the change means is acknowledged in ALLOWED_REMOVALS, one
# "<app>: <object>" per line as reported below (validate-instance.yml reads
# them from the `Removes:` trailers of the change's commits): it is listed,
# and only the others fail.
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
# An app only the head renders: every object of it is new.
while IFS= read -r file; do
  app="${file#"$head"/}"; app="${app%.objects}"
  [ -f "$base/$app.objects" ] && continue
  while IFS=' ' read -r id _; do added+=("$app: $id"); done <"$file"
done < <(cd / && command find "$head" -name '*.objects' | sort)

report() { # report <title> <items...>
  local title="$1"; shift
  [ "$#" -gt 0 ] || return 0
  echo "$title ($#):"; printf '  %s\n' "$@"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    { echo "### $title ($#)"; printf -- '- `%s`\n' "$@"; echo; } >>"$GITHUB_STEP_SUMMARY"
  fi
}
# Commit messages are anyone's text: printable characters only reach the log.
allowed=$(printf '%s\n' "${ALLOWED_REMOVALS:-}" | LC_ALL=C tr -cd '[:print:]\n')
acknowledged=() unacknowledged=() stale=()
for item in "${removed[@]+"${removed[@]}"}"; do
  if grep -qxF -- "$item" <<<"$allowed"; then acknowledged+=("$item"); else unacknowledged+=("$item"); fi
done
while IFS= read -r item; do
  [ -n "$item" ] || continue
  printf '%s\n' "${removed[@]+"${removed[@]}"}" | grep -qxF -- "$item" || stale+=("$item")
done <<<"$allowed"

report "Deleted or renamed" "${unacknowledged[@]+"${unacknowledged[@]}"}"
report "Deleted or renamed, acknowledged by the change" "${acknowledged[@]+"${acknowledged[@]}"}"
report "Changed" "${changed[@]+"${changed[@]}"}"
report "Added" "${added[@]+"${added[@]}"}"
report "Acknowledged but not removed" "${stale[@]+"${stale[@]}"}"

if [ "${#unacknowledged[@]}" -gt 0 ]; then
  echo "FAIL ${#unacknowledged[@]} object(s) of the base would be deleted or renamed on the instance's cluster"
  exit 1
fi
if [ "${#acknowledged[@]}" -gt 0 ]; then
  echo "ok   ${#acknowledged[@]} acknowledged removal(s), nothing else deleted or renamed (${#changed[@]} changed, ${#added[@]} added)"
else
  echo "ok   nothing deleted or renamed (${#changed[@]} changed, ${#added[@]} added)"
fi
