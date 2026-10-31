# Projects

This page describes how to interact with an ohmlab platform depending on your role.

```mermaid
flowchart LR
    admin([Admin / Operator])
    user([Platform user])
    contrib([Contributor])

    admin -->|provisions| infra[Gateway + K3s]
    admin -->|bootstraps| core[ohmlab]
    core -->|drives| platforms[Apps & SSO]
    user -->|consumes| platforms
    contrib -->|PRs| repo[(Git repo)]
    repo -->|GitOps| core
```

## Admin

The admin owns the cluster lifecycle: provisioning hosts, bootstrapping ArgoCD, and curating the apps catalog.

### First install

1. Prepare the inventory in [ansible/inventory/](../ansible/inventory-example/) (hosts, SSH keys, gateway settings).
2. Deploy infrastructure and fetch kubeconfig:
   ```sh
   ./run.sh -p ./ansible/install.yml -u -k
   ```
3. Bootstrap the GitOps stack:
   ```sh
   kubectl config use-context homelab
   ARGOCD_ADMIN_PASSWORD=<choose-a-password> ./run.sh -b mylab
   ```
4. Once `keycloak` is healthy, enable OIDC for the core ArgoCD by uncommenting the `oidc.config` block in [argo-cd/instances/\<instance\>/values/core/ohmlab.yaml](../argo-cd/instances/_example/values/core/ohmlab.yaml).

### Enable / disable services

1. Edit [argo-cd/instances/\<instance\>/core.yaml](../argo-cd/instances/_example/core.yaml) (platform tier) or [tenant.yaml](../argo-cd/instances/_example/tenant.yaml) (apps tier) and flip `enabled` on the relevant entry.
2. Commit & push. The corresponding child AppSet (`core-mylab` or `tenant-mylab`) reconciles automatically (no helm/kubectl needed).

### Tune service values

1. Edit [argo-cd/instances/\<instance\>/values/core/\<app\>.yaml](../argo-cd/instances/_example/values/core/) or [tenant/\<app\>.yaml](../argo-cd/instances/_example/values/tenant/).
2. Commit & push. The corresponding `Application` syncs the new values.

### Adding a new app

1. Create a Helm chart under `argo-cd/apps/<app>/` (or a wrapper around an upstream chart).
2. Decide the tier:
   - **core** for infra / identity / observability / security / cluster-wide concerns.
   - **tenant** for user-facing apps.
   - **tier-flexible** for things that may live in either tier depending on topology
     (e.g. ingress controller, cert-manager, longhorn, keycloak, prometheus-stack,
     teleport, vault, kubernetes-dashboard). For these, list the entry in BOTH
     `_example/core.yaml` AND `_example/tenant.yaml` (with appropriate `syncWave`s
     per tier) but only enable it in **one** tier per concrete instance — enabling
     it in both would create duplicate Application names.
3. Create a values file at `argo-cd/instances/<inst>/values/<tier>/<app>.yaml`.
4. Add an entry in `argo-cd/instances/<inst>/<tier>.yaml` with the appropriate `syncWave`.
5. Commit & push.

### Adding a new instance (another cluster / tenant / topology)

1. Create the folder `argo-cd/instances/<name>/` (copy from [_example/](../argo-cd/instances/_example/) as a template; drop the leading underscore — folders prefixed with `_` are excluded by the root manager AppSet and treated as templates).
2. Edit `instance.yaml` to set the destination cluster, env, repos, project bindings.
3. Populate `core.yaml` and/or `tenant.yaml` with the apps the instance should run.
4. Create `argo-cd/instances/<name>/values/{core,tenant}/` mirroring those catalogs (one values file per enabled app, plus `core/ohmlab.yaml` if the instance ships its own core ArgoCD).
5. The root `manager` picks up the new folder automatically. For a brand-new admin cluster, run `./run.sh -b <name>` once against it; for a tenant-only instance attached to an existing core, just commit & push.

See `Installation > Topologies` for the all-in-one / SaaS / dedicated-core variants.

### Backups & disaster recovery

Three data layers, three mechanisms — check each is actually **enabled** for your instance (several ship disabled until S3 credentials exist):

