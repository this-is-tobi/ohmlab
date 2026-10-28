# shellcheck shell=bash
# Where the instance folders are, for the checks that render charts with an
# instance's values. INSTANCES_DIR is a directory laid out like
# argo-cd/instances, for an instance kept in a repository of its own; it
# defaults to this repository's. The documented _example always comes from this
# repository. The caller sets ROOT, the repository root.

: "${INSTANCES_DIR:=$ROOT/argo-cd/instances}"
INSTANCES_DIR="${INSTANCES_DIR%/}"
EXAMPLE_DIR="$ROOT/argo-cd/instances/_example"

# instance_dirs: the real instance folders of INSTANCES_DIR (a leading `_`
# marks a template), one per line, sorted.
instance_dirs() {
  local d
  for d in "$INSTANCES_DIR"/*/; do
    [ -d "$d" ] || continue
    d="${d%/}"
    case "${d##*/}" in _*) continue ;; esac
    printf '%s\n' "$d"
  done
}

# values_file <app>: the values file production would render the app with, the
# first found among the real instances and then the example; nothing when none
# has one.
values_file() {
  local d scope
  while IFS= read -r d; do
    for scope in core tenant; do
      if [ -f "$d/values/$scope/$1.yaml" ]; then printf '%s\n' "$d/values/$scope/$1.yaml"; return; fi
    done
  done < <(instance_dirs; printf '%s\n' "$EXAMPLE_DIR")
}
