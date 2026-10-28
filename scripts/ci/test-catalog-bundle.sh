#!/usr/bin/env bash
# Checks the catalog bundle built by scripts/build-catalog.sh: it holds exactly
# the files git tracks (plus the vendored dependencies of each Chart.lock) and
# nothing that must never be published, and every chart renders from it exactly
# as it does from the repository, with the values of the example instance and of
# the real ones. Needs git, helm, yq (mikefarah v4) and jq.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"
# shellcheck source=../lib/helm-repos.sh
source "$ROOT/scripts/lib/helm-repos.sh"

work=$(mktemp -d)
planted=()
cleanup() {
  rm -rf "$work"
  [ "${#planted[@]}" -eq 0 ] || rm -f "${planted[@]}"
  [ -z "${made_dir:-}" ] || rmdir "$made_dir" 2>/dev/null
}
trap cleanup EXIT
fail=0

ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; shift; [ $# -eq 0 ] || printf '     %s\n' "$@"; fail=1; }
check() { # label expected actual
  if [ "$3" = "$2" ]; then ok "$1"; else bad "$1" "expected: $2" "actual:   $3"; fi
}

# --- retry of transient failures ---------------------------------------------
export RETRY_DELAY=0
attempts=0
flaky() { attempts=$((attempts + 1)); [ "$attempts" -ge 3 ]; }
retry 3 flaky; check "a command that fails twice and then works succeeds" 0 "$?"
check "...after three attempts" 3 "$attempts"
attempts=0; retry 2 flaky; check "a command that keeps failing fails once the attempts are used" 1 "$?"
check "...after two attempts" 2 "$attempts"
retry 3 true; check "a command that works is not repeated or failed" 0 "$?"

# Files that must never reach the bundle: a plaintext secret and a stale vendored
# chart, both ignored by git. They are planted in the working tree before the build.
sops="$ROOT/argo-cd/apps/sops"
if [ ! -d "$sops/charts" ]; then mkdir -p "$sops/charts"; made_dir="$sops/charts"; fi
secret="$sops/templates/zz-canary.dec.yaml"
stale="$sops/charts/zz-canary-0.0.1.tgz"
for f in "$secret" "$stale"; do printf 'canary\n' >"$f"; planted+=("$f"); done
for f in "$secret" "$stale"; do
  git -C "$ROOT" check-ignore -q "$f" || { bad "the canary ${f#"$ROOT"/} is not ignored by git, the check cannot plant it"; exit 1; }
done

"$ROOT/scripts/build-catalog.sh" "$work/out" >"$work/build.log" 2>&1 \
  || { bad "the bundle builds" "$(tail -3 "$work/build.log")"; exit 1; }
ok "the bundle builds"
mkdir "$work/bundle" && tar -xzf "$work/out/catalog.tar.gz" -C "$work/bundle" || { bad "the bundle extracts"; exit 1; }

# The repository side of the comparison is a copy of the tracked files: rendering
# the working tree would also render ignored local files (a plaintext secret).
mkdir "$work/repo"
git -C "$ROOT" ls-files -z -- argo-cd/apps argo-cd/instances/_example utils/helm \
  | while IFS= read -r -d '' f; do [ -f "$ROOT/$f" ] && printf '%s\0' "$f"; done \
  | (cd "$ROOT" && COPYFILE_DISABLE=1 tar --null -T - -cf -) | tar -xf - -C "$work/repo"

# --- content ---------------------------------------------------------------
tracked=$(git -C "$ROOT" ls-files -- argo-cd/apps argo-cd/instances/_example utils/helm | while IFS= read -r f; do [ -f "$ROOT/$f" ] && echo "$f"; done | sort)
bundled=$(cd "$work/bundle" && find . -type f | sed 's|^\./||' | grep -v -E '/charts/[^/]+\.tgz$' | sort)
extra=$(comm -13 <(echo "$tracked") <(echo "$bundled"))
missing=$(comm -23 <(echo "$tracked") <(echo "$bundled"))
check "the bundle holds no file git does not track" "" "$extra"
check "the bundle holds every tracked file" "" "$missing"
check "the files of the bundle are the tracked files, byte for byte" "" "$(diff -rq -x charts "$work/repo" "$work/bundle")"
check "a plaintext secret and a stale vendored chart are left out" "" "$(cd "$work/bundle" && find . -name 'zz-canary*')"

# --- vendored dependencies -----------------------------------------------------
for lock in "$work"/bundle/argo-cd/apps/*/Chart.lock "$work/bundle/utils/helm/Chart.lock"; do
  [ -f "$lock" ] || continue
  dir=$(dirname "$lock")
  label="${dir#"$work"/bundle/}"
  want=$(yq -N '.dependencies[] | .name + " " + .version' "$lock" | sort -u)
  have=$(for f in "$dir"/charts/*.tgz; do [ -f "$f" ] && helm show chart "$f" | yq -N '.name + " " + .version'; done | sort -u)
  check "$label vendors exactly the dependencies of its Chart.lock" "$want" "$have"
done

# --- the same render as from the repository -------------------------------------
export HELM_REPOSITORY_CONFIG="$work/repositories.yaml"
export HELM_REPOSITORY_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/ohmlab-catalog/helm"
mkdir -p "$HELM_REPOSITORY_CACHE"
add_dependency_repos "$ROOT"/argo-cd/apps/*/Chart.yaml "$ROOT/utils/helm/Chart.yaml"

# mask <file>: the lines of the render with the value of every key listed in
# $work/volatile replaced, since a chart that generates random material changes
# it on every render.
mask() {
  awk -v vf="$work/volatile" '
    BEGIN { while ((getline l < vf) > 0) { i = index(l, ": "); if (i > 0) k[substr(l, 1, i + 1)] = 1; else e[l] = 1 } }
    { i = index($0, ": ")
      if (i > 0 && (substr($0, 1, i + 1) in k)) print substr($0, 1, i + 1) "<volatile>"
      else if ($0 in e) print "<volatile>"
      else print }'
}

# The example, then every real instance of INSTANCES_DIR.
instances=("$EXAMPLE_DIR")
while IFS= read -r d; do instances+=("$d"); done < <(instance_dirs)

# The repository side builds its own dependencies, independently of the bundle's.
repo_charts=()
for chartdir in utils/helm argo-cd/apps/*/; do
  chartdir="${chartdir%/}"
  [ -f "$ROOT/$chartdir/Chart.yaml" ] || continue
  yq -e '.dependencies | length > 0' "$ROOT/$chartdir/Chart.yaml" >/dev/null 2>&1 && repo_charts+=("$work/repo/$chartdir")
done
mkdir "$work/deps"
vendor_dependencies "$work/deps" ${repo_charts[@]+"${repo_charts[@]}"}

for chartdir in utils/helm argo-cd/apps/*/; do
  chartdir="${chartdir%/}"
  [ -f "$ROOT/$chartdir/Chart.yaml" ] || continue
  name=$(basename "$chartdir"); [ "$name" = helm ] && name=ohmlab
  if grep -qxF "$work/repo/$chartdir" "$work/deps/failed" 2>/dev/null; then
    bad "$name: helm dependency build in the repository"; continue
  fi

  for inst_dir in "${instances[@]}"; do
    inst=$(basename "$inst_dir")
    # The example renders every chart; a real instance only the ones it deploys.
    args=()
    if [ "$name" = instance-manager ]; then
      [ "$inst" = _example ] || continue
      args=(-f "$EXAMPLE_DIR/instance.yaml" --set instance.name=example)
    else
      vf=""
      for scope in core tenant; do
        [ -f "$inst_dir/values/$scope/$name.yaml" ] && { vf="$inst_dir/values/$scope/$name.yaml"; break; }
      done
      if [ -n "$vf" ]; then args=(-f "$vf"); elif [ "$inst" != _example ]; then continue; fi
    fi
    label="$name renders the same from the bundle ($inst)"
    if ! git_a=$(helm template catalog "$work/repo/$chartdir" "${args[@]+"${args[@]}"}" 2>"$work/err"); then
      bad "$label" "does not render from the repository: $(grep -m1 '^Error' "$work/err" | cut -c1-200)"; continue
    fi
    if ! from_bundle=$(helm template catalog "$work/bundle/$chartdir" "${args[@]+"${args[@]}"}" 2>"$work/err"); then
      bad "$label" "does not render from the bundle: $(grep -m1 '^Error' "$work/err" | cut -c1-200)"; continue
    fi
    if [ "$git_a" = "$from_bundle" ]; then ok "$label"; continue; fi
    # Some charts generate random material on every render: lines that differ
    # between two renders of the repository are not compared.
    git_b=$(helm template catalog "$work/repo/$chartdir" "${args[@]+"${args[@]}"}" 2>/dev/null)
    diff <(printf '%s\n' "$git_a") <(printf '%s\n' "$git_b") | grep -E '^[<>] ' | sed -E 's/^[<>] //' | sort -u >"$work/volatile"
    left=$(printf '%s\n' "$git_a" | mask)
    right=$(printf '%s\n' "$from_bundle" | mask)
    if [ "$left" = "$right" ]; then ok "$label"; else
      # Values are masked: a render can hold secret material.
      bad "$label" "$(diff <(printf '%s\n' "$left") <(printf '%s\n' "$right") | head -6 | sed -E 's/: .*/: <masked>/' | cut -c1-160)"
    fi
  done
done

exit "$fail"
