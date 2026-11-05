#!/usr/bin/env bash
# Decision table for scripts/ci/check-doc-links.sh on a hand-written tree.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-doc-links.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

# check <label> <expected exit> <text the output must hold> <markdown of docs/page.md>
check() {
  local out rc
  rm -rf "$work/root"
  mkdir -p "$work/root/docs/sub" "$work/root/examples/one"
  printf '# Readme\n' >"$work/root/README.md"
  printf '# Other\n' >"$work/root/docs/other.md"
  printf '# Sub\n' >"$work/root/docs/sub/README.md"
  printf '# One\n' >"$work/root/examples/one/README.md"
  printf '%s\n' "$4" >"$work/root/docs/page.md"
  out=$("$SCRIPT" "$work/root" 2>&1); rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then echo "ok   $1"; else
    echo "FAIL $1 (exit $rc, want $2, looking for '$3')"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
  fi
}

check "a link to a file passes" 0 "ok   " "See [other](other.md)."
check "a link to a directory passes" 0 "ok   " "See [sub](sub/)."
check "a link up to the repository passes" 0 "ok   " "See [readme](../README.md) and [one](../examples/one/README.md)."
check "a link with an anchor passes when the file exists" 0 "ok   " "See [other](other.md#a-heading)."
check "a broken link fails, naming the file and the target" 1 "FAIL docs/page.md: missing.md does not exist" "See [gone](missing.md)."
check "a broken image fails" 1 "FAIL docs/page.md: img/x.png does not exist" "![x](img/x.png)"
check "a broken reference link fails" 1 "FAIL docs/page.md: gone.md does not exist" "$(printf 'See [it][ref].\n\n[ref]: gone.md\n')"
check "an external link is not checked" 0 "ok   " "See [site](https://example.com/x) and [mail](mailto:a@example.com)."
check "an anchor of the page itself is not checked" 0 "ok   " "See [below](#below)."
check "a link in a code block is not checked" 0 "ok   " "$(printf '```\n[x](missing.md)\n```\n')"
check "a link with a title passes" 0 "ok   " 'See [other](other.md "Other").'
check "every broken link of a page is reported" 2 "two.md does not exist" "See [a](one.md) and [b](two.md)."

exit "$fail"
