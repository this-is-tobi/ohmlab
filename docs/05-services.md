# Services

## Gateway

### Haproxy

[HAProxy](https://www.haproxy.org/) is a free and open source software that provides a high availability load balancer and reverse proxy for TCP and HTTP-based applications that spreads requests across multiple servers.

Haproxy load-balances all incoming http and https traffic from the Internet (ports 80 and 443) onto ports 80/443 of every cluster node (workers preferred, masters as backup), in TCP/TLS-passthrough mode — TLS terminates at traefik inside the cluster. How those node ports reach traefik depends on your [networking choices](./04-infrastructure.md#networking-choices): either K3s ServiceLB publishing them as host ports, or kube-proxy DNAT via the traefik Service's `externalIPs` list. It also load-balances Kubernetes api server traffic on the local network (port 6443) onto the master nodes. An ACL rule is defined to accept only local network IP address requests for the api server.

The web interface lets you view the health status of every backend node on both types of endpoints (server api and internet traffic).

### Pi-Hole

[Pi-hole](https://pi-hole.net/) is a Linux network-level advertisement and Internet tracker blocking application which acts as a DNS sinkhole and optionally a DHCP server, intended for use on a private network. It is designed for low-power embedded devices with network capability, such as the Raspberry Pi, but can be installed on almost any Linux machine.

Pi-hole has the ability to block traditional website advertisements as well as advertisements in unconventional places, such as smart TVs and mobile operating system advertisements.

Using the web interface, you can enable/disable ad and tracker blocking, add a list of domains to be blocked, and configure local network DNS settings (and DHCP if required). It is also possible to view statistics on blocked domains according to the privacy rules set.

### Wireguard

[WireGuard](https://www.wireguard.com/) is a communication protocol and free and open-source software that implements encrypted virtual private networks (VPNs), and was designed with the goals of ease of use, high speed performance, and low attack surface.

Wireguard's web interface lets you create / delete / activate / deactivate VPN users, download their configuration file and display the user's QrCode. With this user configuration file, a user can access the homelab network to perform an ssh connection to the machines and then request the Kubernetes api server.

The gateway role deploys [wg-easy](https://github.com/wg-easy/wg-easy) v15: the admin account and network settings are seeded from the inventory (`wireguard.*`) on the container's first start and afterwards live in wg-easy's own database — later changes are made in the UI (or by wiping `/services/wireguard/.wg-easy`). The UI is bound to `127.0.0.1:51821` on the gateway; reach it through an SSH tunnel (`ssh -L 51821:127.0.0.1:51821 <gateway>`). Every `adminUsers` entry gets a VPN client of the same name; `state: absent` deletes it.

### CrowdSec

[CrowdSec](https://www.crowdsec.net/) is an open-source security engine that analyses logs from various sources (HAProxy, sshd, syslog) and detects malicious behaviour using community-curated scenarios. Detected attackers are blocked at the network level via an nftables firewall bouncer.

The gateway deployment runs the CrowdSec engine as a Docker container, watching the gateway host's own logs:
- `crowdsecurity/linux` — SSH brute force, bad user agents, port scans
- `crowdsecurity/sshd` — SSH-specific attack patterns

HTTP-level collections are deliberately not installed here: this HAProxy runs in `mode tcp` (pure L4 passthrough), so its logs carry no HTTP fields for an HTTP parser to match. HTTP detection happens in-cluster instead, against traefik's own access logs.

A separate Kubernetes deployment (CrowdSec Helm chart) provides cluster-wide detection, parsing the traefik JSON access logs (see `agent.additionalAcquisition` in [argo-cd/apps/crowdsec/values.yaml](../argo-cd/apps/crowdsec/values.yaml) for how the agent reads them).

### Access

Gateway web interface services are deployed and accessible for admin purpose, they are available on local network at :

| Name                | Url                         |
| ------------------- | --------------------------- |
| Haproxy dashboard   | <http://192.168.1.99:8404>  |
| Pihole dashboard    | <http://192.168.1.99:5353>  |
| Wireguard dashboard | <http://192.168.1.99:51821> |

> *__Notes:__ Replace `192.168.1.99` with the gateway's ip address set in [hosts.yml](../ansible/inventory-example/hosts.yml).*

## Kubernetes

### Services

The app catalog under [argo-cd/apps/](../argo-cd/apps/) provides the following charts; each instance enables its own subset via the `enabled` flag in its [core.yaml](../argo-cd/instances/homelab/core.yaml) / [tenant.yaml](../argo-cd/instances/homelab/tenant.yaml) catalogs :

| Name                                                                              | Description                                                                                                  | Helm chart                                                                                                                                                                                                |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Actions-runner-controller](https://github.com/actions/actions-runner-controller) | Github Actions runners controller                                                                            | [actions-runner-controller/actions-runner-controller](https://artifacthub.io/packages/helm/actions-runner-controller/actions-runner-controller)                                                           |
| [ArgoCD](https://argo-cd.readthedocs.io/en/stable/)                               | GitOps continuous delivery tool                                                                              | [argo/argo-cd](https://artifacthub.io/packages/helm/argo/argo-cd)                                                                                                                                         |
| [Argo-workflows](https://argoproj.github.io/workflows/)                           | Workflow automation engine                                                                                   | [argo/argo-workflows](https://artifacthub.io/packages/helm/argo/argo-workflows)                                                                                                                           |
| [Cert-manager](https://cert-manager.io/)                                          | Cloud native certificate management                                                                          | [cert-manager/cert-manager](https://artifacthub.io/packages/helm/cert-manager/cert-manager)                                                                                                               |
| [Cloud-native-postgres](https://cloudnative-pg.io/)                               | Cloud native postgres database management                                                                    | [cnpg/cloudnative-pg](https://artifacthub.io/packages/helm/cloudnative-pg/cloudnative-pg)                                                                                                                 |
| [Coder](https://coder.com/)                                                       | Remote selfhosted development environments                                                                   | [coder-v2/coder](https://artifacthub.io/packages/helm/coder-v2/coder)                                                                                                                                     |
| [CrowdSec](https://www.crowdsec.net/)                                             | Open-source security engine & threat detection                                                               | [crowdsec/crowdsec](https://artifacthub.io/packages/helm/crowdsec/crowdsec)                                                                                                                               |
| [Homepage](https://gethomepage.dev/)                                              | Home dashboard                                                                                               | [this-is-tobi/homepage](https://github.com/this-is-tobi/helm-charts/tree/main/charts/homepage)                                                                                                            |
| [Gitea](https://about.gitea.com/)                                                 | Private, Fast, Reliable DevOps Platform                                                                      | [gitea/gitea](https://artifacthub.io/packages/helm/gitea/gitea)                                                                                                                                           |
| [Harbor](https://goharbor.io/)                                                    | Cloud native registry                                                                                        | [bitnami/harbor](https://artifacthub.io/packages/helm/bitnami/harbor)                                                                                                                                     |
| [Traefik](https://doc.traefik.io/traefik/)                                        | Ingress controller & Gateway API implementation                                                              | [traefik/traefik](https://artifacthub.io/packages/helm/traefik/traefik)                                                                                                                                   |
| [Keycloak](https://keycloak.org)                                                  | Single Sign On service                                                                                       | [cloudpirates/keycloak](https://artifacthub.io/packages/helm/cloudpirates/keycloak)                                                                                                                       |
| [Kyverno](https://kyverno.io/)                                                    | Kubernetes policy engine (admission control)                                                                 | [kyverno/kyverno](https://artifacthub.io/packages/helm/kyverno/kyverno)                                                                                                                                   |
| [Longhorn](https://longhorn.io/)                                                  | Cloud native distributed block storage                                                                       | [longhorn/longhorn](https://artifacthub.io/packages/helm/longhorn/longhorn)                                                                                                                               |
| [Mattermost](https://mattermost.com/)                                             | Chat service with file sharing and integrations                                                              | [mattermost/mattermost-team-edition](https://artifacthub.io/packages/helm/mattermost/mattermost-team-edition)                                                                                             |
| [MLflow](https://mlflow.org/)                                                     | ML experiment tracking and model registry                                                                    | [community-charts/mlflow](https://artifacthub.io/packages/helm/community-charts/mlflow)                                                                                                                   |
| [Ohmlab-controller](08-controller.md)                                             | Keeps Vault (later Harbor and SonarQube) configured from Kubernetes objects                                  | -                                                                                                                                                                                                         |
| [Outline](https://www.getoutline.com/)                                            | Share notes and wiki with your team                                                                          | [community-charts/outline](https://artifacthub.io/packages/helm/community-charts/outline)                                                                                                                 |
| [Prometheus-stack](https://prometheus.io/)                                        | Open-source monitoring solution                                                                              | [prometheus-community/kube-prometheus-stack](https://artifacthub.io/packages/helm/prometheus-community/kube-prometheus-stack)                                                                             |
| [RustFS](https://rustfs.com/)                                                     | High Performance Object Storage                                                                              | -                                                                                                                                                                                                         |
| [Sonarqube](https://www.sonarsource.com/products/sonarqube/)                      | Code quality analysis service                                                                                | [sonarqube/sonarqube](https://artifacthub.io/packages/helm/sonarqube/sonarqube)                                                                                                                           |
| [Sops](https://github.com/isindir/sops-secrets-operator)                          | Secret manager that decode on the fly                                                                        | [sops-secrets-operator/sops-secrets-operator](https://artifacthub.io/packages/helm/sops-secrets-operator/sops-secrets-operator)                                                                           |
| [System-upgrade-controller](https://github.com/rancher/system-upgrade-controller) | K3S upgrade controller                                                                                       | -                                                                                                                                                                                                         |
| [Teleport](https://goteleport.com/)                                               | Secure access and identity for infrastructure                                                                | [teleport/teleport-cluster](https://artifacthub.io/packages/helm/teleport/teleport-cluster)                                                                                                               |
| [Trivy-operator](https://aquasecurity.github.io/trivy-operator/latest/)           | Kubernetes-native security toolkit                                                                           | [aqua/trivy-operator](https://aquasecurity.github.io/helm-charts/)                                                                                                                                        |
| [Vault](https://www.vaultproject.io/)                                             | Secret management service (standalone chart)                                                                 | [hashicorp/vault](https://artifacthub.io/packages/helm/hashicorp/vault)                                                                                                                                   |
| [Vault-operator](https://bank-vaults.dev/)                                        | Bank-Vaults operator (HA Vault cluster) + [VSO](https://developer.hashicorp.com/vault/docs/platform/k8s/vso) | [bank-vaults/vault-operator](https://artifacthub.io/packages/helm/bank-vaults/vault-operator) + [hashicorp/vault-secrets-operator](https://artifacthub.io/packages/helm/hashicorp/vault-secrets-operator) |
| [Vaultwarden](https://github.com/dani-garcia/vaultwarden)                         | Password management service                                                                                  | [vaultwarden/vaultwarden](https://artifacthub.io/packages/helm/vaultwarden/vaultwarden)                                                                                                                   |

### Versions

All services helm charts and versions are managed through ArgoCD ApplicationSets with configuration stored in:
- **App charts**: [./argo-cd/apps/](../argo-cd/apps/) — each app has its own `Chart.yaml` defining the chart version and dependencies.
- **Per-instance metadata**: [./argo-cd/instances/\<instance\>/instance.yaml](../argo-cd/instances/homelab/instance.yaml) — cluster destination, env, repos, AppProject bindings.
- **Per-instance app catalog**: [./argo-cd/instances/\<instance\>/core.yaml](../argo-cd/instances/homelab/core.yaml) and [tenant.yaml](../argo-cd/instances/homelab/tenant.yaml) — enable/disable apps + per-app overrides (sync wave, namespace, release name, ...).
- **Per-instance values**: [./argo-cd/instances/\<instance\>/values/core/\<app\>.yaml](../argo-cd/instances/homelab/values/core/) and [tenant/\<app\>.yaml](../argo-cd/instances/homelab/values/tenant/) — values overrides applied on top of the chart defaults.

### Management

Services are managed by a **two-level** ApplicationSet hierarchy declared by the `ohmlab` chart in the `argocd-system` namespace:

- The root `manager` AppSet discovers each instance folder and emits one Application per instance pointing at the `instance-manager` chart.
- That chart renders two child AppSets per instance — `core-<instance>` (platform tier, bound to `admin-core` AppProject) and `tenant-<instance>` (apps tier, bound to `admin-tenant` AppProject).

To enable or disable a service, edit the relevant entry in [argo-cd/instances/homelab/core.yaml](../argo-cd/instances/homelab/core.yaml) or [tenant.yaml](../argo-cd/instances/homelab/tenant.yaml) and flip `enabled: "true"` / `enabled: "false"`.

```mermaid
flowchart LR
    subgraph git["Git repository"]
        direction TB
        subgraph instFolder["argo-cd/instances/homelab/"]
            direction TB
            subgraph valsTree["values/"]
                direction LR
                coreVals["core/&lt;app&gt;.yaml"]
                tenantVals["tenant/&lt;app&gt;.yaml"]
            end
            subgraph catalogs["catalogs"]
                direction LR
                coreCat["core.yaml"]
                tenantCat["tenant.yaml"]
            end
            instYaml["instance.yaml"]
        end
        chart["argo-cd/apps/&lt;app&gt;/<br/>(Chart.yaml + templates)"]
    end

    subgraph engine["core ArgoCD (argocd-system)"]
        direction TB
        rootAS["AppSet: manager (root)"]
        instApp["Application: instance-homelab"]
        subgraph childAS["child AppSets"]
            direction LR
            coreAS["core-homelab<br/>(AppProject: admin-core)"]
            tenantAS["tenant-homelab<br/>(AppProject: admin-tenant)"]
        end
        subgraph leafApps["leaf Applications"]
            direction LR
            coreApp["homelab-&lt;coreApp&gt;"]
            tenantApp["homelab-&lt;tenantApp&gt;"]
        end
    end

    k8s[(Kubernetes resources)]

    %% Git → ArgoCD wiring
    rootAS -->|scans instances/*| instApp
    instYaml -->|configures| instApp
    instApp -->|emits| coreAS & tenantAS
    coreCat -->|drives| coreAS
    tenantCat -->|drives| tenantAS
    coreAS -->|one per enabled app| coreApp
    tenantAS -->|one per enabled app| tenantApp

    %% Per-app sources (chart + values)
    chart -. chart source .-> coreApp & tenantApp
    coreVals -. values override .-> coreApp
    tenantVals -. values override .-> tenantApp

    %% Sync to cluster
    coreApp & tenantApp -->|helm template + apply| k8s
```

### Access

Kubernetes services that are available through user interfaces are centralized on the [Homepage](https://gethomepage.dev/) dashboard. Platform (core) services live under the `core.` subdomain; tenant apps sit directly under the root domain :

#### Core (platform)

| Name            | Url                                |
| --------------- | ---------------------------------- |
| ArgoCD *(core)* | <https://gitops.core.domain.com>   |
| Keycloak        | <https://sso.core.domain.com>      |
| Longhorn        | <https://longhorn.core.domain.com> |
| Teleport        | <https://teleport.core.domain.com> |
| Vault           | <https://vault.core.domain.com>    |

#### Tenant (apps)

| Name               | Url                             |
| ------------------ | ------------------------------- |
| ArgoCD             | <https://gitops.domain.com>     |
| Gitea              | <https://git.domain.com>        |
| Grafana            | <https://monitoring.domain.com> |
| Homepage           | <https://domain.com>            |
| Mattermost         | <https://mattermost.domain.com> |
| RustFS *- api*     | <https://s3.domain.com>         |
| RustFS *- console* | <https://console.s3.domain.com> |

> *__Notes:__ Replace `domain.com` by your own domain configured in your values files. Optional catalog apps (Coder, Harbor, Outline, SonarQube, Vaultwarden, ...) follow the same pattern when enabled.*

### Single sign on

[Keycloak](https://keycloak.org/) is deployed as the cluster single sign-on tool. It provides a single account (username / password pair) that grants access to multiple services, and propagates user groups to control access levels.

Users and access levels are managed via the Keycloak interface (cf. [keycloak service url](#kubernetes)) using the admin credentials from Vault (`keycloak.username` / `keycloak.password` under the `keycloak` secret path).

> Don't forget to select the `homelab` realm.

A default `admin` group grants admin-level access on every connected service; users not in this group get standard access.

```mermaid
flowchart LR
    user([User])
    kc[Keycloak<br/>realm: homelab]
    subgraph apps[Connected services]
        argo[ArgoCD]
        gitea[Gitea]
        graf[Grafana]
        lh[Longhorn]
        rustfs[RustFS]
        vault[Vault]
    end
    user -->|login| apps
    apps -->|OIDC redirect| kc
    kc -->|id_token + groups| apps
```

Services currently connected through SSO (client secrets are stored in Vault and delivered by VSO):
- ArgoCD (core + tenant instances)
- Gitea
- Grafana
- Longhorn
- RustFS (console OIDC): the `rustfs` client emits a flat `rustfs_policy` claim from its client roles, and RustFS uses its values as policy names. Members of `/admin` get the `consoleAdmin` client role (full admin); any other realm user has no RustFS policy and is denied. RustFS discovers the provider only at startup, so if the *Keycloak* button is missing from the console login page, restart the RustFS pod once Keycloak is reachable. The static root credential keeps working either way.
- Vault

Optional catalog apps (Coder, Harbor, Outline, SonarQube, ...) ship with the same Keycloak OIDC wiring and join the list when enabled.

### Secrets

Secrets are sourced from Vault and synced into Kubernetes by the [Vault Secrets Operator](https://developer.hashicorp.com/vault/docs/platform/k8s/vso) (VSO). Each chart that needs secrets depends on the `vso-utils` subchart, which renders `VaultStaticSecret` custom resources pointing at a Vault path.

```mermaid
flowchart LR
    vault[(HashiCorp Vault)]
    vso[Vault Secrets Operator]
    vss[VaultStaticSecret CR<br/>per app]
    secret[Kubernetes Secret]
    pod([App pod])

    vss -->|watched by| vso
    vso -->|reads| vault
    vso -->|materializes| secret
    pod -->|envFrom / volume| secret
```

VSO talks to Vault over TLS through the operator-wide `default` VaultConnection in `vault-operator-system`, which validates the server against the `vault-tls` secret (`defaultVaultConnection` in [argo-cd/apps/vault-operator/values.yaml](../argo-cd/apps/vault-operator/values.yaml)). No app `VaultAuth` sets `vaultConnectionRef`, and VSO falls back to that `default` connection when it is unset. Each app's `vso-utils` values still render a per-namespace `<release>-vso-default` VaultConnection pointing at the local `vault-tls` secret (the `ca.crt`-only copy bank-vaults writes into every namespace listed in the Vault resource's `caNamespaces`, `["*"]` here; an instance that restricts it must include every namespace holding a VaultConnection), but no `VaultAuth` references those connections: an invalid one only produces VSO error events and does not stop secret delivery. Traefik validates Vault's own certificate through a `BackendTLSPolicy`, which only accepts a ConfigMap CA reference: the `trust-manager` app publishes it as `vault-ca-bundle` in `vault-operator-system`, read from the same `vault-tls` secret. When secrets do stop syncing, `ohmlab check` reports the failing `VaultStaticSecret` resources first.

Each app also gets a dedicated least-privilege Vault policy
(`homelab/data/platforms/+/+/<app>`, read-only) and a Kubernetes auth role
bound to the `vso` ServiceAccount **in the app's own namespace** — a role
bound to the wrong namespace fails with `403 namespace not authorized`.

Values that cannot be generated are declared as empty placeholders under `ohmlab.vault.secrets` and set by hand in Vault. The ohmlab controller keeps a key that exists, generates one that is missing, and leaves a placeholder declared as `""` for a person to fill in; to regenerate keys, set `rotate.token` on the entry ([docs/08-controller.md](08-controller.md)). The GitHub App of the runner scale sets is one: put its `id`, `installationId` and `privateKey` (the PEM) in a JSON file and patch them in, so the key never lands in the shell history, then delete the file. VSO builds the `github-app` Secret that every scale set references from them.

```sh
vault kv patch -mount=homelab platforms/production/tenant/actions-runner-controller - < github-app.json
# {"githubApp": {"id": "...", "installationId": "...", "privateKey": "-----BEGIN RSA PRIVATE KEY-----\n..."}}
```

#### Multi-cluster (spoke clusters)

Everything above assumes VSO and Vault live in the same cluster — the default, and what every instance in this repo runs today. Vault can additionally act as a **hub** that separate Kubernetes clusters pull their own secrets from, via `vault.externalConfig.spokeClusters` in [argo-cd/apps/vault-operator/values.yaml](../argo-cd/apps/vault-operator/values.yaml) (shape documented, commented out, in [argo-cd/instances/_example/values/core/vault-operator.yaml](../argo-cd/instances/_example/values/core/vault-operator.yaml)). Not enabled on any instance in this repo; nothing changes until an instance sets it.

A **spoke cluster** here means a separate physical/virtual Kubernetes cluster — for instance a `tenant`-tier instance ([Tier-flexible apps](03-installation.md#tier-flexible-apps)) deployed as its own cluster rather than the tenant tier of the hub's own instance. That is a different axis from the existing core/tenant **tier** split within one instance (above); a spoke cluster's apps can sit in either tier.

```mermaid
flowchart LR
    subgraph hub[Hub cluster]
        vault[(HashiCorp Vault)]
    end
    subgraph spoke[Spoke cluster]
        vso[Vault Secrets Operator]
        secret[Kubernetes Secret]
        pod([App pod])
    end

    vso -->|1: jwt login, pinned pubkeys| vault
    vso -->|2: read own path only| vault
    vso -->|materializes| secret
    pod -->|envFrom / volume| secret
```

The reason this needs a different Vault auth method, not just another `kubernetes`-auth role: that method validates a token by calling **TokenReview on the cluster that issued it**, so the hub would need a network route *into* every spoke's API server — the direction that does not survive NAT, firewalls, or a spoke cluster that comes and goes. `jwt` auth validates the token's signature locally against public keys pinned per spoke, so the hub never calls back into it. Isolation between spokes is therefore cryptographic (mismatched signature → rejected), not based on `bound_issuer` — Kubernetes gives every cluster the same default issuer, so that claim alone would tell spokes apart in name only.

### Security policies

The main Kyverno policies guarding admissions (see
[argo-cd/apps/kyverno/templates/](../argo-cd/apps/kyverno/templates/)):

| Policy                    | Action  | Notes                                                                                          |
| ------------------------- | ------- | ---------------------------------------------------------------------------------------------- |
| `pod-security-baseline`   | Enforce | PSS baseline; infra namespaces needing hostPath excluded                                       |
| `require-non-root`        | Enforce | runAsNonRoot; nginx/log-reader namespaces excluded                                             |
| `disallow-latest-tag`     | Enforce | `:latest` blocked; `kube-system` and `kyverno` excluded                                        |
| `require-resource-limits` | Audit   | stays Audit — blocking operator-created pods unexpectedly is worse than a report               |
| `verify-images`           | Enforce | this repo's images must be signed by its release workflow, with provenance naming this repo    |

Every policy is a CEL object of the `policies.kyverno.io` group (`ValidatingPolicy`, `ImageValidatingPolicy`, `GeneratingPolicy` and `PolicyException`), the types that replace `ClusterPolicy` and the legacy `PolicyException` Kyverno 1.20 removes; CI refuses a chart that renders the old ones. `Enforce` in the table is the `failureAction` value and means `validationActions: [Deny]`, `Audit` means `[Audit]`. The policies scope themselves inside their expressions instead of with a namespace selector: each policy with its own selector gets its own Kyverno webhook, which costs one more admission call per pod and policy. Policy results are the `kyverno_validating_policy_results_total` metric and reports with source `KyvernoValidatingPolicy`; the Kyverno dashboard and the `KyvernoPolicyDenials` alert read them.

Actions are configurable per instance via `policies.<name>.failureAction` in
the kyverno app values. Exceptions are GitOps-managed: the kyverno
`PolicyException` feature is enabled but restricted to the `kyverno`
namespace (a CEL `PolicyException` anywhere else is ignored), so every exception lives in
[policy-exceptions.yaml](../argo-cd/apps/kyverno/templates/policy-exceptions.yaml)
and workloads cannot self-exempt. In-cluster traffic is encrypted wherever the
component supports it: CNPG PostgreSQL serves TLS and every client connects
with `sslmode=require`; gitea⇄valkey uses password auth over TLS; Vault and
argocd-server serve TLS that traefik verifies upstream via BackendTLSPolicy
(Vault against its own CA, ArgoCD against its Let's Encrypt cert); Teleport
terminates TLS itself behind a Gateway API TLSRoute passthrough.

### Monitoring

The cluster itself and the platform services are monitored using [Prometheus](https://prometheus.io/) and [Grafana](https://grafana.com/). Every component that exposes metrics ships a `ServiceMonitor`/`PodMonitor` (ArgoCD, Vault, Traefik, cert-manager, CrowdSec, Longhorn, Gitea, Keycloak, CNPG, sops, ...) — roughly 30 monitors across the cluster.

Some dashboards are already delivered with the installation but more can be added in [argo-cd/apps/prometheus-stack/grafana-dashboards/](../argo-cd/apps/prometheus-stack/grafana-dashboards/), they will be automatically loaded on ArgoCD synchronization via the [dashboards.yaml](../argo-cd/apps/prometheus-stack/templates/dashboards.yaml) template. Already added dashboards are:

| Dashboard file                                                                                 | Grafana dashboard ID                                                                                          |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| [argo-cd.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/argo-cd.json)               | [14584](https://grafana.com/grafana/dashboards/14584-argo-cd/)                                                |
| [cert-manager.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/cert-manager.json)     | [20340](https://grafana.com/grafana/dashboards/20340-cert-manager/)                                           |
| [cloudnative-pg.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/cloudnative-pg.json) | [20417](https://grafana.com/grafana/dashboards/20417-cloudnativepg/)                                          |
| [gitea.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/gitea.json)                   | [17802](https://grafana.com/grafana/dashboards/17802-gitea-dashbaord/)                                        |
| [harbor.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/harbor.json)                 | *- ( [source](https://github.com/goharbor/harbor/blob/main/contrib/grafana-dashboard/metrics-example.json) )* |
| [k3s.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/k3s.json)                       | [15282](https://grafana.com/grafana/dashboards/15282-k8s-rke-cluster-monitoring/)                             |
| [kube-global.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/kube-global.json)       | [15757](https://grafana.com/grafana/dashboards/15757-kubernetes-views-global/)                                |
| [kube-node.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/kube-node.json)           | [15759](https://grafana.com/grafana/dashboards/15759-kubernetes-views-nodes/)                                 |
| [kube-ns.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/kube-ns.json)               | [15758](https://grafana.com/grafana/dashboards/15758-kubernetes-views-namespaces/)                            |
| [kube-pod.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/kube-pod.json)             | [15760](https://grafana.com/grafana/dashboards/15760-kubernetes-views-pods/)                                  |
| [longhorn.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/longhorn.json)             | [13032](https://grafana.com/grafana/dashboards/13032-longhorn-example-v1-1-0/)                                |
| [trivy.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/trivy.json)                   | [16337](https://grafana.com/grafana/dashboards/16337-trivy-operator-vulnerabilities/)                         |
| [vault.json](../argo-cd/apps/prometheus-stack/grafana-dashboards/vault.json)                   | [12904](https://grafana.com/grafana/dashboards/12904-hashicorp-vault/)                                        |

#### Alerting

Alertmanager posts every alert except the always-firing `Watchdog` to a Mattermost incoming webhook in the channel `alerts`, through its Slack-compatible receiver (`kube-prometheus-stack.alertmanager.config` in [argo-cd/apps/prometheus-stack/values.yaml](../argo-cd/apps/prometheus-stack/values.yaml)). Mattermost generates the hook id, so the webhook URL cannot be seeded: create the channel and an incoming webhook locked to it in Mattermost, then store the URL once by hand in the Vault field `alertmanagerMattermostWebhookUrl` (mount `homelab`, path `platforms/production/core/prometheus-stack`) with `vault kv patch`, never `vault kv put`, which would replace the whole path and wipe the Grafana, Keycloak and Postgres credentials stored next to it:

```sh
printf '%s' 'http://mattermost.mattermost.svc.cluster.local:8065/hooks/<id>' | vault kv patch -mount=homelab platforms/production/core/prometheus-stack alertmanagerMattermostWebhookUrl=-
```

Use the in-cluster Service form of the URL shown above rather than the public one: delivery then doesn't depend on Traefik or the public hostname, and the Mattermost NetworkPolicy admits Alertmanager on that port. VSO delivers the field to the `alertmanager-mattermost` secret within its hourly refresh, and Alertmanager reads it on every notification, so no restart is needed. While the field is empty or missing, every notification fails (`unsupported protocol scheme ""` in the Alertmanager logs) and no alert reaches anyone.
