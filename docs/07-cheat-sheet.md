# Cheat Sheet

## ohmlab CLI

The Go CLI in [cli/](../cli/) ships operational commands. Take it from the [release archives](https://github.com/this-is-tobi/ohmlab/releases), run it from the image `ghcr.io/this-is-tobi/ohmlab/cli` (which also carries kubectl, helm and the vault CLI), or `go run .` from `cli/`:

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

# Find the in-cluster ports that do not speak TLS (read-only, through
# port-forwards). Exits non-zero when one is plaintext with nothing allowing it;
# --output json for the whole report, --all to list every port.
ohmlab audit tls --context <context>

# Fetch the cluster's kubeconfig from the first master (through Ansible and the
# inventory) and merge it into yours as a named context. --dry-run prints the
# Ansible command and the change, and runs nothing.
ohmlab kubeconfig --product <checkout of ohmlab> --name <context>

# Install the instance's bootstrap chart on its cluster: the Gateway API CRDs
# when they are missing, Argo CD alone on an empty cluster, then the whole
# chart. --dry-run prints the plan and the helm commands, and changes nothing.
ohmlab bootstrap [instance]

# Print the admin password Argo CD generated at the bootstrap, and nothing
# else, so it pipes.
ohmlab argocd password

# Run the product's playbook on the inventory's hosts (every tag but those
# that need care). Arguments after -- go to ansible-playbook. --dry-run prints
# the commands, and runs nothing.
ohmlab provision [--tags gateway,k3s] [--update] [-- --limit <host>]

# Remove k3s from every node (asks for the word yes), or upgrade the packages
# of every host, one at a time.
ohmlab provision destroy
ohmlab provision upgrade-os

# Optional Sops helpers: decrypt every *.enc.yaml under argo-cd/ to
# *.dec.yaml, or encrypt them back, never truncating a good file.
ohmlab sops decrypt
ohmlab sops encrypt
```

`ohmlab check` uses the in-cluster ServiceAccount when available and falls
back to `$KUBECONFIG` / `~/.kube/config` (static certs or token).

`ohmlab audit tls` loads the kubeconfig as kubectl does (exec plugins work). Metrics ports (named so, or on a Service named so, but not Loki's API port, which its chart names `http-metrics`) and DNS may stay plaintext, and so may the exceptions of the workspace file (below; `--config` for another), each with its reason:

```yaml
audit:
  tls:
    exceptions:
    - namespace: homepage
      service: homepage
      ports: [3000]   # every port of the Service when left out
      reason: Homepage serves HTTP only
```

An exception that allows nothing any more, such as one whose port speaks TLS now, is reported as stale. The command needs to list Services and EndpointSlices, to read `kube-system/cilium-config` (it reports whether Cilium encrypts the traffic between nodes) and to create `pods/portforward`.

`ohmlab kubeconfig` fetches `/etc/rancher/k3s/k3s.yaml` from the first master with `ansible --become` (adding the inventory's `group_vars/vault.yml` when it exists), into a temporary directory it removes afterwards. It merges the cluster, the user and a context, all called `--name`, into the file kubectl writes (`--kubeconfig`, else the one kubectl picks from `$KUBECONFIG`, else `~/.kube/config`), with the server set to the inventory's first `gateway` host on 6443, else its first master. An existing file is copied to `<file>.bak` first and keeps its current context. A context of that name that leads to another server is replaced only after a yes (`--yes` gives it). Ansible reads the product's `ansible/ansible.cfg`. The vault password file is the `vaultPasswordFile` setting (below), else the workspace's `ansible/.vault_password`. Ansible gets it through `ANSIBLE_VAULT_PASSWORD_FILE`, in place of the one `ansible.cfg` names.

`ohmlab bootstrap` installs an instance's `ohmlab` chart as its self-managed Application then runs it, through `helm` (Helm 4). It acts on the workspace's context (`contextName`, below), never on whichever context is current, and shows it with its server before asking. The chart is the published one the instance's `ohmlab` entry pins in `core.yaml` (its `catalog` and `targetRevision`), else the entry's `chartPath` (`charts/ohmlab` by default) in the product checkout, or the directory `--chart` names. The release name and namespace come from the entry too (`ohmlab` in `argocd-system` by default). On an empty cluster it installs the Gateway API CRDs (experimental channel), then Argo CD alone, then the whole chart; on a cluster already bootstrapped it upgrades the release. The Argo CD admin password comes from `--admin-password-file` (`-` reads it from standard input, which then needs `--yes`) or `ARGOCD_ADMIN_PASSWORD`. It reaches Helm as a bcrypt hash in a file only you can read, removed afterwards, from the first phase on, so Argo CD generates none. Without either, an installed Argo CD keeps the password it holds (Helm would otherwise drop it), and a new one generates one, which the command prints on a terminal only. `--dry-run` prints the plan and the helm commands, and changes nothing.

`ohmlab argocd password` prints the password Argo CD generated, from `argocd-initial-admin-secret` in the bootstrap release's namespace (`--namespace`, `argocd-system` by default), and nothing else, so it pipes. It uses the workspace's context too. Argo CD keeps that Secret until it is deleted, which is due once the password is changed.

`ohmlab provision` runs the product's playbook (`ansible/install.yml` of the product checkout, or `--playbook`) on the hosts of the workspace's inventory, with the product's `ansible/ansible.cfg` and the workspace's vault password file, and asks first (`--yes` answers). `--tags` picks the plays (`all` by default, which leaves out the ones tagged `never`; `never` selects them all, `k3s-destroy` included, so it asks for the word `yes` too), `--update` installs the playbook's collections first, and the arguments after `--` go to `ansible-playbook` as they are (`--limit`, `--check`, `-v`). One play runs on the workstation: `k3s-users`, which creates the inventory's `adminUsers` on the cluster. It gets a kubeconfig holding only the workspace's context (`contextName`, below), never whichever context is current, in a temporary directory removed afterwards. Without that context, it gets none and reaches no cluster: fetch it with `ohmlab kubeconfig`, then run `ohmlab provision --tags k3s-users`. `ohmlab provision destroy` runs `k3s-destroy`, which removes k3s, its data and `/var/lib/rancher` from every node, once you type `yes`. `ohmlab provision upgrade-os` runs `os-upgrade`: the packages of every host, one host at a time, rebooting those that need it. `--dry-run` prints the commands and runs nothing.

`ohmlab sops decrypt` and `ohmlab sops encrypt`, for instances that commit Sops-encrypted manifests, convert every `*.enc.yaml` under the workspace's `argo-cd/` (`--dir` for another place) to `*.dec.yaml`, and back. sops writes to a hidden temporary file that replaces the target only once it succeeded, so a missing key never truncates a good file, and an empty `*.dec.yaml` is never encrypted. `.gitignore` must cover `*.dec.yaml`: a decrypted file that git does not ignore gets a warning.

### The workspace file

Run from an instance repository, the workstation commands take their settings from its `ohmlab.yaml`, the nearest one from the current directory up to the repository root. A flag wins over the environment variable `run.sh` read, which wins over the file, which wins over the default. Paths in the file are relative to it; the defaults are relative to the workspace (the file's directory, else the repository root).

| Key | Flag | Environment | Default |
| --- | --- | --- | --- |
| `product` | `--product` | | the workspace |
| `instances` | `--instances` | | `argo-cd/instances` |
| `instance` | `--instance` | | the only folder there with an `instance.yaml` and no leading `_` |
| `inventory` | `--inventory` | `ANSIBLE_INVENTORY` | `ansible/inventory` |
| `vaultPasswordFile` | `--vault-password-file` | `ANSIBLE_VAULT_PASSWORD_FILE` | `ansible/.vault_password` when it exists (`ohmlab kubeconfig` and `ohmlab provision` need one) |
| `contextName` | `--name` (`ohmlab kubeconfig`), `--context` (`ohmlab bootstrap`, `ohmlab argocd password`, `ohmlab provision`) | `KUBE_CONTEXT_NAME` | `ohmlab` |

```yaml
# ohmlab.yaml at the root of an instance repository
product: ../ohmlab
inventory: ansible/inventory
contextName: mylab
```

## Network

```sh
# Find ips on local subnet
sudo nmap -sN 192.168.1.1/24

# Traefik dashboard (never exposed publicly)
kubectl -n traefik port-forward deploy/traefik 8080:8080
# → http://localhost:8080/dashboard/

# Test a hostname against one node, bypassing DNS
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
  --profile mylab \
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