| Layer | Mechanism | Where configured |
| ----- | --------- | ---------------- |
| PostgreSQL databases | [CloudNative-PG](https://cloudnative-pg.io/) base backups + WAL to S3 | `backup.enabled` in each app's instance values (gitea, keycloak, mattermost, crowdsec, ...) |
| Vault (all credentials) | Raft snapshot CronJob to S3 | `backup-utils` block in `values/core/vault-operator.yaml` |
| Everything else on PVCs (git repos/LFS, attachments, object stores) | Longhorn RecurringJobs: local snapshots + S3 backups | `backups.recurringJobs` + `longhorn.defaultSettings.backupTarget` in `values/core/longhorn.yaml` |

ArgoCD itself is fully reconcilable from git — only `argocd-secret` (admin password / TLS) needs to survive a reinstall.

> ⚠ Local Longhorn **snapshots** live on the same disks as the volume — they protect against application-level mistakes, not disk or cluster loss. Only `task: backup` jobs (with a configured `backupTarget`) leave the cluster.

#### Restore runbooks

An untested backup is not a backup — rehearse these on a scratch namespace before you need them.

**PostgreSQL (CNPG)** — restore into a NEW cluster from S3 (never in place). The apps use the official CloudNativePG [`cluster` chart](https://artifacthub.io/packages/helm/cloudnative-pg/cluster) under the `cnpg` key, whose recovery mode is selected with `mode` + a `recovery` block pointing at the OLD cluster's object store:

```yaml
# In the app's `cnpg` values (new fullnameOverride, same bucket):
mode: recovery
recovery:
  method: plugin
  pluginConfiguration:
    name: barman-cloud.cloudnative-pg.io
  clusterName: <old-cluster-name>         # serverName the backups were written under
  destinationPath: s3://<bucket>/<path>   # the OLD cluster's backups.destinationPath
  endpointURL: https://s3.<region>.<provider>
  secret:
    create: false
    name: <existing-s3-secret>            # keys ACCESS_KEY_ID / ACCESS_SECRET_KEY
  instanceSidecarConfiguration:
    env:
    - name: AWS_DEFAULT_REGION
      value: <region>
```

then let the operator bootstrap from the base backup + WAL. Details: [CNPG recovery docs](https://cloudnative-pg.io/documentation/current/recovery/).

After any change of Postgres base image (a bump of the shared `ClusterImageCatalog`, or a restore onto a different image than the one that wrote the backup), run the read-only [cnpg-collation-audit.sql](../scripts/cnpg-collation-audit.sql) on each cluster's replica: its header explains how to run it and what to do if it reports collation-dependent objects.

**Vault** — restore a raft snapshot (also the recovery path if the PVC is lost):

```sh
# Fetch the snapshot from S3, copy it into the pod, then restore:
kubectl -n vault-operator-system cp vault-<date>.snap vault-0:/tmp/vault-<date>.snap
kubectl -n vault-operator-system exec -it vault-0 -- \
  vault operator raft snapshot restore -force /tmp/vault-<date>.snap
# -force is required when the snapshot comes from a different cluster ID
# (fresh reinstall). Unseal keys are those of the cluster that TOOK the
# snapshot — keep them offline, and with operator-managed auto-unseal replace
# the `vault-unseal-keys` secret with the OLD cluster's keys before restoring,
# otherwise the operator can never unseal the restored data.
```

**Longhorn volume** — UI → Backup → select backup → *Restore Latest Backup* (creates a new volume), then create a PV/PVC from it and point the workload at it. CLI equivalent: create a `Volume` CR with `spec.fromBackup` set to the backup URL.

## Platform user

A platform user consumes the services running on the cluster (Gitea, Mattermost, Grafana, ...). They do not have access to the cluster control plane.

### Logging in

1. Open <https://sso.\<your-domain\>>; sign up or use a pre-provisioned account.
2. From there, every service listed below uses Keycloak SSO — just click "Login with Keycloak" on the service of choice.

### Service catalogue

See `Services > Access` for the full list of user-facing endpoints (ArgoCD, Coder, Gitea, Grafana, Harbor, Mattermost, RustFS, Outline, SonarQube, Vaultwarden, ...).

### Personal ArgoCD sandbox

The user-facing ArgoCD instance lives at <https://gitops.\<your-domain\>> (in namespace `argo-cd`). It runs **without** the `manager` AppSet — use it to deploy ad-hoc Applications via the UI without affecting the platform.

> The "core" ArgoCD that drives the platform itself is internal and not user-facing.

### Authoring CI workflows

- [Gitea Actions runners](https://about.gitea.com/) are deployed cluster-wide via [actions-runner-controller](https://github.com/actions/actions-runner-controller).
- [Argo Workflows](https://argoproj.github.io/workflows/) is available for batch / DAG-style pipelines.

## Contributor

Contributors propose changes to ohmlab itself (charts, values, scripts, docs).

### Workflow

```mermaid
flowchart LR
    fork[Fork & branch]
    edit[Edit chart / values /<br/>instance JSON or YAML]
    test[helm template +<br/>./run.sh -b mylab<br/>against test cluster]
    pr[Open PR]
    merge[Merge to main]
    sync[Manager reconciles<br/>(root + child AppSets)]

    fork --> edit --> test --> pr --> merge --> sync
```

### Repository layout

| Path                                        | Purpose                                                                                                                                      |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| [ansible/](../ansible/)                     | Infrastructure (gateway + K3s) provisioning roles & playbooks.                                                                               |
| [utils/helm/](../utils/helm/)               | Bootstrap chart — ships core ArgoCD, the root `manager` AppSet, and `admin-core` / `admin-tenant` AppProjects.                               |
| [argo-cd/apps/](../argo-cd/apps/)           | Helm chart catalog (`gitea`, `keycloak`, ..., plus the `instance-manager` chart used by the root manager).                                   |
| [argo-cd/instances/](../argo-cd/instances/) | Per-instance folders. One per cluster / tenant, each with `instance.yaml` + `core.yaml` / `tenant.yaml` + `values/{core,tenant}/<app>.yaml`. |
| [run.sh](../run.sh)                         | Wrapper around Ansible + Helm with sane defaults.                                                                                            |

### Local validation

Before opening a PR:

```sh
# Render the bootstrap chart against an instance's values
helm template ohmlab ./utils/helm \
  -f argo-cd/instances/mylab/values/core/ohmlab.yaml

# Render the per-instance chart for a given instance
helm template instance-mylab ./argo-cd/apps/instance-manager \
  -f argo-cd/instances/mylab/instance.yaml \
  --set instance.name=mylab

# Lint a specific app chart
helm lint ./argo-cd/apps/<app>

# Validate instance catalogs (YAML)
yq e '.' argo-cd/instances/mylab/core.yaml >/dev/null
yq e '.' argo-cd/instances/mylab/tenant.yaml >/dev/null
```

### Conventions

- One commit per logical change (Conventional Commits format).
- Helm chart bumps go in the chart's `Chart.yaml` (subchart deps + parent version).
- Per-app values changes go in `argo-cd/instances/<instance>/values/<tier>/<app>.yaml` — never in the chart's own `values.yaml` (which holds defaults only).
- Secrets are never committed in plaintext: they live in Vault (see [Secrets](05-services.md#secrets)).
- Mermaid is the only diagram format used in docs.
