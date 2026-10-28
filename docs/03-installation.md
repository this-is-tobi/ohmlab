# Installation

The installation is performed in two phases:
1. **Infrastructure** deployment with [Ansible](https://www.ansible.com/) for gateway and K3s cluster setup
2. **Applications** deployment with [ArgoCD](https://argo-cd.readthedocs.io/) following a GitOps approach

> [!TIP]
> **Already have a cluster?** Phase 1 is optional. If you run managed Kubernetes (EKS/GKE/AKS/DOKS/...) or any existing cluster, skip straight to [Applications (GitOps)](#applications-gitops) — the `argo-cd/` tree only needs a working kubeconfig and does not depend on Ansible, K3s, or the gateway host. See [Using an existing cluster](#using-an-existing-cluster) for what to adjust.

## Prerequisites

Following tools need to be installed on the computer running the deployment:
- [ansible](https://ansible.com) *- infrastructure as code software tools.*
- [age](https://github.com/FiloSottile/age) *- simple, modern and secure encryption tool (only for the optional Sops secrets).*
- [helm](https://helm.sh/) *- Kubernetes package manager.*
- [htpasswd](https://httpd.apache.org/docs/current/programs/htpasswd.html) *- bcrypt password hashing (apache2-utils on Linux, ships with macOS).*
- [kubectl](https://kubernetes.io/docs/reference/kubectl/) *- Kubernetes command-line tool.*
- [sops](https://github.com/getsops/sops) *- simple and flexible tool for managing secrets (only for the optional Sops secrets).*
- [sshpass](https://sourceforge.net/projects/sshpass) *- non-interactive ssh password auth.*
- [yq](https://github.com/mikefarah/yq) *- portable command-line YAML, JSON, XML, CSV, TOML and properties processor.*

```sh
# Clone the repository
git clone --depth 1 https://github.com/this-is-tobi/homelab.git && cd ./homelab && rm -rf ./.git

# Copy inventory example to inventory
cp -R ./ansible/inventory-example ./ansible/inventory
```

### Ansible Vault

Sensitive values (passwords, tokens, certificates) are stored in `ansible/inventory/group_vars/vault.yml` and encrypted at rest with [ansible-vault](https://docs.ansible.com/ansible/latest/vault_guide/index.html).

1. Create a vault password file (never committed — already in `.gitignore`):

```sh
# Generate a strong random password
openssl rand -base64 32 > ./ansible/.vault_password
```

2. Fill in the secrets in `ansible/inventory/group_vars/vault.yml`:

```yaml
vault_ansible_password: <ssh-password>
vault_pihole_password: "" # auto-generated if empty
vault_wireguard_password: "" # auto-generated if empty
vault_k3s_token: "" # populated during cluster bootstrap
vault_k3s_ca_data: "" # populated during cluster bootstrap
```

3. Encrypt the vault file:

```sh
cd ansible && ansible-vault encrypt inventory/group_vars/vault.yml
```

> **Notes**:
>
> *To edit secrets later: `ansible-vault edit inventory/group_vars/vault.yml`*
>
> *Auto-generated values are created once and persisted in gitignored files next to the inventory — `inventory/group_vars/.pihole_password`, `.wireguard_password` (by the gateway roles) and `.pi_password` (by `scripts/setup-pi.sh`) — and reused on later runs. Copy them into the vault when you want them under version control.*
>
> *The vault password file path is configured in `ansible.cfg` (`vault_password_file = .vault_password`).*

> __*Notes*__:
>
> *PiHole and Wireguard installation can be ignored by setting `enabled: false` in [gateway group_vars](../ansible/inventory-example/group_vars/gateway.yml).*

## Settings

### Infrastructure

Update the [hosts file](../ansible/inventory-example/hosts.yml) and [group_vars files](../ansible/inventory-example/group_vars/) to provide the appropriate infrastructure settings.

All sensitive values are indirected through `vault_*` variables in `group_vars/vault.yml` (see [Ansible Vault](#ansible-vault) above). Non-secret settings are in `all.yml`, `gateway.yml` and `k3s.yml`.

To create admin access to the machines, provide admin user information in `group_vars/all.yml` (`adminUsers`). Each entry creates:
- a **dedicated OS account** on every host with the given `sshPubKey` in its own `authorized_keys` (attributable logins — keys are never appended to the shared `ansible_user` account), plus optional passwordless sudo with `sudo: true`;
- a **Kubernetes user** (x509 client certificate + kubeconfig) with the RBAC tier set by `role`: `namespace-admin` (default — a personal namespace with full rights inside it), `cluster-admin`, or `view`.

`state: absent` removes the OS account, the SSH key and all Kubernetes RBAC. Note that the issued client certificate keeps *authenticating* until it expires (Kubernetes has no CRL) — deleting the bindings is what revokes *authorization*. Certificates are valid one year and re-issued automatically when a run finds them within 30 days of expiry.

### Applications (GitOps)

Applications are managed by a **two-level** ApplicationSet hierarchy:

1. The root `manager` ApplicationSet (shipped by `ohmlab`) discovers every folder under [./argo-cd/instances/](../argo-cd/instances/) and emits one Application per instance pointing at the [./argo-cd/apps/instance-manager](../argo-cd/apps/instance-manager) chart.
2. That chart in turn renders **two child ApplicationSets per instance** — `core-<instance>` and `tenant-<instance>` — that fan out into the actual leaf Applications.

Configuration is split per instance and per scope:

| Path                                                                                                             | Purpose                                                                                                       |
| ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| [./argo-cd/instances/\<instance\>/instance.yaml](../argo-cd/instances/homelab/instance.yaml)                     | Per-instance metadata: cluster destination, env, repos, AppProject bindings.                                  |
| [./argo-cd/instances/\<instance\>/core.yaml](../argo-cd/instances/homelab/core.yaml)                             | Core tier app catalog (platform/infra/identity/observability/security).                                       |
| [./argo-cd/instances/\<instance\>/tenant.yaml](../argo-cd/instances/homelab/tenant.yaml)                         | Tenant tier app catalog (user-facing services).                                                               |
| [./argo-cd/instances/\<instance\>/values/core/\<app\>.yaml](../argo-cd/instances/homelab/values/core/)           | Per-instance Helm values for each core app.                                                                   |
| [./argo-cd/instances/\<instance\>/values/tenant/\<app\>.yaml](../argo-cd/instances/homelab/values/tenant/)       | Per-instance Helm values for each tenant app.                                                                 |
| [./argo-cd/instances/\<instance\>/values/core/ohmlab.yaml](../argo-cd/instances/homelab/values/core/ohmlab.yaml) | Bootstrap values for core ArgoCD + the root `manager` AppSet + the `admin-core` / `admin-tenant` AppProjects. |
| [./argo-cd/apps/\<app\>/](../argo-cd/apps/)                                                                      | Helm chart catalog (chart sources only — values live in the trees above).                                     |

To enable or disable a service for an instance, edit the matching `core.yaml` or `tenant.yaml` and flip the `"enabled"` field on the relevant entry.

Per-app overrides supported in the JSON catalogues (all optional):

| Field                | Default                                                  | Use case                                                                   |
| -------------------- | -------------------------------------------------------- | -------------------------------------------------------------------------- |
| `chart`              | same as `app`                                            | Use a different chart directory under `argo-cd/apps/`.                     |
| `chartPath`          | `argo-cd/apps/<chart>`                                   | Point at a chart **outside** `argo-cd/apps/` (e.g. self-managed `ohmlab`). |
| `catalog`            | `defaultCatalog`, else `git` (the repository of the instance) | Take the chart from another catalog, see [Catalogs](#catalogs) and [Charts from a registry](#charts-from-a-registry). |
| `releaseName`        | same as `app`                                            | Adopt an existing helm release for self-management.                        |
| `namespace`          | `<prefix><app><suffix>`                                  | Pin to an explicit namespace (e.g. `argocd-system`).                       |
| `destination.server` | `instance.yaml.destination.server`                       | Target a different cluster (multi-cluster).                                |
| `valuesPath`         | `argo-cd/instances/<instance>/values/<scope>/<app>.yaml` | Point to a non-conventional values file.                                   |
| `targetRevision`     | the version of its catalog, else `instance.yaml.targetRevision` | Pin the app to a git revision, to another catalog version, or to a chart version with a registry catalog. |
| `syncWave`           | required                                                 | ArgoCD sync ordering.                                                      |

#### Catalogs

Where the charts come from is the instance's choice, declared once in `instance.yaml`. Without any setting every chart is the folder `argo-cd/apps/<chart>` of `repoURL` at `targetRevision`, the catalog named `git`. A bundle catalog gives the same folder from another repository (git, or an `oci://` artifact holding the same layout) at one version, so an instance runs one tested set of charts and moves all of them by changing one line:

```yaml
# argo-cd/instances/<instance>/instance.yaml
catalogs:
  release:
    repoURL: https://github.com/this-is-tobi/homelab.git
    version: v0.1.0
defaultCatalog: release # every entry without `catalog:` comes from it

# argo-cd/instances/<instance>/core.yaml
- app: gitea              # runs `release` at v0.1.0
- app: kyverno
  targetRevision: v0.0.9  # this app stays on an older version
- app: my-own-app
  catalog: git            # a chart of the instance repository, as without a default
```

- `catalogs.git` is reserved. `defaultCatalog` must be declared under `catalogs` (or be `git`), and a bundle needs a `repoURL`: the manager chart refuses to render otherwise, instead of Applications quietly coming from somewhere else.
- A bundle without `version` leaves the revision to each entry's `targetRevision`; an entry with neither gets an invalid revision, so nothing resolves by accident. A catalog name that is not declared gives the Application no repository.
- The AppProject of the tier must list the repository of every catalog in `sourceRepos`, spelled exactly like the catalog (with the `oci://` scheme for an artifact).
- The `ohmlab` entry that `./run.sh -b` installs needs a registry catalog (below): helm installs a chart, not a bundle.
- `scripts/build-catalog.sh <dir>` builds the bundle from a checkout, for a fork or a local test: one `catalog.tar.gz` with the charts of `argo-cd/apps` and their dependencies vendored (so Argo CD needs no access to any upstream chart repository), the `ohmlab` chart and the example instance, from the files git tracks only. Push it as a single-layer OCI artifact (`crane append --oci-empty-base -f catalog.tar.gz -t <registry>/<repo>:<version>`) and declare that repository as a catalog.
- The catalog released from this repository is `oci://ghcr.io/this-is-tobi/ohmlab/catalog`, one version per release of `argo-cd/apps` (OCI tag `X.Y.Z`, git tag `catalog-vX.Y.Z`). A release PR collects the conventional commits that touched `argo-cd/apps`, Renovate's chart bumps included (they are `fix(deps)`, or `feat(deps)` for a major); merging it publishes the bundle, signs it, attaches a build provenance and then checks the published digest the way an adopter would (`scripts/ci/verify-catalog.sh`: anonymous pull, signature, provenance, every chart rendered from the bundle alone). Check a version before you pin it:

```sh
cosign verify ghcr.io/this-is-tobi/ohmlab/catalog:<version> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/this-is-tobi/github-workflows/\.github/workflows/attest-docker\.yml@refs/tags/v[0-9]+(\.[0-9]+)*$'
```

#### Charts from a registry

By default every chart is a directory of the git repository. An entry can instead take a published Helm chart from an OCI registry: declare the registry once in `instance.yaml`, then point the entry at it.

```yaml
# argo-cd/instances/<instance>/instance.yaml
catalogs:
  ohmlab: ghcr.io/this-is-tobi/charts # registry path, no oci:// scheme

# argo-cd/instances/<instance>/core.yaml
- app: ohmlab
  enabled: "true"
  catalog: ohmlab
  targetRevision: 0.1.3 # chart version, required
  releaseName: ohmlab
  namespace: argocd-system
  syncWave: -10
```

The chart name is the entry's `chart` (default: the app name) and the values still come from the instance values tree, so nothing else changes. A few rules keep this safe:

- `targetRevision` is the exact chart version and has no default. An entry without it, or with a version range, is not pinned: leave a range out, since a registry tag can be re-pushed. The Application of an entry that omits it reports an invalid revision and no other app is affected.
- A `catalog` name that is not declared in `instance.yaml` gives that Application no repository, so it is invalid instead of falling back to git.
- The AppProject of the tier must list the registry path in `sourceRepos` (`projects.<tier>.sourceRepos` in the `ohmlab` values), spelled exactly like the catalog entry.
- The manager chart reads `catalogs` from `instance.yaml` while the ApplicationSet reads the entries from `core.yaml` / `tenant.yaml`, on separate refreshes. An entry added in the same commit as its catalog can therefore show `Unknown` (invalid, nothing deployed or removed) for a few minutes until both have caught up. Declaring the catalog in an earlier change avoids it.
- The `ohmlab` pin of an instance is bumped by Renovate (`renovate.json` watches the registry): merging its pull request is what updates the self-managed Argo CD.

> **`syncWave` semantics**: on an AppSet-generated Application, the sync-wave annotation alone orders **nothing** — apps sync in parallel and converge by retry. To actually enforce the ordering, enable progressive sync (`progressiveSync.enabled: true` in the `instance-manager` chart values); it maps waves onto ApplicationSet `RollingSync` steps. Requires the alpha `ApplicationSetProgressiveSyncs` feature gate, which the `ohmlab` chart enables on the core ArgoCD. It also turns automated sync off on the generated Applications: the ApplicationSet controller syncs an app again only when its revision changes, so drift at the same commit (a hand edit, a chart that renders more once a CRD exists) waits for the next commit or a manual sync.

### What the apps expect from each other

Waves are not enforced by default (see the `syncWave` semantics above), so an app whose chart ships objects of another app's CRDs fails its first sync and converges once that app is up, provided it is up within the retry window: a failed automated sync is retried three times (about three and a half minutes) and then not again for the same commit. An app still failed after that stays so until you sync it once by hand or push a new commit. When you enable a subset, enable the providers first:

| Needs                                                      | Provided by        | Used by                                                                                                                             |
| ---------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| Vault Secrets Operator CRDs (`secrets.hashicorp.com`)      | `vault-operator`   | Almost every chart: its secrets are `vso.vaultStaticSecrets` entries, each with the Vault KV `mount` and `path` to set per instance |
| Prometheus Operator CRDs (`monitoring.coreos.com`)         | `prometheus-stack` | Charts that ship a ServiceMonitor, PodMonitor or PrometheusRule: switch their `monitoring` values off where you do not run it (`vault-operator` and `ohmlab-controller` leave theirs out by themselves when the CRDs are missing) |
| Gateway API CRDs                                           | `./run.sh -b`      | The routes of the apps exposed through a gateway                                                                                    |

Vault, delivered by the Vault Secrets Operator, is the only secret backend supported today. Other backends (External Secrets, ...) are planned: every chart declares its secrets in one place (`vso:`) so they can be swapped later.

The `_example` values render for every chart (CI checks it) and every secret they declare is readable with the Vault policies and roles of the chart defaults (CI checks that too), so only the hostnames, the paths under `platforms/<environment>/<tier>/<app>` and similar are yours to set. The Vault KV mount is `homelab`, created by the `vault-operator` chart; to use another name, override its policies and `secrets` list completely. The example values run Argo CD with one replica of each component and no Redis HA, and Vault with one replica, so that they work on a single node; the chart defaults are highly available (three nodes or more), so drop those overrides there.

### Secrets Management

Secrets live in Vault, delivered by the Vault Secrets Operator (see [Secrets](05-services.md#secrets)). During setup every password, token and so on that can be generated is randomly generated and stored in Vault; the credentials that cannot be generated (a GitHub App, a webhook URL, S3 keys) are declared as empty placeholders and set by hand. Nothing secret is committed.

Each `vso.vaultStaticSecrets` entry copies only what its consumer needs into its Kubernetes Secret: its `destination.transformation` sets `excludeRaw: true` and `excludes: [".*"]` and lists the keys to build under `templates`. Without that, the Vault Secrets Operator also writes the whole Vault path (as `_raw`) into the Secret, so a pod that mounts one Secret of an app would receive every credential of that app, such as the database superuser. CI refuses an entry that leaves them out (`scripts/ci/check-vso-isolation.sh`).

#### Optional: Sops-encrypted manifests

The `sops` app ([sops-secrets-operator](https://github.com/isindir/sops-secrets-operator)) and the `./run.sh -d` / `-e` helpers are there for instances that prefer to commit encrypted Kubernetes manifests (a `SopsSecret` in the `templates/` of a chart) next to their values. This repository's own instance does not use them. Prefer Vault where you can: the operator creates Secrets in any namespace, so it holds a cluster-wide permission on Secrets.

1. Have the ohmlab controller generate the age key pair in Vault, by adding this entry to `ohmlab.vault.secrets` in the `vault-operator` values (the controller must be enabled, see [docs/08-controller.md](08-controller.md)), then enable the `sops` app in `core.yaml` (the example values of the app point at that path).

   ```yaml
   - path: homelab/platforms/production/core/sops
     data:
       secret: "<age:secret>"
       public: "<age:public>"
   ```

2. Create `.sops.yaml` at the root of the repository. The first `age` entry is the `public` key generated above, so the cluster can decrypt; add your own keys (age or PGP) so you can edit locally.

   ```yaml
   creation_rules:
   - path_regex: \.yaml$
     encrypted_regex: ^(data|stringData)$
     key_groups:
     - age:
       - <public key of the cluster>
       - <your own public key>
   ```

3. Write the manifests as `*.dec.yaml` (git-ignored) under `argo-cd/`, encrypt them with `./run.sh -e` and commit the `*.enc.yaml` files; `./run.sh -d` decrypts them back for editing.

## Deploy

### Infrastructure

Deploy gateway and K3s cluster using the Ansible playbook:

```sh
# Update Ansible collections and deploy infrastructure
./run.sh -p ./ansible/install.yml -u -k

# Or with specific tags
./run.sh -p ./ansible/install.yml -t gateway   # Deploy gateway only
./run.sh -p ./ansible/install.yml -t k3s       # Deploy K3s cluster only
```

The `-k` flag fetches the kubeconfig from the master node and merges it into your local kubeconfig.

### Applications (GitOps)

Once the infrastructure is ready, bootstrap the GitOps stack with a single command:

```sh
# Set kubectl context
kubectl config use-context homelab

# Bootstrap (or upgrade) the homelab instance
./run.sh -b homelab
```

This installs the `ohmlab` Helm release in the `argocd-system` namespace, which contains:
- The **core ArgoCD** instance (engine; not user-facing).
- The root `manager` ApplicationSet (discovers every instance under `argo-cd/instances/*`).
- The `admin-core` and `admin-tenant` AppProjects.

The chart is the local [./utils/helm](../utils/helm), unless the `ohmlab` entry of the instance's `core.yaml` has a `catalog` (see [Charts from a registry](#charts-from-a-registry)): then the published chart at the exact version pinned there is installed, which is the chart the self-managed Application runs afterwards. A `catalog` that is not declared in `instance.yaml`, or a version that is not exact, stops the script before it touches the cluster.

The root manager then renders one `instance-<name>` Application per discovered folder. That Application points at the [./argo-cd/apps/instance-manager](../argo-cd/apps/instance-manager) chart, which produces two child ApplicationSets (`core-<name>` and `tenant-<name>`). The first sync wave (-10) reconciles `ohmlab` itself onto its chart — from git by default, or from a pinned registry version with `catalog` (see [Charts from a registry](#charts-from-a-registry)) — and the bootstrap release is then **self-managed**.

```mermaid
sequenceDiagram
    participant Op as Operator
    participant Helm
    participant Core as core ArgoCD<br/>(argocd-system)
    participant Git as Git repo
    participant K8s as Kubernetes
    Op->>Helm: ./run.sh -b homelab
    Helm->>K8s: install ohmlab release<br/>(ArgoCD + root manager AppSet + AppProjects)
    Core->>Git: discover argo-cd/instances/*/
    loop For each instance folder
        Core->>K8s: render Application instance-<name><br/>(via instance-manager chart)
        Core->>K8s: emit core-<name> & tenant-<name> AppSets
    end
    loop For each enabled app per scope
        Core->>Git: read app chart and values
        Core->>K8s: apply Application (sync-wave order)
    end
    Core->>Core: adopt ohmlab release<br/>(self-management)
```

> __*Notes*__:
>
> *Multiple tags can be passed as follows:* `./run.sh -p ./ansible/install.yml -t gateway,k3s`
>
> *First gateway init can take a long time to run because of OpenVPN key generation (5-10min).*
>
> *Bootstrap admin password: pass `ARGOCD_ADMIN_PASSWORD=mypass ./run.sh -b homelab` to set it explicitly. Without this var, the ArgoCD chart auto-generates a password and stores it in `argocd-initial-admin-secret`; the script prints it at the end of the run.*
>
> *OIDC for the core ArgoCD is intentionally disabled at bootstrap. Enable it in [argo-cd/instances/homelab/values/core/ohmlab.yaml](../argo-cd/instances/homelab/values/core/ohmlab.yaml) once Keycloak is ready (uncomment the `oidc.config` block and provide the client secret out-of-band).*

### Using an existing cluster

The GitOps layer (`argo-cd/`) is independent of how the cluster was created — it only needs a kubeconfig with cluster-admin. On managed Kubernetes (EKS/GKE/AKS/...) or any pre-existing cluster, skip the Ansible phase entirely and run `./run.sh -b <instance>` against your context.

Create your own instance folder rather than reusing `homelab` (which is this repo's own cluster, full of environment-specific values) — copy [argo-cd/instances/_example](../argo-cd/instances/_example) and see [Adding a new instance](#topologies). Points worth checking for a non-K3s cluster:

- **Storage** — `longhorn` assumes bare-metal disks. On a cloud provider, disable it and use the provider's own `StorageClass` (set it as default, or set `storageClass` explicitly on the apps that request one).
- **Ingress exposure** — the `traefik` app's Service is type `LoadBalancer`. A cloud provider assigns a real external IP automatically, so drop the `spec.externalIPs` list this repo uses (that exists only because it runs bare metal without an LB controller).
- **CNI** — managed clusters ship their own; leave the `cilium` app disabled. Read the CNI caveat at the top of any `templates/networkpolicy.yaml` before enabling `networkPolicy` — the rules were verified against Cilium and rely on three CNI-specific behaviours.
- **Pod/service CIDRs** — anything referencing `10.42.0.0/16` / `10.45.0.0/16` / `10.43.0.0/16` (NetworkPolicy `ipBlock` rules, `lanOnly.sourceRange`, crowdsec's `allowed_ranges`) must match your cluster's actual ranges.
- **Node labels** — apps pinning workloads with `nodeSelector: {node-type: worker}` (CNPG databases, Vault) need that label to exist, or the selector removed.
- **Gateway-host services** — HAProxy, PiHole, WireGuard and the gateway CrowdSec engine are Ansible-managed Docker services on a separate host. They have no in-cluster equivalent; ignore them unless you replicate that setup.

## Destroy

It is possible to cleanly destroy the K3s cluster by running:

```sh
# Destroy cluster
./run.sh -p ./ansible/install.yml -t k3s-destroy
```

## Maintenance

### OS upgrades

Run an OS package upgrade on all managed hosts (gateway + K3s nodes), rebooting only if required:

```sh
./run.sh -p ./ansible/install.yml -t os-upgrade
```

### Debian major version upgrade

Upgrade all hosts in-place from one Debian release to the next (e.g. bookworm → trixie). K3s nodes are automatically **drained** before the upgrade and **uncordoned** after reboot. Hosts are processed one at a time (`serial: 1`).

1. Set the target release in `inventory/group_vars/all.yml` (or pass it as extra var):

```yaml
common_dist_upgrade_target_release: trixie
```

2. Run the dist-upgrade:

```sh
./run.sh -p ./ansible/install.yml -t dist-upgrade
```

> **Notes**:
>
> *Hosts already running the target release are automatically skipped.*
>
> *Ensure `kubectl` is configured locally — the drain/uncordon commands run from your workstation.*
>
> *After a major upgrade, re-run the full infra playbook to reconcile Docker repos and other codename-dependent configuration: `./run.sh -p ./ansible/install.yml -u`*

### Ansible collection updates

Update pinned Ansible Galaxy collections to the latest compatible version before running a playbook:

```sh
./run.sh -p ./ansible/install.yml -u
```

Collection version ranges are pinned in [ansible/collections/requirements.yml](../ansible/collections/requirements.yml). Bump the major range when upgrading to a new major release.

### Ansible vault

Edit encrypted secrets:

```sh
cd ansible && ansible-vault edit inventory/group_vars/vault.yml
```

### Docker image updates (gateway)

Image versions for gateway services (HAProxy, PiHole, WireGuard) are managed in `inventory/group_vars/gateway.yml`. Update the `version` field and re-run the gateway playbook:

```sh
./run.sh -p ./ansible/install.yml -t gateway
```

### K3s version updates

The K3s version is pinned in two places that Renovate bumps together in one PR:

- `system-upgrade.k3s.version` in [argo-cd/instances/\<instance\>/values/core/system-upgrade.yaml](../argo-cd/instances/homelab/values/core/system-upgrade.yaml): the version the running cluster is upgraded to. The [system-upgrade-controller](https://github.com/rancher/system-upgrade-controller) applies it in-cluster, control-plane nodes one at a time then the agents, inside the maintenance window set in [argo-cd/apps/system-upgrade/values.yaml](../argo-cd/apps/system-upgrade/values.yaml) (every night, 02:00–06:00 Europe/Paris).
- `k3sVersion` in `inventory/group_vars/k3s.yml` (plus the example inventory and the role default): the version ansible installs on new or re-provisioned nodes.

Merging the PR is the upgrade: it starts in the next window, or right away if merged during one. Follow it with `kubectl -n system-upgrade get plans,jobs` and `kubectl get nodes`.

Kubernetes does not support skipping a minor version, so Renovate opens one PR per minor, and minor bumps wait for approval on the dependency dashboard (patch bumps open on their own, after a week of soak). Before approving a minor:

1. Check that every chart deployed on the cluster supports the new Kubernetes minor.
2. In the same PR, bump by hand `KUBECTL_VERSION` in `utils/Dockerfile` and `controller.job.kubectlImage` in `argo-cd/apps/system-upgrade/values.yaml` to that minor (Renovate only moves their patch version).

### Kubernetes application updates

Application chart versions are managed via GitOps — update the Helm chart version in the relevant `argo-cd/apps/<app>/Chart.yaml` and push. ArgoCD auto-syncs the change.

### Releases

Three parts are released from this repository, each with its own version, release PR, changelog, git tag and GitHub release:

| Part | Published as | Git tag | Changelog |
|---|---|---|---|
| `ohmlab` CLI (`utils/`) | binaries on the GitHub release, image `ghcr.io/this-is-tobi/ohmlab/cli:X.Y.Z` | `vX.Y.Z` | `utils/CHANGELOG.md` |
| bootstrap chart (`utils/helm`) | `oci://ghcr.io/this-is-tobi/charts/ohmlab`, version `X.Y.Z` | `chart-vX.Y.Z` | `utils/helm/CHANGELOG.md` |
| catalog (`argo-cd/apps`) | `oci://ghcr.io/this-is-tobi/ohmlab/catalog:X.Y.Z` | `catalog-vX.Y.Z` | `argo-cd/apps/CHANGELOG.md` |

release-please opens one release PR per part from the conventional commits under its directory (Renovate's dependency bumps there are `fix(deps)`, `feat(deps)` for a major); merging it publishes that part only, signed and with a build provenance, and the chart and the catalog are then checked the way an adopter would use them (anonymous pull of the published digest, signature, provenance, render with the example instance: `scripts/ci/verify-chart.sh`, `scripts/ci/verify-catalog.sh`). The CLI release stays marked "Latest". The chart README is generated by helm-docs from `utils/helm/README.md.gotmpl` (`helm-docs --chart-search-root utils/helm`); CI fails when it is stale.

## Architecture

### Two ArgoCD instances

The cluster runs **two** ArgoCD instances with very different roles:

| Instance     | Namespace       | Purpose                                                                                                                               |
| ------------ | --------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| **core**     | `argocd-system` | The engine. Runs the `manager` ApplicationSet that drives every other app. Not user-facing.                                           |
| **personal** | `argo-cd`       | A user-facing sandbox at `gitops.<domain>`. Driven by core, but has no `manager` itself — used through the UI for ad-hoc deployments. |

Both instances are deployed from the same chart at [argo-cd/apps/argo-cd/](../argo-cd/apps/argo-cd/), differentiated by their per-instance values files.

### App-of-apps flow

```mermaid
flowchart TB
    subgraph cli["Operator (CLI)"]
        runsh["./run.sh -b homelab"]
    end

    subgraph bootstrap["Helm release: ohmlab (argocd-system)"]
        coreArgo["core ArgoCD"]
        projC["AppProject: admin-core"]
        projT["AppProject: admin-tenant"]
        rootAS["AppSet: manager (root)"]
    end

    subgraph generated["Per-instance generated"]
        instApp["Application: instance-homelab"]
        coreAS["AppSet: core-homelab"]
        tenantAS["AppSet: tenant-homelab"]
    end

    subgraph apps["Leaf Applications"]
        selfApp["ohmlab (self)"]
        coreApps["longhorn, cert-manager,<br/>vault-operator, keycloak,<br/>prometheus-stack, ..."]
        tenantApps["argo-cd (personal),<br/>gitea, mattermost,<br/>rustfs, teleport, ..."]
    end

    subgraph git["Git repository"]
        instYaml["argo-cd/instances/<br/>homelab/instance.yaml"]
        coreJson["argo-cd/instances/<br/>homelab/core.yaml"]
        tenantJson["argo-cd/instances/<br/>homelab/tenant.yaml"]
        appCharts["argo-cd/apps/&lt;app&gt;/"]
        coreVals["argo-cd/instances/<br/>homelab/values/core/"]
        tenantVals["argo-cd/instances/<br/>homelab/values/tenant/"]
    end

    runsh -->|helm install| bootstrap
    rootAS -->|discovers| git
    rootAS -->|renders| instApp
    instApp -->|emits| coreAS
    instApp -->|emits| tenantAS
    coreAS -->|reads| coreJson
    tenantAS -->|reads| tenantJson
    coreAS -->|generates| selfApp
    coreAS -->|generates| coreApps
    tenantAS -->|generates| tenantApps
    selfApp -.->|adopts release| bootstrap
    coreApps -->|chart from| appCharts
    coreApps -->|values from| coreVals
    tenantApps -->|chart from| appCharts
    tenantApps -->|values from| tenantVals
```

Adding a new instance is purely declarative — just create a folder under [argo-cd/instances/](../argo-cd/instances/) (with `instance.yaml` + `core.yaml` + `tenant.yaml`) and a matching `argo-cd/instances/<name>/values/{core,tenant}/` tree. The root manager picks it up on its next reconciliation:

1. Create `argo-cd/instances/<name>/instance.yaml` (cluster destination, repos, project bindings).
2. Create `argo-cd/instances/<name>/core.yaml` and/or `argo-cd/instances/<name>/tenant.yaml`.
3. Create `argo-cd/instances/<name>/values/core/` and/or `argo-cd/instances/<name>/values/tenant/` with at least a `ohmlab.yaml` (under `core/`) for the self-managed bootstrap App when shipping core on that cluster.
4. Bootstrap the **first** instance with `./run.sh -b <name>` against its target cluster; subsequent instances are then picked up automatically by the existing root manager.

### Topologies

The two-level pattern accommodates very different deployment models, all driven by the same root manager and chart catalog:

```mermaid
flowchart TB
    subgraph t1["All-in-one (homelab)"]
        h1(["folder: homelab/<br/>core.yaml + tenant.yaml"]) --> c1["single cluster"]
    end
    subgraph t2["SaaS shared core"]
        a2(["folder: saas-admin/<br/>core.yaml"]) --> ca["admin cluster"]
        b2a(["folder: saas-customer-a/<br/>tenant.yaml"]) --> cca["customer-a cluster"]
        b2b(["folder: saas-customer-b/<br/>tenant.yaml"]) --> ccb["customer-b cluster"]
    end
    subgraph t3["Dedicated core"]
        a3(["folder: org-x-admin/<br/>core.yaml"]) --> ox1["org-x admin cluster"]
        b3(["folder: org-x-prod/<br/>tenant.yaml"]) --> ox2["org-x prod cluster"]
    end
```

| Topology             | Folders on disk                                                  | Where things run                                                                   |
| -------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| All-in-one (homelab) | `homelab/` with both `core.yaml` and `tenant.yaml`               | Single cluster, single ArgoCD, both AppSets land on `in-cluster`.                  |
| SaaS shared core     | `saas-admin/` (core only) + N × `saas-customer-*/` (tenant only) | Admin cluster runs the core stack; each customer gets its own cluster for tenants. |
| Dedicated core       | One pair per org: `<org>-admin/` (core) + `<org>-prod/` (tenant) | Strict per-org isolation: dedicated admin cluster + dedicated app cluster.         |

The target cluster for each instance is set in its `instance.yaml.destination.server`; remote clusters are registered in ArgoCD via `Cluster` secrets (managed via Vault/VSO if desired).

### Tier-flexible apps

A handful of apps don't naturally belong in a single tier — they're needed *wherever workloads run*, regardless of whether the cluster is acting as a "core/admin" cluster or a "tenant/apps" cluster. The catalog lists them in **both** [`_example/core.yaml`](../argo-cd/instances/_example/core.yaml) and [`_example/tenant.yaml`](../argo-cd/instances/_example/tenant.yaml) with appropriate per-tier `syncWave`s; for any concrete instance you enable the entry in **exactly one** tier and leave the other disabled.

| App                    | Where to enable                                                                     |
| ---------------------- | ----------------------------------------------------------------------------------- |
| `cert-manager`         | Wherever Ingress / TLS certificates are issued.                                     |
| `traefik`              | Wherever an ingress controller is needed.                                           |
| `keycloak`             | Wherever the SSO IdP runs (often tenant in SaaS, core in all-in-one).               |
| `kubernetes-dashboard` | Wherever cluster admins want a UI; one per cluster.                                 |
| `longhorn`             | Wherever block storage is needed (typically every cluster with stateful workloads). |
| `prometheus-stack`     | Wherever observability is collected (often core in shared topologies).              |
| `teleport`             | Wherever the access proxy runs.                                                     |
| `vault`                | Wherever the secrets backend lives (often co-located with workloads consuming it).  |

> Enabling a tier-flexible app in **both** tiers of the same instance would create two `Application`s with the same name and is not supported. The `_example` template ships with everything disabled to make this an explicit, deliberate choice.

### Splitting infra and values across two repositories

Per-instance values live alongside the rest of the instance metadata (`argo-cd/instances/<inst>/values/{core,tenant}/<app>.yaml`). For most setups keeping everything in a single repo is the simplest option.

If you need to keep the chart catalog public while keeping per-instance values (which often contain hostnames, secrets references, OIDC client IDs, etc.) in a private repository, the `instance-manager` chart supports it natively:

```yaml
# argo-cd/instances/<inst>/instance.yaml
repoURL: https://github.com/this-is-tobi/homelab.git # public: charts + this file
valuesRepoURL: https://github.com/<you>/homelab-private.git # private: values tree only
targetRevision: main
```

When `valuesRepoURL` is set, the child AppSets attach **two** git sources to every generated `Application`:

- `repoURL` (the chart source) — used to fetch `argo-cd/apps/<app>/`.
- `valuesRepoURL` (the `$values` ref) — used to fetch `argo-cd/instances/<inst>/values/<tier>/<app>.yaml`.

In the values repository, mirror the path layout exactly:

```
homelab-private/                                # your private repo root
└─ argo-cd/
   └─ instances/
      └─ <inst>/
         └─ values/
            ├─ core/<app>.yaml
            └─ tenant/<app>.yaml
```

Per-app overrides also exist if a single app needs a different layout — `valuesPath` overrides the whole path, useful for charts that ship in their own repository entirely.

### Sync waves

Apps are reconciled in `syncWave` order. Default ordering for the homelab instance:

| Wave | Tier   | Apps                                     |
| ---- | ------ | ---------------------------------------- |
| -10  | core   | `ohmlab` (self)                          |
| 0    | core   | `longhorn`                               |
| 10   | core   | `cert-manager`, `vault-operator`         |
| 11   | core   | `traefik`                                |
| 13   | core   | `trust-manager`                          |
| 15   | core   | `kyverno`                                |
| 20   | core   | `cloudnative-pg`                         |
| 50   | core   | `prometheus-stack`                       |
| 55   | core   | `keycloak`                               |
| 60   | core   | `crowdsec`, `system-upgrade`, `teleport` |
| 100  | tenant | `argo-cd` (personal)                     |
| 110  | tenant | `gitea`, `mattermost`, `rustfs`          |
| 200  | tenant | `homepage`                               |

### Core Services

Core services provide the foundation for the platform:
- **Longhorn** *- storage management in the cluster.*
- **Traefik** *- ingress controller & Gateway API implementation to expose services.*
- **Cert-Manager** *- certificate management for TLS.*
- **Vault Operator** *- secret management for services deployments.*
- **Trust-Manager** *- publishes Vault's CA as a ConfigMap for Traefik's backend TLS validation.*
- **Kyverno** *- admission policy enforcement.*
- **ArgoCD** *- deployment management following GitOps.*
- **CloudNative-PG** *- PostgreSQL operator for databases.*

### Platform Services

Platform services are deployed on top of core services:
- **Keycloak** *- identity and access management (SSO).*
- **Gitea** *- self-hosted Git service.*
- **Mattermost** *- team communication.*
- **And more...*

## Known issues

At the moment, `mattermost` and `outline` images are not `arm64` compatible so their deployment are using custom mirror image with compatibility (see. [this repo](https://github.com/this-is-tobi/multiarch-mirror) and associated ArgoCD applications).

The [official Harbor helm chart](https://artifacthub.io/packages/helm/harbor/harbor) cannot be used due to arm64 incompatibility, the [Bitnami distribution](https://artifacthub.io/packages/helm/bitnami/harbor) is used instead.
