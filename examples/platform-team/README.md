# A platform team's instances

An instance repository for a company's platform team, which runs one management cluster and two managed clusters (CNI and storage come with them):

| Instance | Cluster | Tiers | Runs |
| --- | --- | --- | --- |
| `platform` | core (in-cluster) | core | the core Argo CD with the root manager, Vault as the hub, the ohmlab controller, Keycloak for the admins, cert-manager, trust-manager, Traefik, CloudNativePG, Kyverno, prometheus-stack |
| `nonprod` | nonprod | core | the cluster layer: cert-manager, Traefik (one Gateway for both environments), the Vault Secrets Operator alone, CloudNativePG, Kyverno, Prometheus and Alertmanager |
| `dev` | nonprod | tenant | Keycloak (realm `dev`), Gitea, Mattermost, RustFS, Vaultwarden, in namespaces prefixed `dev-` |
| `preprod` | nonprod | tenant | the same, prefixed `preprod-` |
| `prod` | prod | core and tenant | the cluster layer, Keycloak (realm `prod`) and the same apps |

The core Argo CD deploys every instance; each remote instance reads its own KV mount on the platform's Vault, through a jwt mount that validates its cluster's tokens. The product's CI checks this folder against its own charts whenever they change.

## Use it

1. Copy this folder into a repository of your own, and set its URL in every `instance.yaml` (`repoURL`) and in `platform/values/core/ohmlab.yaml` (`rootManager.repoURL.values`). The root manager finds the instances there, and takes its own chart from the catalog bundle (`rootManager.repoURL.sources`), as the instances do.
2. Replace the placeholders:
   - the domains (`example.com` and its subdomains), the cluster API servers (`https://nonprod.example.com:6443`, `https://prod.example.com:6443`) and the git server (`git.example.org`);
   - each managed cluster's OIDC issuer, at its provider (`spokeClusters[].oidcDiscoveryUrl` in `platform/values/core/vault-operator.yaml`, `oidc.example.org` here), which the platform's Vault must reach over HTTPS;
   - the outbound addresses of the remote clusters and the admins' network (`gateway.sourceRange` there, documentation ranges here), the only sources the Vault route admits;
   - the catalog version each instance pins (`catalogs.release.version`), and the root manager's (`rootManager.sourcesRevision`).
3. On every cluster, give cert-manager a DNS-01 issuer of your DNS provider named `letsencrypt-dns-prod`: the Gateways' wildcard certificates need it, and on the core cluster Argo CD's own. On each managed cluster, also install the Gateway API CRDs: the bootstrap installs them on the core cluster only.
4. Bootstrap the core cluster from a checkout of the product: `./run.sh -b platform -i <this repository>/argo-cd/instances`. The root manager then picks up the other instances.
5. Create a service account for Argo CD on each remote cluster, and set its bearer token and the cluster's CA (base64 PEM) in Vault at `ohmlab/platforms/production/core/argo-cd`, keys `clusters.<cluster>.bearerToken` and `clusters.<cluster>.caData`: the seeds create both empty, and Argo CD reaches a cluster once they are set.
6. Set the other values seeded empty by hand: each cluster's Alertmanager webhook (`<instance>/platforms/<env>/core/prometheus-stack`, and the platform's own on `ohmlab`).

## Promote a catalog version

Each instance pins its own catalog version, so a new release reaches `dev` first: raise `catalogs.release.version` in `dev/instance.yaml`, then in `preprod`, then in `prod` once the previous one runs it cleanly.

## Read more

- [The platform team topology](../../docs/09-topologies/20-platform-team.md), which walks through this folder.
- [The example instance](../../argo-cd/instances/_example), which documents every field these files set.
