#!/bin/sh
# Copies each replicated bucket of this store to the cloud replica, encrypted
# on the client side: rclone's crypt remote over <bucket>/<prefix> of the
# cloud bucket, so the provider holds ciphertext and encrypted names only.
#
# A bucket that lists nothing at the source while the replica holds some of it
# fails, so a store rebuilt empty never turns the replica's objects into
# delete markers. A listed bucket missing at the source is skipped. Any failed
# bucket fails the run; the others still copy.
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

set -- --ca-cert "$bundle" --transfers "${TRANSFERS:-2}" --checkers "${CHECKERS:-4}" --bwlimit "${BWLIMIT:-0}" \
  --stats 1m --stats-one-line --stats-log-level NOTICE
if [ "${DRY_RUN:-false}" = true ]; then set -- "$@" --dry-run; fi
# shellcheck disable=SC2086 # EXTRA_ARGS is a list of flags
set -- "$@" ${EXTRA_ARGS:-}

failed=""
for b in $BUCKETS; do
  # rclone exits 3 for a directory, here a bucket, that does not exist. Only
  # stdout is the listing.
  if listing=$(rclone lsf --max-depth 1 "$@" "store:$b"); then
    :
  else
    rc=$?
    if [ "$rc" -eq 3 ]; then
      echo "replica: bucket $b is missing at the source: skipped" >&2
    else
      echo "replica: bucket $b: listing the source failed (exit $rc)" >&2
      failed="$failed $b"
    fi
    continue
  fi
  if [ -z "$listing" ] && [ -n "$(rclone lsf --max-depth 1 "$@" "crypt:$b" 2>/dev/null | head -n 1)" ]; then
    echo "replica: bucket $b is empty at the source and not in the replica: refusing to empty the replica" >&2
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
