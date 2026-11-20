#!/usr/bin/env bash
# Tests the rustfs chart's replica script (argo-cd/apps/rustfs/files/replicate.sh)
# in the chart's own rclone image, as the job runs it (uid 65534, read-only
# root). Alias remotes into a Docker volume stand in for the store and the
# cloud: what it copies and encrypts, what it refuses, and that no secret
# reaches its output.
#
#   test-rustfs-replica-script.sh
#
# Needs docker, openssl and yq (mikefarah v4).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART="$ROOT/argo-cd/apps/rustfs"
IMAGE="$(yq '.replica.image.repository + ":" + .replica.image.tag' "$CHART/values.yaml")"
VOL="ohmlab-replica-test-$$"
work=$(mktemp -d)
trap 'docker volume rm -f "$VOL" >/dev/null 2>&1; rm -rf "$work"' EXIT
mkdir -p "$work/tmp" "$work/scripts"
chmod 0777 "$work/tmp"
cp "$CHART/files/replicate.sh" "$work/scripts/replicate.sh" 2>/dev/null || : >"$work/scripts/replicate.sh"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj "/CN=store" \
  -keyout /dev/null -out "$work/scripts/store-ca.crt" 2>/dev/null
STORE_CA_LINE=$(sed -n 2p "$work/scripts/store-ca.crt")

# The run's secrets, looked for in every output.
SRC_SECRET=$(openssl rand -hex 20) TGT_SECRET=$(openssl rand -hex 20)
CRYPT1=$(openssl rand -hex 24) CRYPT2=$(openssl rand -hex 24)

failures=0
check() { # <label> <expected> <actual>
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; failures=$((failures + 1))
  fi
}

# as_root <sh>: a shell in the image as root over the volume, to set up and look.
as_root() { docker run --rm -v "$VOL:/data" --entrypoint sh "$IMAGE" -c "$1"; }

# fresh: an empty volume with buckets b1 and b2 in the store, owned by the job's user.
fresh() {
  docker volume rm -f "$VOL" >/dev/null 2>&1
  docker volume create "$VOL" >/dev/null
  as_root 'mkdir -p /data/store/b1/dir /data/store/b2/wal /data/target/replica &&
    echo one >/data/store/b1/a.txt && echo two >/data/store/b1/dir/x.txt && echo wal >/data/store/b2/wal/000001 &&
    chown -R 65534:65534 /data'
}

# replicate [VAR=value...]: runs the script as the job does; its output goes
# to $work/out, its exit code to $rc.
replicate() {
  local env=(
    -e RCLONE_CONFIG_STORE_TYPE=alias -e RCLONE_CONFIG_STORE_REMOTE=/data/store
    -e RCLONE_CONFIG_TARGET_TYPE=alias -e RCLONE_CONFIG_TARGET_REMOTE=/data/target
    -e SOURCE_ENDPOINT=https://store.invalid:9000 -e SOURCE_REGION=us-east-1
    -e SOURCE_ACCESS_KEY=SOURCEKEY -e "SOURCE_SECRET_KEY=$SRC_SECRET"
    -e TARGET_PROVIDER=Other -e TARGET_ENDPOINT=https://cloud.invalid -e TARGET_REGION=fr-par
    -e TARGET_ACCESS_KEY=TARGETKEY -e "TARGET_SECRET_KEY=$TGT_SECRET"
    -e TARGET_BUCKET=replica -e PREFIX=homelab
    -e "CRYPT_PASSWORD=$CRYPT1" -e "CRYPT_PASSWORD2=$CRYPT2"
    -e STORE_CA=/scripts/store-ca.crt -e BWLIMIT=0 -e "BUCKETS=b1 b2"
  )
  local a
  for a in "$@"; do env+=(-e "$a"); done
  docker run --rm --user 65534:65534 --read-only -v "$work/tmp:/tmp" -v "$VOL:/data" -v "$work/scripts:/scripts:ro" \
    "${env[@]}" --entrypoint sh "$IMAGE" /scripts/replicate.sh >"$work/out" 2>&1
  rc=$?
  cat "$work/out" >>"$work/all-out"
}

