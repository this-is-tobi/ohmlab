# Homelab :alembic:

This project aims to build a homelab for personal testing on infrastructure, development, CI/CD, etc...

It provides a complete configuration with common web services using:
- **Ansible** for infrastructure deployment (gateway and [k3s](https://k3s.io) cluster)
- **GitOps** (ArgoCD) for Kubernetes applications deployment

It is a quick starting point for simple infrastructure needs or for testing various tools such as monitoring, alerting, automated deployment, security testing, etc...

## Quickstart

Make sure all prerequisites are met (check the installation section if needed).

__Setup directory:__
```sh
# Clone the repository
git clone --depth 1 https://github.com/this-is-tobi/ohmlab.git && cd ./ohmlab && rm -rf ./.git && git init

# Copy inventory example to inventory
cp -R ./ansible/inventory-example ./ansible/inventory
```

### Infrastructure

__Setup inventory:__
- [hosts.yml](../ansible/inventory-example/hosts.yml)
- [all.yml](../ansible/inventory-example/group_vars/all.yml)
- [gateway.yml](../ansible/inventory-example/group_vars/gateway.yml)
- [k3s.yml](../ansible/inventory-example/group_vars/k3s.yml)

__Install:__
```sh
# Deploy gateway and K3s cluster, fetch kubeconfig
./run.sh -p ./ansible/install.yml -u -k
```

### Kubernetes Services (GitOps)

__Setup configuration:__
- Pick (or create) an instance under [./argo-cd/instances/](../argo-cd/instances/) (e.g. `homelab/`):
  - [`instance.yaml`](../argo-cd/instances/_example/instance.yaml) — cluster destination, env, repos, project bindings.
  - [`core.yaml`](../argo-cd/instances/_example/core.yaml) — platform/core tier app catalog.
  - [`tenant.yaml`](../argo-cd/instances/_example/tenant.yaml) — user-facing apps catalog.
- Configure values in [./argo-cd/instances/\<instance\>/values/core/](../argo-cd/instances/_example/values/core/) and [./argo-cd/instances/\<instance\>/values/tenant/](../argo-cd/instances/_example/values/tenant/).
- For a brand-new instance, copy the documented template at [./argo-cd/instances/_example/](../argo-cd/instances/_example/) (folders prefixed with `_` are excluded by the root manager and treated as templates). See `Installation > Tier-flexible apps` for apps (ingress, cert-manager, keycloak, prometheus-stack, ...) that can live in either tier depending on topology.

__Install:__
```sh
# Set kubectl context
kubectl config use-context homelab

# Bootstrap (or upgrade) the homelab instance
./run.sh -b homelab
```

> *Optionally pass `ARGOCD_ADMIN_PASSWORD=mypass` before the command to set the ArgoCD admin password explicitly. Otherwise the chart auto-generates one and the script prints it at the end.*
