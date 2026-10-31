# ohmlab :alembic:

ohmlab is a GitOps kit to run a self-hosted platform on Kubernetes:
- **Ansible** roles that provision a gateway and a [k3s](https://k3s.io) cluster
- a **bootstrap chart** that installs a self-managed Argo CD, which then deploys everything else from git
- a **catalog** of charts (identity, secrets, storage, observability, security, developer tools) that each instance enables and configures
- a **controller** that seeds the secrets the apps need into Vault

A cluster's own configuration lives in an instance: which apps it runs, with which values, on which machines. An instance can sit in a clone of this repository or in a repository of its own; [this-is-tobi/homelab](https://github.com/this-is-tobi/homelab) is the reference deployment.

The two halves are independent: if you already have a cluster (managed Kubernetes or otherwise), skip Ansible and use the GitOps layer on its own — it only needs a kubeconfig. See [Using an existing cluster](./docs/03-installation.md#using-an-existing-cluster).

It is a quick starting point for simple infrastructure needs, or for testing tools such as monitoring, alerting, automated deployment and security scanning.

## Documentation

__Website:__ <https://this-is-tobi.com/ohmlab/introduction>.

__Table of Contents__ *- md sources*:
- [Compatibility](./docs/02-compatibility.md)
- [Installation](./docs/03-installation.md)
- [Infrastructure](./docs/04-infrastructure.md)
- [Services](./docs/05-services.md)
- [Projects](./docs/06-projects.md)
- [Cheat sheet](./docs/07-cheat-sheet.md)

## Quickstart

Make sure all [prerequisites](./docs/03-installation.md#prerequisites) are met.

__Setup directory:__
```sh
# Clone the repository
git clone --depth 1 https://github.com/this-is-tobi/ohmlab.git && cd ./ohmlab && rm -rf ./.git && git init

# Copy inventory example to inventory
cp -R ./ansible/inventory-example ./ansible/inventory
```

### Infrastructure

__Setup inventory:__
- [hosts.yml](./ansible/inventory-example/hosts.yml)
- [all.yml](./ansible/inventory-example/group_vars/all.yml)
- [gateway.yml](./ansible/inventory-example/group_vars/gateway.yml)
- [k3s.yml](./ansible/inventory-example/group_vars/k3s.yml)

__Install:__
```sh
# Deploy gateway and K3s cluster, fetch kubeconfig
./run.sh -p ./ansible/install.yml -u -k
```

### Kubernetes Services (GitOps)

__Setup configuration:__
- Pick (or create) an instance under [./argo-cd/instances/](./argo-cd/instances/) (e.g. `mylab/`):
  - [`instance.yaml`](./argo-cd/instances/_example/instance.yaml) — cluster destination, env, repos, project bindings.
  - [`core.yaml`](./argo-cd/instances/_example/core.yaml) — platform/core tier app catalog.
  - [`tenant.yaml`](./argo-cd/instances/_example/tenant.yaml) — user-facing apps catalog.
- Configure values in [./argo-cd/instances/\<instance\>/values/core/](./argo-cd/instances/_example/values/core/) and [./argo-cd/instances/\<instance\>/values/tenant/](./argo-cd/instances/_example/values/tenant/).
- For a brand-new instance, copy the documented template at [./argo-cd/instances/_example/](./argo-cd/instances/_example/) (folders prefixed with `_` are excluded by the root manager and treated as templates). See [Installation > Tier-flexible apps](./docs/03-installation.md#tier-flexible-apps) for apps (ingress, cert-manager, keycloak, prometheus-stack, ...) that can live in either tier depending on topology.

__Install:__
```sh
# Set kubectl context
kubectl config use-context homelab

# Bootstrap (or upgrade) the mylab instance
./run.sh -b mylab
```

> *Optionally pass `ARGOCD_ADMIN_PASSWORD=mypass` before the command to set the ArgoCD admin password explicitly. Otherwise the chart auto-generates one and the script prints it at the end.*
