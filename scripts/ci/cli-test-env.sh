# shellcheck shell=bash
# Sourced by the Go test job (working directory cli/) to prepare what the
# suites expect, in the shell that then runs `go test`:
#
#   KUBEBUILDER_ASSETS      an API server and etcd for envtest, fetched by the
#                           module's own setup-envtest tool
#   OHMLAB_REQUIRE_ENVTEST  a missing API server fails the suite instead of
#                           skipping it, so CI cannot go green on nothing
#   OHMLAB_TEST_VAULT_*     a Vault dev server, for the tests that check what
#                           the in-memory KV only imitates (compare-and-set,
#                           the controller's policy, parity with the CLI engine)
#
# Vault comes from HashiCorp's release, verified against its published
# checksums, at the version cli/Dockerfile pins for the toolbox image: one
# place to bump, and no registry to be rate limited by.
#
# Locally: `source ../scripts/ci/cli-test-env.sh` from cli/, then go test.

_ohmlab_fail() { echo "cli-test-env: $*" >&2; return 1; }

KUBEBUILDER_ASSETS="$(go tool setup-envtest use -p path)" || { _ohmlab_fail "setup-envtest failed"; return 1; }
export KUBEBUILDER_ASSETS
export OHMLAB_REQUIRE_ENVTEST=1

_ohmlab_version="$(sed -n 's/^ARG VAULT_VERSION=//p' Dockerfile)"
[ -n "$_ohmlab_version" ] || { _ohmlab_fail "no VAULT_VERSION in Dockerfile"; return 1; }
_ohmlab_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in x86_64) _ohmlab_arch=amd64 ;; aarch64 | arm64) _ohmlab_arch=arm64 ;; *) _ohmlab_fail "unsupported architecture $(uname -m)"; return 1 ;; esac

_ohmlab_dir="$(mktemp -d)"
_ohmlab_zip="vault_${_ohmlab_version}_${_ohmlab_os}_${_ohmlab_arch}.zip"
_ohmlab_url="https://releases.hashicorp.com/vault/${_ohmlab_version}"
curl -fsSL "$_ohmlab_url/$_ohmlab_zip" -o "$_ohmlab_dir/$_ohmlab_zip" \
  && curl -fsSL "$_ohmlab_url/vault_${_ohmlab_version}_SHA256SUMS" -o "$_ohmlab_dir/SUMS" \
  || { _ohmlab_fail "cannot download Vault $_ohmlab_version"; return 1; }
(cd "$_ohmlab_dir" && grep " ${_ohmlab_zip}\$" SUMS | { sha256sum -c - 2>/dev/null || shasum -a 256 -c -; }) >/dev/null \
  || { _ohmlab_fail "Vault $_ohmlab_version does not match its checksum"; return 1; }
(cd "$_ohmlab_dir" && unzip -oq "$_ohmlab_zip" vault) || { _ohmlab_fail "cannot unpack Vault"; return 1; }

"$_ohmlab_dir/vault" server -dev -dev-root-token-id=root -dev-listen-address=127.0.0.1:18200 >"$_ohmlab_dir/vault.log" 2>&1 &
export OHMLAB_TEST_VAULT_ADDR=http://127.0.0.1:18200 OHMLAB_TEST_VAULT_TOKEN=root
for _ohmlab_i in $(seq 1 50); do
  curl -fsS "$OHMLAB_TEST_VAULT_ADDR/v1/sys/health" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$OHMLAB_TEST_VAULT_ADDR/v1/sys/health" >/dev/null 2>&1 || { _ohmlab_fail "the Vault dev server did not start: $(tail -3 "$_ohmlab_dir/vault.log")"; return 1; }
echo "cli-test-env: envtest at $KUBEBUILDER_ASSETS, Vault $_ohmlab_version dev server at $OHMLAB_TEST_VAULT_ADDR"
unset -f _ohmlab_fail
