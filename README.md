# Homelab :alembic:

This project aims to build a homelab for personal testing on infrastructure, development, CI/CD, etc...

It provides a complete configuration with common web services using:
- **Ansible** for infrastructure deployment (gateway and [k3s](https://k3s.io) cluster)
- **GitOps** (ArgoCD) for Kubernetes applications deployment

The two halves are independent: if you already have a cluster (managed Kubernetes or otherwise), skip Ansible and use the GitOps layer on its own — it only needs a kubeconfig. See [Using an existing cluster](./docs/03-installation.md#using-an-existing-cluster).

It is a quick starting point for simple infrastructure needs or for testing various tools such as monitoring, alerting, automated deployment, security testing, etc...

## Documentation

__Website:__ <https://this-is-tobi.com/homelab/introduction>.

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
- Pick (or create) an instance under [./argo-cd/instances/](./argo-cd/instances/) (e.g. `homelab/`):
  - [`instance.yaml`](./argo-cd/instances/homelab/instance.yaml) — cluster destination, env, repos, project bindings.
  - [`core.yaml`](./argo-cd/instances/homelab/core.yaml) — platform/core tier app catalog.
  - [`tenant.yaml`](./argo-cd/instances/homelab/tenant.yaml) — user-facing apps catalog.
- Configure values in [./argo-cd/instances/homelab/values/core/](./argo-cd/instances/homelab/values/core/) and [./argo-cd/instances/homelab/values/tenant/](./argo-cd/instances/homelab/values/tenant/).
- For a brand-new instance, copy the documented template at [./argo-cd/instances/_example/](./argo-cd/instances/_example/) (folders prefixed with `_` are excluded by the root manager and treated as templates). See [Installation > Tier-flexible apps](./docs/03-installation.md#tier-flexible-apps) for apps (ingress, cert-manager, keycloak, prometheus-stack, ...) that can live in either tier depending on topology.

__Install:__
```sh
# Set kubectl context
kubectl config use-context homelab

# Bootstrap (or upgrade) the homelab instance
./run.sh -b homelab
```

> *Optionally pass `ARGOCD_ADMIN_PASSWORD=mypass` before the command to set the ArgoCD admin password explicitly. Otherwise the chart auto-generates one and the script prints it at the end.*
