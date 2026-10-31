#!/bin/bash
#
# ohmlab Management Script
# ========================
# This script provides a unified interface for managing an ohmlab platform's infrastructure
# and GitOps-based Kubernetes deployments.
#
# Infrastructure (Ansible):
#   - Gateway setup (HAProxy, WireGuard, PiHole)
#   - K3s cluster deployment
#
# Applications (GitOps via ArgoCD):
#   - Core services (Longhorn, Cert-Manager, Vault, ArgoCD)
#   - Platform services (Keycloak, Gitea, Harbor, etc.)
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_PATH="$(cd -- "$(dirname "$0")" >/dev/null 2>&1; pwd -P)"

# Gateway API CRDs applied at bootstrap when missing. Experimental channel:
# TLSRoute (teleport passthrough) + traefik's kubernetesGateway
# experimentalChannel require it. Keep in sync with what the live cluster
# runs (bundle-version annotation).
GATEWAY_API_VERSION="v1.6.3"

# Defaults
export ANSIBLE_CONFIG="$SCRIPT_PATH/ansible/ansible.cfg"
FETCH_KUBECONFIG="false"
PLAYBOOK=""
TAGS="all"
DECRYPT="false"
ENCRYPT="false"
UPDATE="false"
BOOTSTRAP_INSTANCE=""
INSTANCES_DIR="$SCRIPT_PATH/argo-cd/instances"
YES="false"

# =============================================================================
# Helper Functions
# =============================================================================

log() {
  echo -e "${BLUE}[ohmlab]${NC} $*"
}

warn() {
  echo -e "${YELLOW}[ohmlab]${NC} $*" >&2
}

error() {
  echo -e "${RED}[ohmlab]${NC} $*" >&2
}

success() {
  echo -e "${GREEN}[ohmlab]${NC} $*"
}

confirm() {
  local message="$1"
  if [[ "$YES" == "true" ]]; then
    return 0
  fi
  echo -e "${YELLOW}[ohmlab]${NC} $message (Y/n)"
  read -r ANSWER
  if [[ "$ANSWER" =~ ^[Nn] ]]; then
    return 1
  fi
  return 0
}

# =============================================================================
# Help
# =============================================================================

