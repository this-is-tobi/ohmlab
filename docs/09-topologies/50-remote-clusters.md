# Remote clusters

The core Argo CD, the management instance's, deploys every instance of the repository to its own cluster: an instance names its cluster in `destination.server`, and the core Argo CD needs credentials for each cluster but its own.

## Registering a cluster

The management instance's `ohmlab` values list the clusters, by name:

```yaml
clusters:
  nonprod:
    server: https://nonprod.example.com:6443
vso:
  enabled: true
```

With `vso.enabled`, each becomes the Argo CD cluster Secret `cluster-<name>`, which the Vault Secrets Operator writes from the core Argo CD's own Vault path (the `ohmlab` app's: `<system mount>/platforms/<env>/core/argo-cd`), keys `clusters.<name>.bearerToken` and `clusters.<name>.caData`. Seed both empty on the hub, then set them from a service account on the cluster:

```bash
ctx=nonprod   # the cluster's kube context
kubectl --context "$ctx" -n kube-system create serviceaccount argocd-manager
kubectl --context "$ctx" create clusterrolebinding argocd-manager \
  --clusterrole cluster-admin --serviceaccount kube-system:argocd-manager
kubectl --context "$ctx" -n kube-system apply -f - <<'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: argocd-manager-token
  annotations:
    kubernetes.io/service-account.name: argocd-manager
type: kubernetes.io/service-account-token
EOF
# The cluster's entry under clusters, the other clusters' kept.
vault kv get -mount=ohmlab -format=json platforms/production/core/argo-cd \
  | jq --arg t "$(kubectl --context "$ctx" -n kube-system get secret argocd-manager-token -o jsonpath='{.data.token}' | base64 -d)" \
       --arg ca "$(kubectl --context "$ctx" -n kube-system get secret argocd-manager-token -o jsonpath='{.data.ca\.crt}')" \
       --arg name "$ctx" \
       '{clusters: (.data.data.clusters + {($name): {bearerToken: $t, caData: $ca}})}' \
  | vault kv patch -mount=ohmlab platforms/production/core/argo-cd -
```

The service account is cluster-admin, as the instances deploy their cluster's layer, CRDs and webhooks included; the projects below are what bounds the core Argo CD there. The controller never overwrites a key once set, so the values stay.

## One server string per cluster

Applications, projects and `clusters` name a cluster by its API server URL, and Argo CD and the checks compare those URLs as strings: give each cluster one, the same in every `instance.yaml`, project and `clusters` entry. Two strings for one cluster (`https://kubernetes.default.svc` and its external URL) would make it two clusters to `check-shared-clusters.sh`, which then could not see two of its instances collide.

## Projects per instance

The base projects allow the in-cluster server only. Each remote instance names projects of its own in `instance.yaml` (`projects.core`, `projects.tenant`), which the management instance's `ohmlab` values declare with `inherit`: a project that takes a base project's sources, whitelists and blacklists, with destinations of its own.

```yaml
projects:
  dev-tenant:
    enabled: true
    inherit: tenant
    name: dev-tenant
    destinations:
    - server: https://nonprod.example.com:6443
      namespace: "dev-*"
```

Argo CD refuses any Application of the instance outside that cluster and those namespaces. `check-app-projects.sh` checks every enabled entry against its project before a change merges.

## Network paths

- **The core Argo CD to each API server.** With `networkPolicy.egressDeny` in the management instance's `ohmlab` values, allow the servers' addresses and port, for instance:

```yaml
networkPolicy:
  extraEgress:
  - name: remote-clusters
    rules:
    - to:
      - ipBlock:
          cidr: 198.51.100.10/32
      ports:
      - protocol: TCP
        port: 6443
```

- **Each remote cluster to the hub's Vault**, through its route and allowlist ([a remote Vault](40-remote-vault.md#reaching-the-hub)).
- **The hub's Vault to each managed cluster's issuer**, over HTTPS, where Vault discovers the cluster's keys.

## Managed clusters

A managed cluster comes with its CNI and its storage, so its layer runs neither `cilium`, `longhorn` nor `system-upgrade`:

- **Storage and scheduling:** some charts name the `longhorn` StorageClass or select nodes labelled `node-type: worker`, and a managed cluster has neither. Unset them, as the examples do: `vault.storage.raft.storageClass: ""` and `vault.nodeSelector: null` in `vault-operator`, `rustfs.storageclass.name: ""`, and `cnpg.cluster.affinity.nodeSelector: null` for each app's database. The volumes then take the cluster's default class.
- **Gateway API:** install its CRDs first: the bootstrap installs them on the core cluster only. The Gateway's wildcard certificates need a DNS-01 issuer of the domain's DNS provider.
- **Load balancer and client address:** Traefik's Service is a LoadBalancer, which the provider backs. Allowlists and anything else that reads the client's address need Traefik to see it: PROXY protocol from the load balancer, or `externalTrafficPolicy: Local`.