# decrypted <bucket>: the replica's names of a bucket, decrypted with the run's passwords.
decrypted() {
  docker run --rm -v "$VOL:/data" -e "P1=$CRYPT1" -e "P2=$CRYPT2" -e "B=$1" --entrypoint sh "$IMAGE" -c '
    export RCLONE_CONFIG_CRYPT_TYPE=crypt RCLONE_CONFIG_CRYPT_REMOTE=/data/target/replica/homelab
    RCLONE_CONFIG_CRYPT_PASSWORD=$(printf %s "$P1" | rclone obscure -) RCLONE_CONFIG_CRYPT_PASSWORD2=$(printf %s "$P2" | rclone obscure -)
    export RCLONE_CONFIG_CRYPT_PASSWORD RCLONE_CONFIG_CRYPT_PASSWORD2
    rclone lsf -R --files-only "crypt:$B" 2>/dev/null | sort | tr "\n" " "'
}

fresh
replicate
check "every listed bucket is copied: exit" "0" "$rc"
check "b1 decrypts to its objects" "a.txt dir/x.txt " "$(decrypted b1)"
check "b2 decrypts to its objects" "wal/000001 " "$(decrypted b2)"
check "the replica holds no plaintext name" "" "$(as_root 'cd /data/target && find . | grep -E "a\.txt|x\.txt|000001|/b1|/b2|/dir|/wal" || true')"
check "the CA bundle keeps the system roots and adds the store's" "yes yes" \
  "$([ "$(grep -c 'BEGIN CERTIFICATE' "$work/tmp/ca-bundle.pem" 2>/dev/null || echo 0)" -gt 1 ] && echo yes || echo no) $(grep -qF "$STORE_CA_LINE" "$work/tmp/ca-bundle.pem" 2>/dev/null && echo yes || echo no)"

# A store rebuilt empty: the replica keeps every object, and the run fails.
as_root 'rm -rf /data/store/b1/* && true'
replicate
check "an empty source does not empty the target: exit" "nonzero" "$([ "$rc" -ne 0 ] && echo nonzero || echo 0)"
check "an empty source does not empty the target: the replica" "a.txt dir/x.txt " "$(decrypted b1)"
check "an empty source does not empty the target: the log names it" "yes" "$(grep -q 'b1' "$work/out" && grep -qi 'empty' "$work/out" && echo yes || echo no)"

fresh
replicate "BUCKETS=b1 gone"
check "a missing source bucket is skipped: exit" "0" "$rc"
check "a missing source bucket is skipped: the warning names it" "yes" "$(grep -q 'gone' "$work/out" && echo yes || echo no)"
check "a missing source bucket is skipped: the others copy" "a.txt dir/x.txt " "$(decrypted b1)"

fresh
as_root 'mkdir -p /data/store/bad && echo secret >/data/store/bad/locked && chmod 000 /data/store/bad/locked && chown -R 65534:65534 /data/store/bad'
replicate "BUCKETS=b1 bad b2"
check "one failed bucket fails the run" "nonzero" "$([ "$rc" -ne 0 ] && echo nonzero || echo 0)"
check "the others still sync" "a.txt dir/x.txt |wal/000001 " "$(decrypted b1)|$(decrypted b2)"

fresh
replicate DRY_RUN=true
check "a dry run exits 0" "0" "$rc"
check "a dry run changes nothing" "" "$(as_root 'find /data/target/replica -mindepth 1 | head -1')"

leaks=""
for s in "$SRC_SECRET" "$TGT_SECRET" "$CRYPT1" "$CRYPT2"; do
  grep -qF -- "$s" "$work/all-out" && leaks="$leaks x"
done
check "no secret reaches the output" "" "$leaks"

exit "$failures"
