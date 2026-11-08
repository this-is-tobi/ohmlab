#!/usr/bin/env bash
# run.sh's guards, with fake tools first on PATH (nothing reaches a host or a
# cluster): k3s-destroy asks for the word yes however the tags select it, and
# the k3s-users play's kubeconfig copy never outlives run.sh, even when -b,
# which sets an EXIT trap of its own, runs after -p.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

check() { # label expected actual
  if [ "$3" = "$2" ]; then echo "ok   $1"; else
    echo "FAIL $1"; echo "     expected: $2"; echo "     actual:   $3"; fail=1
  fi
}

mkdir -p "$work/bin" "$work/instances/lab/values/core"
# ansible-playbook records the kubeconfig the k3s-users play would get.
cat >"$work/bin/ansible-playbook" <<'EOF'
#!/bin/sh
for a; do case "$a" in K8S_AUTH_KUBECONFIG=*) echo "${a#K8S_AUTH_KUBECONFIG=}" >"$PLAYBOOK_LOG" ;; esac; done
EOF
# kubectl: a context to copy, a current one, and every CRD present.
cat >"$work/bin/kubectl" <<'EOF'
#!/bin/sh
case "$*" in
  "config view"*) printf 'apiVersion: v1\nkind: Config\ncurrent-context: lab\n' ;;
  "config current-context") echo lab ;;
esac
EOF
printf '#!/bin/sh\n' >"$work/bin/helm"
printf '#!/bin/sh\necho ":\\$2y\\$10\\$fake"\n' >"$work/bin/htpasswd"
chmod +x "$work/bin/"*
# An instance for -b, with no ohmlab entry: the local chart.
printf 'apps: []\n' >"$work/instances/lab/core.yaml"
printf 'instance:\n  name: lab\n' >"$work/instances/lab/instance.yaml"
printf '{}\n' >"$work/instances/lab/values/core/ohmlab.yaml"

# runsh <stdin> [VAR=value...] -- <run.sh arguments>: run.sh's exit code.
runsh() {
  local in="$1" vars=()
  shift
  while [ "$1" != "--" ]; do vars+=("$1"); shift; done
  shift
  rm -f "$work/playbook.log"
  printf '%b' "$in" | (cd "$ROOT" && env PATH="$work/bin:$PATH" KUBECONFIG="$work/none" PLAYBOOK_LOG="$work/playbook.log" \
    ANSIBLE_RUN_TAGS= ${vars[@]+"${vars[@]}"} ./run.sh "$@") >"$work/out" 2>&1
}

# ran: whether ansible-playbook ran.
ran() { [ -f "$work/playbook.log" ] && echo yes || echo no; }

# copy: whether the play's kubeconfig copy outlived run.sh.
copy() {
  [ -f "$work/playbook.log" ] || { echo "no run"; return; }
  [ -e "$(dirname "$(cat "$work/playbook.log")")" ] && echo left || echo removed
}

for tags in "k3s-destroy" "gateway,k3s-destroy" "gateway, k3s-destroy" "never" "all,never"; do
  runsh 'y\n' -- -p ansible/install.yml -t "$tags"
  check "-t '$tags', answer y: aborted before the playbook" "1 no" "$? $(ran)"
done
runsh 'y\n' ANSIBLE_RUN_TAGS=k3s-destroy -- -p ansible/install.yml -t gateway
check "ANSIBLE_RUN_TAGS=k3s-destroy, answer y: aborted before the playbook" "1 no" "$? $(ran)"
runsh 'yes\n' -- -p ansible/install.yml -t never
check "-t never, answer yes: the playbook runs" "0 yes" "$? $(ran)"
runsh '' -- -p ansible/install.yml -t gateway
check "-t gateway: the playbook runs, without a question" "0 yes" "$? $(ran)"
check "-p: the kubeconfig copy is removed" "removed" "$(copy)"
runsh '' ARGOCD_ADMIN_PASSWORD=throwaway -- -y -p ansible/install.yml -t gateway -b lab -i "$work/instances"
check "-p then -b: run.sh succeeds" "0" "$?"
check "-p then -b: the kubeconfig copy is removed" "removed" "$(copy)"

exit "$fail"
