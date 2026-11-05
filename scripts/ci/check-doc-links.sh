#!/usr/bin/env bash
# Every relative link of the docs resolves: the pages of docs/, the README and
# each example's README link each other, the charts and the instance folders by
# relative path, and a file moved or renamed leaves them pointing nowhere.
# Links with a scheme and anchors are not checked: an anchor after a path is
# dropped and the path checked. Links in code are not read.
#
#   check-doc-links.sh [<root>]
#
# Prints one `FAIL <file>: <path> does not exist` per broken link, else one ok
# line; exits with the number of failures, at most 255.
set -uo pipefail

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
cd "$root" || exit 1

# The link targets of a Markdown file, one per line.
targets() {
  awk '
    function emit(t) {
      sub(/^</, "", t); sub(/>$/, "", t)
      if (t == "" || t ~ /^#/ || t ~ /^[A-Za-z][A-Za-z0-9+.-]*:/) return
      print t
    }
    /^[[:space:]]*(```|~~~)/ { fence = !fence; next }
    fence { next }
    {
      line = $0
      gsub(/`[^`]*`/, "", line)
      if (match(line, /^[[:space:]]*\[[^\]]+\]:[[:space:]]*[^[:space:]]+/)) {
        def = substr(line, RSTART, RLENGTH)
        sub(/^[[:space:]]*\[[^\]]+\]:[[:space:]]*/, "", def)
        emit(def)
        next
      }
      while (match(line, /\]\([^)]*\)/)) {
        t = substr(line, RSTART + 2, RLENGTH - 3)
        line = substr(line, RSTART + RLENGTH)
        sub(/^[[:space:]]+/, "", t)
        sub(/[[:space:]].*$/, "", t)
        emit(t)
      }
    }' "$1"
}

failures=0 links=0 files=0
while IFS= read -r f; do
  files=$((files + 1))
  while IFS= read -r target; do
    links=$((links + 1))
    path="${target%%#*}"
    path="${path%%\?*}"
    if [ -n "$path" ] && [ ! -e "$(dirname "$f")/$path" ]; then
      echo "FAIL $f: $path does not exist"
      failures=$((failures + 1))
    fi
  done < <(targets "$f")
done < <({ [ -f README.md ] && echo README.md; find docs examples -name '*.md' 2>/dev/null | grep -E '^(docs/|examples/[^/]+/README\.md$)'; } | sort)

[ "$failures" -gt 0 ] || echo "ok   $links link(s) in $files file(s) resolve"
exit $((failures > 255 ? 255 : failures))
