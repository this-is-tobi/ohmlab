#!/usr/bin/env bash
# Prints "<chart reference><TAB><version>" for the ohmlab bootstrap chart of an
# instance, so `run.sh -b` installs the same chart the self-managed Application
# runs: when the `ohmlab` entry of core.yaml has a `catalog`, the published
# chart (oci://<registry>/<chart>) at the exact version pinned in
# `targetRevision`; otherwise the local chart directory, with an empty version.
# Needs yq (mikefarah v4).
set -euo pipefail

instance_dir="${1:?usage: ohmlab-chart.sh <instance-dir> [<local-chart-dir>]}"
local_chart="${2:-charts/ohmlab}"
core="$instance_dir/core.yaml"
instance="$instance_dir/instance.yaml"

die() { echo "ohmlab-chart: $*" >&2; exit 1; }

entry() { yq ".apps[] | select(.app == \"ohmlab\") | $1" "$core"; }

catalog=$(entry '.catalog // ""')
if [[ -z "$catalog" ]]; then
  printf '%s\t\n' "$local_chart"
  exit 0
fi

version=$(entry '.targetRevision // ""')
chart=$(entry '.chart // .app')
registry=$(CATALOG="$catalog" yq '.catalogs[strenv(CATALOG)] // ""' "$instance")

[[ -n "$registry" ]] || die "catalog '$catalog' is not declared under catalogs in $instance"
# A bundle ({repoURL, version}) is a tree for Argo CD, not a chart helm can install.
[[ "$(CATALOG="$catalog" yq '.catalogs[strenv(CATALOG)] | tag' "$instance")" == "!!str" ]] \
  || die "catalog '$catalog' is a bundle; the ohmlab chart needs a registry path (a string) under catalogs in $instance"
# An exact version only: a range or a tag name would not be what the
# self-managed Application runs, and a registry tag can be re-pushed.
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] \
  || die "the ohmlab entry of $core needs an exact chart version in targetRevision (got '${version}')"

printf 'oci://%s/%s\t%s\n' "$registry" "$chart" "$version"
