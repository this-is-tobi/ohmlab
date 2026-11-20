#!/bin/sh
# Copies each replicated bucket of this store to the cloud replica, encrypted
# on the client side: rclone's crypt remote over <bucket>/<prefix> of the
# cloud bucket, so the provider holds ciphertext and encrypted names only.
#
# A bucket that holds less than half of what its replica holds fails, so a
# store rebuilt empty, or refilled by its producers after a lost volume, never
# turns the replica's objects into delete markers. A listed bucket missing at
# the source, or a replica that cannot be listed, fails too. Any failed bucket
# fails the run; the others still copy.
set -eu

: "${BUCKETS?}" "${SOURCE_ENDPOINT:?}" "${SOURCE_REGION:?}" "${SOURCE_ACCESS_KEY:?}" "${SOURCE_SECRET_KEY:?}"
: "${TARGET_ENDPOINT:?}" "${TARGET_REGION:?}" "${TARGET_ACCESS_KEY:?}" "${TARGET_SECRET_KEY:?}"
: "${TARGET_BUCKET:?}" "${PREFIX:?}" "${CRYPT_PASSWORD:?}" "${CRYPT_PASSWORD2:?}"

# --ca-cert replaces the system roots: the cloud needs those, the store its CA.
bundle="${TMPDIR:-/tmp}/ca-bundle.pem"
cat "${SYSTEM_CA:-/etc/ssl/certs/ca-certificates.crt}" "${STORE_CA:-/etc/ohmlab/store-ca/ca.crt}" >"$bundle"

export RCLONE_CONFIG="${TMPDIR:-/tmp}/rclone.conf"
: >"$RCLONE_CONFIG" # empty: the remotes come from the environment
: "${RCLONE_CONFIG_STORE_TYPE:=s3}" "${RCLONE_CONFIG_TARGET_TYPE:=s3}"
export RCLONE_CONFIG_STORE_TYPE RCLONE_CONFIG_TARGET_TYPE
export RCLONE_CONFIG_STORE_PROVIDER=Other RCLONE_CONFIG_STORE_FORCE_PATH_STYLE=true
export RCLONE_CONFIG_STORE_ENDPOINT="$SOURCE_ENDPOINT" RCLONE_CONFIG_STORE_REGION="$SOURCE_REGION"
export RCLONE_CONFIG_STORE_ACCESS_KEY_ID="$SOURCE_ACCESS_KEY" RCLONE_CONFIG_STORE_SECRET_ACCESS_KEY="$SOURCE_SECRET_KEY"
export RCLONE_CONFIG_TARGET_PROVIDER="${TARGET_PROVIDER:-Other}" RCLONE_CONFIG_TARGET_NO_CHECK_BUCKET=true
export RCLONE_CONFIG_TARGET_ENDPOINT="$TARGET_ENDPOINT" RCLONE_CONFIG_TARGET_REGION="$TARGET_REGION"
export RCLONE_CONFIG_TARGET_ACCESS_KEY_ID="$TARGET_ACCESS_KEY" RCLONE_CONFIG_TARGET_SECRET_ACCESS_KEY="$TARGET_SECRET_KEY"
if [ -n "${STORAGE_CLASS:-}" ]; then export RCLONE_CONFIG_TARGET_STORAGE_CLASS="$STORAGE_CLASS"; fi
export RCLONE_CONFIG_CRYPT_TYPE=crypt RCLONE_CONFIG_CRYPT_REMOTE="target:$TARGET_BUCKET/$PREFIX"
export RCLONE_CONFIG_CRYPT_FILENAME_ENCRYPTION=standard RCLONE_CONFIG_CRYPT_DIRECTORY_NAME_ENCRYPTION=true
# Through stdin, so the passwords never reach a command line.
RCLONE_CONFIG_CRYPT_PASSWORD=$(printf '%s' "$CRYPT_PASSWORD" | rclone obscure -)
RCLONE_CONFIG_CRYPT_PASSWORD2=$(printf '%s' "$CRYPT_PASSWORD2" | rclone obscure -)
export RCLONE_CONFIG_CRYPT_PASSWORD RCLONE_CONFIG_CRYPT_PASSWORD2

# --update --use-server-modtime compares the listings' modification times: S3
# keeps rclone's own in metadata, which costs a request per object on both
# sides, every run. An object rewritten after its copy is newer, so it goes
# again; backups rewrite few.
set -- --ca-cert "$bundle" --transfers "${TRANSFERS:-2}" --checkers "${CHECKERS:-4}" --bwlimit "${BWLIMIT:-0}" \
  --update --use-server-modtime --stats 1m --stats-one-line --stats-log-level NOTICE
if [ "${DRY_RUN:-false}" = true ]; then set -- "$@" --dry-run; fi
# shellcheck disable=SC2086 # EXTRA_ARGS is a list of flags
set -- "$@" ${EXTRA_ARGS:-}

# count <rclone size --json output>: its object count.
count() { printf '%s' "$1" | sed -n 's/.*"count":\([0-9][0-9]*\).*/\1/p'; }

failed=""
for b in $BUCKETS; do
  # rclone exits 3 for a directory, here a bucket, that does not exist. Only
  # stdout is the size. A listed bucket existed once: missing, the store lost it.
  if src=$(rclone size --json "$@" "store:$b"); then
    :
  else
    rc=$?
    if [ "$rc" -eq 3 ]; then
      echo "replica: bucket $b is missing at the source: failed (the store lost it, or its claim went without leaving the list)" >&2
    else
      echo "replica: bucket $b: listing the source failed (exit $rc)" >&2
    fi
    failed="$failed $b"
    continue
  fi
  if dst=$(rclone size --json "$@" "crypt:$b"); then
    :
  else
    rc=$?
    if [ "$rc" -eq 3 ]; then
      dst='{"count":0}'
    else
      echo "replica: bucket $b: listing the replica failed (exit $rc)" >&2
      failed="$failed $b"
      continue
    fi
  fi
  # A store rebuilt or refilled after a lost volume holds a fraction of what
  # its producers pruned over weeks: deleting more than half of the replica is
  # refused, so its current objects never become delete markers.
  s=$(count "$src") d=$(count "$dst")
  if [ "${ALLOW_SHRINK:-false}" != true ] && [ $((${s:-0} * 2)) -lt "${d:-0}" ]; then
    echo "replica: bucket $b holds ${s:-0} object(s) in the store and ${d:-0} in the replica: refusing to delete more than half of the replica (a store rebuilt or refilled; replica.allowShrink lets it through)" >&2
    failed="$failed $b"
    continue
  fi
  echo "replica: bucket $b" >&2
  if ! rclone sync "$@" "store:$b" "crypt:$b"; then
    failed="$failed $b"
  fi
done

if [ -n "$failed" ]; then
  echo "replica: failed:$failed" >&2
  exit 1
fi
echo "replica: done" >&2
