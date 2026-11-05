# A service provider's instances

An instance repository for a provider that runs the platform for its customers, each on a cluster of its own:

| Instance | Cluster | Tiers | Runs |
| --- | --- | --- | --- |
| `provider` | core (in-cluster) | core | the core Argo CD with the root manager, Vault as the hub, the ohmlab controller, Keycloak for the provider's admins, cert-manager, trust-manager, Traefik, CloudNativePG, Kyverno, prometheus-stack |
| `customer-a` | customer-a | core and tenant | the cluster layer (cert-manager, Traefik, the Vault Secrets Operator alone, CloudNativePG, Kyverno, Prometheus and Alertmanager), its own Keycloak (realm `customer-a`), Gitea, Mattermost, RustFS, Vaultwarden |
| `customer-b` | customer-b | core and tenant | the same, realm `customer-b` |

The core Argo CD deploys every instance. Each customer reads its own KV mount on the provider's Vault, through a jwt mount that validates its cluster's tokens alone: a customer's cluster, keys, mounts, policies and Argo CD project are its own. `customer-a` runs on a managed cluster whose issuer publishes its keys, which Vault fetches; `customer-b` runs on a cluster without a public issuer, whose keys are pinned. The product's CI checks this folder against its own charts whenever they change.

## Use it

1. Copy this folder into a repository of your own, and set its URL in every `instance.yaml` (`repoURL`) and in `provider/values/core/ohmlab.yaml` (`rootManager.repoURL.values`). The root manager finds the instances there, and takes its own chart from the catalog bundle (`rootManager.repoURL.sources`), as the instances do.
2. Replace the placeholders:
   - the domains (`provider.example.com`, `customer-a.example.net`, `customer-b.example.net`), the cluster API servers and the git server (`git.example.org`);
   - in `provider/values/core/vault-operator.yaml`: `customer-a`'s OIDC issuer, at its provider (`oidcDiscoveryUrl`, `oidc.example.org` here), which the provider's Vault must reach over HTTPS, and `customer-b`'s public keys (`publicKeys`, from `kubectl get --raw /openid/v1/jwks` on that cluster, each key as PEM), to pin again when the cluster rotates them;
   - the customers' outbound addresses and the admins' network (`gateway.sourceRange` there, documentation ranges here), the only sources the Vault route admits;
   - the catalog version each instance pins (`catalogs.release.version`), and the root manager's (`rootManager.sourcesRevision`).
3. On every cluster, give cert-manager a DNS-01 issuer named `letsencrypt-dns-prod`, of the provider's or the customer's DNS provider: the Gateways' wildcard certificates need it, and on the core cluster Argo CD's own. On each customer's cluster, also install the Gateway API CRDs: the bootstrap installs them on the core cluster only.
4. Bootstrap the core cluster from a checkout of the product: `./run.sh -b provider -i <this repository>/argo-cd/instances`. The root manager then picks up the customers' instances.
5. Create a service account for Argo CD on each customer's cluster, and set its bearer token and the cluster's CA (base64 PEM) in Vault at `ohmlab/platforms/production/core/argo-cd`, keys `clusters.<cluster>.bearerToken` and `clusters.<cluster>.caData`: the seeds create both empty, and Argo CD reaches a cluster once they are set.
6. Set the other values seeded empty by hand: each cluster's Alertmanager webhook (`<customer>/platforms/production/core/prometheus-stack`, and the provider's own on `ohmlab`).

To add a customer, copy one of the customer folders under a new name, and give it a spoke cluster, an Argo CD project per tier, a cluster entry and its seeds on the provider.

## Read more

- [The service provider topology](../../docs/09-topologies/30-service-provider.md), which walks through this folder.
- [The example instance](../../argo-cd/instances/_example), which documents every field these files set.
