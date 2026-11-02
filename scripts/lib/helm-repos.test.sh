#!/usr/bin/env bash
# Exercises scripts/lib/helm-repos.sh with a stubbed helm. vendor_dependencies:
# every chart is built without refreshing the indexes again, several at a time
# but never more than asked, transient failures are retried, and a chart that
# cannot be built is reported without stopping the others. add_dependency_repos:
# every http(s) repository is added once, a second call over other charts in the
# same helm configuration (a pull request and then its base) adds what is new.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=helm-repos.sh
source "$ROOT/scripts/lib/helm-repos.sh"

dir=$(mktemp -d)
trap 'rm -rf "${dir:?}"' EXIT
mkdir "$dir/bin"
fail=0
export RETRY_DELAY=0

# helm stub: records its arguments and how many builds run at the same time.
# CHART_FAILS=<name>:<n> makes the chart <name> fail its first <n> builds.
# `repo add` keeps helm's rule: adding a name again is skipped for the same URL
# and refused for another one.
cat >"$dir/bin/helm" <<'STUB'
#!/usr/bin/env bash
if [ "$1 $2" = "repo add" ]; then
  known=$(awk -v n="$3" '$1 == n { print $2 }' "$STUB_DIR/repos" 2>/dev/null)
  [ -n "$known" ] || { echo "$3 $4" >>"$STUB_DIR/repos"; exit 0; }
  [ "$known" = "$4" ] && exit 0
  echo "Error: repository name ($3) already exists, please specify a different name" >&2; exit 1
fi
[ "$1 $2" = "dependency build" ] || exit 0
chart=${*: -1}; name=$(basename "$chart")
echo "$*" >>"$STUB_DIR/calls"
mkdir -p "$STUB_DIR/running"; : >"$STUB_DIR/running/$$"
n=$(find "$STUB_DIR/running" -type f | wc -l | tr -d ' '); echo "$n" >>"$STUB_DIR/concurrency"
sleep 0.3
rm -f "$STUB_DIR/running/$$"
case ",${CHART_FAILS:-}," in
  *",$name:"*)
    want=$(printf '%s' "$CHART_FAILS" | tr ',' '\n' | awk -F: -v n="$name" '$1 == n { print $2 }')
    echo x >>"$STUB_DIR/tries-$name"; tries=$(wc -l <"$STUB_DIR/tries-$name" | tr -d ' ')
    if [ "$tries" -le "$want" ]; then echo "Error: cannot reach the repository for $name" >&2; exit 1; fi ;;
esac
exit 0
STUB
chmod +x "$dir/bin/helm"
export PATH="$dir/bin:$PATH" STUB_DIR="$dir/stub"

reset() { rm -rf "${dir:?}/stub" "${dir:?}/logs"; mkdir "$dir/stub" "$dir/logs"; unset CHART_FAILS DEPENDENCY_JOBS; }
charts() { local i; for i in $(seq 1 "$1"); do mkdir -p "$dir/charts/c$i"; printf '%s/charts/c%s\n' "$dir" "$i"; done; }

check() { # check <label> <expected> <actual>
  if [ "$3" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1"; printf '     expected: %s\n     actual:   %s\n' "$2" "$3"; fail=1; fi
}

reset; mapfile -t six < <(charts 6)
export DEPENDENCY_JOBS=3
vendor_dependencies "$dir/logs" "${six[@]}"; rc=$?
check "six charts build" "0" "$rc"
check "every chart is built once" "6" "$(wc -l <"$dir/stub/calls" | tr -d ' ')"
check "no build refreshes the indexes again" "6" "$(grep -c -- '--skip-refresh' "$dir/stub/calls")"
check "several charts build at the same time" "yes" "$([ "$(sort -n "$dir/stub/concurrency" | tail -1)" -ge 2 ] && echo yes || echo no)"
check "never more at a time than DEPENDENCY_JOBS" "yes" "$([ "$(sort -n "$dir/stub/concurrency" | tail -1)" -le 3 ] && echo yes || echo no)"

reset; mapfile -t six < <(charts 6)
vendor_dependencies "$dir/logs" "${six[@]}"
check "six builds at a time by default, none more" "yes" "$([ "$(sort -n "$dir/stub/concurrency" | tail -1)" -le 6 ] && echo yes || echo no)"

reset; mapfile -t two < <(charts 2)
CHART_FAILS="c1:2" vendor_dependencies "$dir/logs" "${two[@]}"; rc=$?
check "a chart that fails twice and then builds succeeds" "0" "$rc"
check "...after three attempts" "3" "$(wc -l <"$dir/stub/tries-c1" | tr -d ' ')"

reset; mapfile -t three < <(charts 3)
CHART_FAILS="c2:99" vendor_dependencies "$dir/logs" "${three[@]}"; rc=$?
check "a chart that keeps failing fails the run" "1" "$rc"
check "...lists that chart, and only that chart, as failed" "$dir/charts/c2" "$(cat "$dir/logs/failed" 2>/dev/null)"
check "...keeps the reason in the log of that chart" "yes" "$(grep -q 'cannot reach the repository for c2' "$dir/logs/$(printf '%s' "$dir/charts/c2" | tr / _).log" && echo yes || echo no)"
check "...while the others are still built" "yes" "$(grep -q 'c1' "$dir/stub/calls" && grep -q 'c3' "$dir/stub/calls" && echo yes || echo no)"

reset; mapfile -t two < <(charts 2)
CHART_FAILS="c1:99" vendor_dependencies "$dir/logs" "${two[@]}"
rm -f "$dir/stub/tries-c1"; unset CHART_FAILS
vendor_dependencies "$dir/logs" "${two[@]}"; check "a rerun in the same directory forgets the failures of the last one" "0" "$?"

reset
vendor_dependencies "$dir/logs"; check "no chart is not an error" "0" "$?"
check "...and helm is not run" "no" "$([ -e "$dir/stub/calls" ] && echo yes || echo no)"

# deps <name> <repository>...: writes a chart depending on each repository and
# prints its Chart.yaml. added: the URLs the stub was given, sorted.
deps() {
  local chart="$dir/deps/$1" repo
  shift
  mkdir -p "$chart"
  {
    printf 'apiVersion: v2\nname: %s\nversion: 0.1.0\ndependencies:\n' "$(basename "$chart")"
    for repo in "$@"; do printf -- '- name: x\n  version: 1.0.0\n  repository: %s\n' "$repo"; done
  } >"$chart/Chart.yaml"
  printf '%s/Chart.yaml\n' "$chart"
}
added() { awk '{ print $2 }' "$dir/stub/repos" 2>/dev/null | sort | paste -sd' ' -; }

reset
add_dependency_repos "$(deps one https://a.example/charts https://b.example)" "$(deps two https://b.example oci://registry.example/charts https://c.example/)"; rc=$?
check "the repositories of the charts are added" "0" "$rc"
check "...every http(s) one once, and no OCI one" "https://a.example/charts https://b.example https://c.example/" "$(added)"

reset
add_dependency_repos "$(deps head https://a.example https://c.example)"
add_dependency_repos "$(deps base https://a.example https://b.example https://c.example)"; rc=$?
check "a second call over other charts succeeds" "0" "$rc"
check "...and adds the repository the first one did not" "https://a.example https://b.example https://c.example" "$(added)"
exit "$fail"
