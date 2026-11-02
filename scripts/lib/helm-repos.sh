#!/usr/bin/env bash
# Shared helpers for the scripts that build charts from Chart.yaml files.

# retry <attempts> <command>...: runs the command until it succeeds, waiting
# RETRY_DELAY seconds (default 10) times the number of failed attempts in
# between. Upstream chart hosts fail transiently (a 500 from a release download
# is enough to fail a build), and a build must not depend on that luck.
retry() {
  # Bash locals are visible to the command that is run: names a caller could use too would be overwritten.
  local retry_max="$1" retry_n=1
  shift
  until "$@"; do
    [ "$retry_n" -lt "$retry_max" ] || return 1
    sleep $((${RETRY_DELAY:-10} * retry_n))
    retry_n=$((retry_n + 1))
  done
}

# add_dependency_repos <Chart.yaml>...: `helm repo add` every http(s) repository
# the charts depend on, then refresh the indexes. `helm dependency build` fails
# for a repository that is not added first (OCI repositories need nothing).
# A repository is named after a checksum of its URL: helm skips adding a name
# again with the same URL but refuses it with another one, and a second call
# over other charts in the same helm configuration (a pull request, then its
# base) must find each URL under the name it already has.
add_dependency_repos() {
  local repo
  while IFS= read -r repo; do
    retry 3 helm repo add "repo-$(printf '%s' "$repo" | cksum | cut -d' ' -f1)" "$repo" >/dev/null
  done < <(yq -N '.dependencies[]?.repository' "$@" 2>/dev/null | grep '^http' | sort -u)
  retry 3 helm repo update >/dev/null
}

# vendor_dependencies <log-dir> <chart-dir>...: `helm dependency build` for every
# chart, DEPENDENCY_JOBS (default 6) at a time with transient failures retried.
# The builds are network bound and independent, and none refreshes the
# indexes again: without --skip-refresh each one downloads the index of every
# added repository, so call add_dependency_repos first. Every chart is
# attempted; the ones that could not be built are listed in <log-dir>/failed
# (one directory per line) with their output in <log-dir>/<name>.log, and the
# return status is 1 when there is any.
vendor_dependencies() {
  local logdir="$1"
  shift
  [ "$#" -gt 0 ] || return 0
  rm -f "$logdir/failed"
  export VENDOR_LOG_DIR="$logdir"
  printf '%s\0' "$@" | xargs -0 -n1 -P "${DEPENDENCY_JOBS:-6}" bash -c '
    source "$0"
    chart=$1
    log="$VENDOR_LOG_DIR/$(printf "%s" "$chart" | tr "/" "_").log"
    retry 3 helm dependency build --skip-refresh "$chart" >"$log" 2>&1 || { echo "$chart" >>"$VENDOR_LOG_DIR/failed"; exit 1; }
  ' "${BASH_SOURCE[0]}" >/dev/null 2>&1
  [ ! -s "$logdir/failed" ] || return 1
}
