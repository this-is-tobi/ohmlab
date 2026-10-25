# Cheat Sheet

## ohmlab CLI

The Go CLI in [utils/](../utils/) ships operational commands (built as
`ghcr.io/this-is-tobi/homelab/ohmlab`, or `go run .` from `utils/`):

```sh
# Read-only platform health sweep: nodes, ArgoCD apps, VSO secret sync,
# Vault HA state, crashlooping pods, Kyverno violations.
# Exits non-zero when problems are found (cron/CI friendly).
ohmlab check

# Initialize/repair declarative Vault secrets (idempotent). The ohmlab
# controller does this continuously; run it by hand only to repair Vault
# without the controller, e.g. after a recovery
ohmlab vault secret --config secrets.json --auth-method token

# Repair Vault identity groups/aliases by hand (OIDC group mapping). The
# platform applies them through bank-vaults (ohmlab.vault.identity in the
# vault-operator values); this is only for a manual repair.
ohmlab vault identity --config identity.json

# Discover the active Vault raft leader
ohmlab vault leader

# Check ohmlab.fr objects (VaultSeed) without a cluster: files, or a rendered
# chart on standard input
helm template vault-operator argo-cd/apps/vault-operator -f argo-cd/instances/<instance>/values/core/vault-operator.yaml \
  --set ohmlab.vault.seeds.enabled=true --set ohmlab.vault.seeds.requireCRD=false | ohmlab validate -
```

`ohmlab check` uses the in-cluster ServiceAccount when available and falls
back to `$KUBECONFIG` / `~/.kube/config` (static certs or token).

## Network

```sh
# Find ips on local subnet
sudo nmap -sN 192.168.1.1/24

# Traefik dashboard (never exposed publicly)
kubectl -n traefik port-forward deploy/traefik 8080:8080
# → http://localhost:8080/dashboard/

# Test a hostname through the traefik path during the ingress migration
curl -vk --resolve <host>:8443:<node-ip> https://<host>:8443/
```

## Disk

```sh
# List disks
lsblk -f

# umount /dev/sda1
sudo wipefs -a /dev/sda

# use ext4 filesystem for /dev/sda
sudo mkfs.ext4 /dev/sda

# mount volume
sudo mkdir /storage01
sudo mount /dev/sda /storage01

VAR_UUID=$(lsblk -n -o UUID /dev/sda)
sudo su -c "echo 'UUID=$VAR_UUID  /storage01       ext4    defaults        0       2' >> /etc/fstab"
```

## System

```sh
# Restart host
sudo reboot
```

## Secrets

```sh
# Generate strong secret with openssl
openssl rand -base64 36
```

## Kubernetes

```sh
# Get pod by node
kubectl get pods --all-namespaces -o wide --field-selector spec.nodeName=<node>

# Get node labels
kubectl get nodes --show-labels

# Create and apply secret from file
kubectl create secret -n <namespace> generic <secret_name> \
  --from-file=<file_name>.yaml \
  --dry-run=client \
  -o yaml \
  | kubectl apply -f -

# Read secret
kubectl -n <namespace> get secret <secret_name> -o jsonpath="{.data.<data_field>}" | base64 --decode

# Forward port
kubectl port-forward -n <namespace> svc/<service_name> <host_port>:<service_port>
kubectl port-forward -n <namespace> pod/<pod_name> <host_port>:<pod_port>
```

## RustFS

```sh
# Generate S3 format secret key (Ruby)
SecureRandom.urlsafe_base64(30)

# Use the AWS CLI against the RustFS S3 endpoint
aws --endpoint-url https://s3.domain.com \
  --profile homelab \
  s3 ls
```

## Local registry

```sh
# Login to registry
docker login <registry_domain>

# List images in a registry
curl -X GET https://<registry_domain>/v2/_catalog

# Push image to registry
docker tag <image_name>:<image_tag> <registry_domain>/<image_name>:<image_tag>
docker push <registry_domain>/<image_name>:<image_tag>

# You can now pull the image
docker pull <registry_domain>/<image_name>:<image_tag>

# ---
# Build multiarch images
docker login <registry_domain>

## Create buildx namespace
docker buildx create --use --name <buildx_namespace>

## Inspect buildx namespace
docker buildx inspect --bootstrap <buildx_namespace>

## Upload image to private registry specifying options (e.g platform linux/arm64)
docker buildx build \
  --platform linux/arm64,linux/amd64 \
  --tag <registry_domain>/<image_name>:<image_tag> \
  --push \
  <dockerfile_folder>
```