print_help() {
  cat << 'EOF'

ohmlab Management Script
========================

This script manages an ohmlab platform's infrastructure (via Ansible) and Kubernetes
applications (via GitOps/ArgoCD).

USAGE:
  ./run.sh [OPTIONS]

INFRASTRUCTURE (Ansible):
  -p <playbook>   Run ansible playbook for infrastructure setup.
                  Example: ./run.sh -p ./ansible/install.yml
                  The inventory is ./ansible/inventory, or the directory in
                  ANSIBLE_INVENTORY (laid out like ./ansible/inventory-example)
                  for an inventory kept in a repository of its own:
                    ANSIBLE_INVENTORY=../mylab/ansible/inventory ./run.sh -p ./ansible/install.yml

  -t <tags>       Tags to run with playbook, default is 'all'.
                  Multiple tags: -t 'gateway,k3s'
                  Available tags:
                    - gateway       : Setup gateway (HAProxy, WireGuard, PiHole)
                    - k3s           : Deploy K3s cluster
                    - k3s-deploy    : Deploy K3s only
                    - k3s-destroy   : Destroy K3s cluster
                    - os-upgrade    : Upgrade OS on all hosts
                    - dist-upgrade  : Debian major version upgrade (e.g. bookworm→trixie)

  -k              Fetch kubeconfig from cluster and configure local kubectl.
                  Kubeconfig saved to: ~/.kube/config.d/<name>, and a
                  cluster, user and context <name> are added to
                  ~/.kube/config (KUBE_CONTEXT_NAME, default 'homelab').

  -u              Update ansible galaxy collections before running playbook.

GITOPS (ArgoCD):
  -b <instance>   Bootstrap (or upgrade) the ohmlab release for the given
                  instance. Installs core ArgoCD + the root `manager`
                  ApplicationSet + the `admin-core` / `admin-tenant`
                  AppProjects. The manager then discovers every instance under:
                    ./argo-cd/instances/*/
                  and renders one Application per instance pointing at the
                  `instance-manager` chart (which fans out into per-scope
                  AppSets reading `core.yaml` and `tenant.yaml`).
                  Per-app values are read from:
                    ./argo-cd/instances/<instance>/values/{core,tenant}/<app>.yaml
                  Example: ./run.sh -b mylab

  -i <dir>        Directory holding the instance folders, for an instance kept
                  in a repository of its own (default: ./argo-cd/instances).
                  Example: ./run.sh -b mylab -i ../mylab/argo-cd/instances

                  An optional admin password may be provided via the
                  ARGOCD_ADMIN_PASSWORD environment variable. The script will
                  bcrypt-hash it locally (requires `htpasswd` from
                  apache2-utils, or `python3` with bcrypt) and inject it into
                  the chart.
                  If unset, the ArgoCD chart auto-generates an admin password
                  and stores it in the `argocd-initial-admin-secret` Secret;
                  the script will print it at the end of the bootstrap.

SECRETS (Sops):
  -d              Decrypt all *.enc.yaml files to *.dec.yaml in ./argo-cd

  -e              Encrypt all *.dec.yaml files to *.enc.yaml in ./argo-cd

GENERAL:
  -y              Skip all confirmation prompts (non-interactive mode).

  -h              Print this help message.

EXAMPLES:

  # Setup infrastructure
  ./run.sh -p ./ansible/install.yml -t gateway    # Deploy gateway only
  ./run.sh -p ./ansible/install.yml -t k3s        # Deploy K3s cluster
  ./run.sh -p ./ansible/install.yml -u -k         # Full infra + fetch kubeconfig

  # Bootstrap GitOps
  ./run.sh -b mylab                                # Bootstrap the mylab instance

  # Secrets management
  ./run.sh -d                                      # Decrypt secrets
  ./run.sh -e                                      # Encrypt secrets

  # Destroy cluster
  ./run.sh -p ./ansible/install.yml -t k3s-destroy

EOF
}

# =============================================================================
# Sops Functions
# =============================================================================

# Fail-safe sops wrapper: write to a temp file and only move it over the
# target on success. The naive `sops -d "$f" > target` truncates the target
# BEFORE sops runs — a missing key then leaves a 0-byte file behind, and a
# later encrypt pass would happily encrypt that emptiness over the good copy.
_sops_convert() {
  local mode="$1" src="$2" dst="$3"
  local tmp
  tmp="$(mktemp "$dst.XXXXXX")"
  if sops "$mode" "$src" > "$tmp"; then
    mv "$tmp" "$dst"
    log "  $src -> $dst"
    return 0
  fi
  rm -f "$tmp"
  error "  FAILED: $src"
  return 1
}

decrypt_secrets() {
  log "Decrypting secrets with Sops..."
  local failures=0 f
  while IFS= read -r -d '' f; do
    _sops_convert -d "$f" "${f%.enc.yaml}.dec.yaml" || failures=$((failures + 1))
  done < <(find ./argo-cd -name '*.enc.yaml' -print0)
  if [[ "$failures" -gt 0 ]]; then
    error "$failures file(s) failed to decrypt"
    exit 1
  fi
  success "Secrets decrypted successfully"
}

encrypt_secrets() {
  log "Encrypting secrets with Sops..."
  local failures=0 f
  while IFS= read -r -d '' f; do
    # Never encrypt an empty file over an existing good .enc.yaml.
    if [[ ! -s "$f" ]]; then
      error "  SKIPPED (empty file): $f"
      failures=$((failures + 1))
      continue
    fi
    _sops_convert -e "$f" "${f%.dec.yaml}.enc.yaml" || failures=$((failures + 1))
  done < <(find ./argo-cd -name '*.dec.yaml' -print0)
  if [[ "$failures" -gt 0 ]]; then
    error "$failures file(s) failed to encrypt"
    exit 1
  fi
  success "Secrets encrypted successfully"
}

# =============================================================================
# Ansible Functions
# =============================================================================

update_collections() {
  log "Updating Ansible collections..."
  ansible-galaxy collection install \
    -r "$SCRIPT_PATH/ansible/collections/requirements.yml" \
    --upgrade
  success "Ansible collections updated"
}

run_playbook() {
  local playbook="$1"
  local tags="$2"

  playbook="$(readlink -f "$playbook")"
  ANSIBLE_CONFIG="$(dirname "$playbook")/ansible.cfg"
  export ANSIBLE_CONFIG

  # Destructive tags need an explicit confirmation (a single mistyped -t must
  # never wipe the cluster without a prompt). -y skips this, like everywhere else.
  # Not confirm(): that helper treats a bare Enter as yes, which is the wrong
  # default for wiping a cluster — require a literal "yes".
  if [[ ",$tags," == *",k3s-destroy,"* && "$YES" != "true" ]]; then
    warn "Tag 'k3s-destroy' will REMOVE k3s (binaries, data, /var/lib/rancher) from every node."
    echo -e "${YELLOW}[ohmlab]${NC} Type 'yes' to destroy the cluster: "
    read -r ANSWER
    if [[ "$ANSWER" != "yes" ]]; then
      error "Aborted."
      exit 1
    fi
  fi

  log "Running Ansible playbook: $playbook"
  log "Tags: $tags"

  # Determine kubeconfig path
  local kubeconfig="${KUBECONFIG_PATH:-${KUBECONFIG:-$HOME/.kube/config}}"

  ansible-playbook "$playbook" \
    --tags "$tags" \
    -e "K8S_AUTH_KUBECONFIG=$kubeconfig"
}

fetch_kubeconfig() {
  log "Fetching kubeconfig from cluster..."

  if ! command -v kubectl &> /dev/null; then
    error "kubectl is not installed"
    exit 1
  fi

  local gateway_ip
  local master_ip
  local user
  # The inventory Ansible itself reads: ANSIBLE_INVENTORY wins over ansible.cfg.
  local inventory="${ANSIBLE_INVENTORY:-$SCRIPT_PATH/ansible/inventory}"
  # The name of the cluster, user and context in the local kubeconfig.
  local name="${KUBE_CONTEXT_NAME:-homelab}"

  gateway_ip=$(yq '[.gateway.hosts[][]][0]' "$inventory/hosts.yml")
  master_ip=$(yq '[.k3s.children.masters.hosts[][]][0]' "$inventory/hosts.yml")
  user=$(yq '.ansible_user' "$inventory/group_vars/all.yml")

  mkdir -p "$HOME/.kube/config.d"
  scp "$user@$master_ip:/etc/rancher/k3s/k3s.yaml" "$HOME/.kube/config.d/$name"
  chmod 600 "$HOME/.kube/config.d/$name"

  # Point the API server at the gateway (HAProxy fronts :6443) and rename
  # the default k3s identifiers so several clusters can coexist locally.
  local tmp_cfg="$HOME/.kube/config.d/$name"
  yq -i '.clusters[0].cluster.server = "https://'"$gateway_ip"':6443"' "$tmp_cfg"
  NAME="$name" yq -i '.clusters[0].name = strenv(NAME)
         | .users[0].name = strenv(NAME)
         | .contexts[0].name = strenv(NAME)
         | .contexts[0].context.cluster = strenv(NAME)
         | .contexts[0].context.user = strenv(NAME)
         | .current-context = strenv(NAME)' "$tmp_cfg"

  # Merge into the main kubeconfig with kubectl itself: set-cluster /
  # set-credentials / set-context are create-or-update, so this works on a
  # brand-new machine too (the previous yq `select(.name == ...)`
  # edits silently did NOTHING when the entries didn't exist yet).
  if [[ -f "$HOME/.kube/config" ]]; then
    cp "$HOME/.kube/config" "$HOME/.kube/config.bak"
    log "Backed up ~/.kube/config to ~/.kube/config.bak"
  fi
  kubectl config set-cluster "$name" \
    --server="https://$gateway_ip:6443" \
    --certificate-authority=<(yq '.clusters[0].cluster.certificate-authority-data' "$tmp_cfg" | base64 -d) \
    --embed-certs=true >/dev/null
  kubectl config set-credentials "$name" \
    --client-certificate=<(yq '.users[0].user.client-certificate-data' "$tmp_cfg" | base64 -d) \
    --client-key=<(yq '.users[0].user.client-key-data' "$tmp_cfg" | base64 -d) \
    --embed-certs=true >/dev/null
  kubectl config set-context "$name" --cluster="$name" --user="$name" >/dev/null

  success "Kubeconfig fetched and configured"
  log "Context '$name' is available. Use: kubectl config use-context $name"
}

# =============================================================================
# GitOps Functions
# =============================================================================

check_kubectl() {
  if ! command -v kubectl &> /dev/null; then
    error "kubectl is not installed"
    exit 1
  fi

  local context
  context=$(kubectl config current-context 2>/dev/null || echo "none")
  if [[ "$context" == "none" ]]; then
    error "No kubectl context configured"
    exit 1
  fi

  if ! confirm "You are using kubectl context '$context', do you want to continue?"; then
    exit 1
  fi
}

check_helm() {
  if ! command -v helm &> /dev/null; then
    error "helm is not installed"
    exit 1
  fi
}

# Install the Gateway API CRDs (experimental channel) when missing. No
# stderr swallowing — a failed apply here must be visible, not discovered
# later as a cryptic HTTPRoute sync error.
ensure_gateway_api_crds() {
  if ! kubectl get crd tlsroutes.gateway.networking.k8s.io &>/dev/null; then
    log "Gateway API CRDs (experimental channel, $GATEWAY_API_VERSION) not found — installing..."
    kubectl apply --server-side -f \
      "https://github.com/kubernetes-sigs/gateway-api/releases/download/$GATEWAY_API_VERSION/experimental-install.yaml"
    kubectl wait --for=condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=60s
  fi
}

# Bcrypt-hash a plain string using whatever's available locally.
# Echoes the hash on stdout; non-zero exit on failure.
bcrypt_hash() {
  local plain="$1"
  if command -v htpasswd &> /dev/null; then
    htpasswd -nbBC 10 "" "$plain" | tr -d ':\n' | sed 's/$2y/$2a/'
  elif command -v python3 &> /dev/null && python3 -c 'import bcrypt' &> /dev/null; then
    python3 -c 'import bcrypt,sys; print(bcrypt.hashpw(sys.argv[1].encode(),bcrypt.gensalt(10)).decode())' "$plain"
  else
    error "Cannot bcrypt-hash password: install 'htpasswd' (apache2-utils) or 'python3-bcrypt'."
    return 1
  fi
}

# Print the auto-generated initial admin password (if the secret exists).
print_initial_admin_password() {
  local secret
  if ! secret=$(kubectl -n argocd-system get secret argocd-initial-admin-secret \
                  -o jsonpath='{.data.password}' 2>/dev/null); then
    return 0
  fi
  if [[ -z "$secret" ]]; then
    return 0
  fi
  log ""
  log "ArgoCD admin password (auto-generated):"
  log "  user:     admin"
  log "  password: $(echo "$secret" | base64 -d)"
  warn "Store this password and delete the secret once rotated:"
  warn "  kubectl -n argocd-system delete secret argocd-initial-admin-secret"
}

bootstrap_instance() {
  local instance="$1"
  local values_file="$INSTANCES_DIR/$instance/values/core/ohmlab.yaml"
  local instance_dir="$INSTANCES_DIR/$instance"

  # Folders prefixed with `_` are templates (e.g. `_example`) and are excluded
  # by the root manager AppSet. Refuse to bootstrap them.
  if [[ "$instance" == _* ]]; then
    error "Instance '$instance' starts with '_' and is treated as a template."
    error "Copy it to a real name first:  cp -r $INSTANCES_DIR/$instance $INSTANCES_DIR/<name>"
    exit 1
  fi

  if [[ ! -d "$instance_dir" ]]; then
    error "Instance '$instance' not found: $instance_dir"
    log "Available instances:"
    find "$INSTANCES_DIR/" -mindepth 1 -maxdepth 1 -type d \
      ! -name '_*' -exec basename {} \; 2>/dev/null | sed 's/^/  /' || echo "  (none)"
    exit 1
  fi

  if [[ ! -f "$instance_dir/instance.yaml" ]]; then
    error "Missing instance.yaml: $instance_dir/instance.yaml"
    exit 1
  fi

  if [[ ! -f "$values_file" ]]; then
    error "ohmlab values not found: $values_file"
    exit 1
  fi

  check_kubectl
  check_helm

  log "Bootstrapping ohmlab for instance: $instance"

  # The chart the self-managed `ohmlab` entry runs: the published chart at its
  # pinned version when the entry has a `catalog`, the local chart otherwise.
  local resolved chart_ref chart_version
  resolved=$("$SCRIPT_PATH/scripts/ohmlab-chart.sh" "$instance_dir" "$SCRIPT_PATH/utils/helm") || exit 1
  IFS=$'\t' read -r chart_ref chart_version <<<"$resolved"
  local chart_args=("$chart_ref")
  if [[ -n "$chart_version" ]]; then
    chart_args+=(--version "$chart_version")
    log "Using the published chart $chart_ref $chart_version"
  else
    log "Updating chart dependencies..."
    helm dependency update "$chart_ref" >/dev/null
  fi

  # Phase 1: If ArgoCD CRDs don't exist yet, install the chart with only
  # ArgoCD enabled (CRDs + core components). This solves the chicken-and-egg
  # where AppProject/Application/HTTPRoute resources need CRDs from the same release.
  if ! kubectl get crd applications.argoproj.io &>/dev/null; then
    log "ArgoCD CRDs not present — phase 1: installing ArgoCD components only..."

    # Gateway API CRDs first — the chart ships HTTPRoutes.
    ensure_gateway_api_crds

    helm upgrade --install ohmlab "${chart_args[@]}" \
      --namespace argocd-system \
      --create-namespace \
      --values "$values_file" \
      --set rootManager.enabled=false \
      --set projects.core.enabled=false \
      --set projects.tenant.enabled=false \
      --set gateway.enabled=false \
      --set vso.enabled=false \
      --wait \
      --timeout 10m

    log "Phase 1 complete — ArgoCD CRDs and components installed."
  fi

  # Upgrade path where ArgoCD was already present but Gateway API was not.
  ensure_gateway_api_crds

  local extra_args=()
  local admin_pw_values=""
  if [[ -n "${ARGOCD_ADMIN_PASSWORD:-}" ]]; then
    log "Bcrypt-hashing admin password from \$ARGOCD_ADMIN_PASSWORD..."
    local bcrypt
    bcrypt=$(bcrypt_hash "$ARGOCD_ADMIN_PASSWORD") || exit 1
    # A --set-string value is visible in `ps` output for the whole helm run —
    # pass the hash through a 0600 temp values file instead.
    admin_pw_values="$(mktemp)"
    # Path expanded NOW: the trap fires after this function has returned,
    # when the local variable no longer exists.
    # shellcheck disable=SC2064
    trap "rm -f '$admin_pw_values'" EXIT
    chmod 600 "$admin_pw_values"
    cat > "$admin_pw_values" << EOF
argo-cd:
  configs:
    secret:
      argocdServerAdminPassword: "$bcrypt"
EOF
    extra_args+=(--values "$admin_pw_values")
  fi

  # Phase 2: Full install/upgrade with all resources (AppProjects, root
  # Application, HTTPRoutes). CRDs are now present from phase 1 or prior run.
  log "Installing/upgrading ohmlab release..."
  helm upgrade --install ohmlab "${chart_args[@]}" \
    --namespace argocd-system \
    --create-namespace \
    --values "$values_file" \
    ${extra_args[@]+"${extra_args[@]}"} \
    --set vso.enabled=false \
    --force-conflicts \
    --wait \
    --timeout 10m

  log "Waiting for ArgoCD server to be ready..."
  kubectl rollout status deployment -n argocd-system \
    -l app.kubernetes.io/name=argocd-server --timeout=300s || true

  success "ohmlab bootstrap complete for instance: $instance"

  if [[ -z "${ARGOCD_ADMIN_PASSWORD:-}" ]]; then
    print_initial_admin_password
  fi
  log ""
  log "The root 'manager' ApplicationSet will now reconcile every instance"
  log "under argo-cd/instances/*/ of its values repository (one per-instance"
  log "Application per folder, each fanning out into core + tenant AppSets)."
  log ""
  log "Monitor progress:"
  log "  kubectl get applications,applicationsets -n argocd-system"
}

# =============================================================================
# Main
# =============================================================================

# Parse options
while getopts "hdekp:t:ub:i:y" flag; do
  case "${flag}" in
    d) DECRYPT="true" ;;
    e) ENCRYPT="true" ;;
    k) FETCH_KUBECONFIG="true" ;;
    p) PLAYBOOK="${OPTARG}" ;;
    t) TAGS="${OPTARG}" ;;
    u) UPDATE="true" ;;
    b) BOOTSTRAP_INSTANCE="${OPTARG}" ;;
    i) INSTANCES_DIR="${OPTARG%/}" ;;
    y) YES="true" ;;
    h)
      print_help
      exit 0
      ;;
    *)
      print_help
      exit 1
      ;;
  esac
done

# Execute requested operations

# Sops operations
if [[ "$DECRYPT" == "true" ]]; then
  decrypt_secrets
fi

if [[ "$ENCRYPT" == "true" ]]; then
  encrypt_secrets
fi

# Ansible operations
if [[ "$UPDATE" == "true" ]]; then
  update_collections
fi

if [[ -n "$PLAYBOOK" ]]; then
  run_playbook "$PLAYBOOK" "$TAGS"
fi

if [[ "$FETCH_KUBECONFIG" == "true" ]]; then
  fetch_kubeconfig
fi

# GitOps operations
if [[ -n "$BOOTSTRAP_INSTANCE" ]]; then
  bootstrap_instance "$BOOTSTRAP_INSTANCE"
fi

# Show help if no action was taken
if [[ "$DECRYPT" == "false" && "$ENCRYPT" == "false" && "$UPDATE" == "false" && \
      -z "$PLAYBOOK" && "$FETCH_KUBECONFIG" == "false" && -z "$BOOTSTRAP_INSTANCE" ]]; then
  print_help
fi
