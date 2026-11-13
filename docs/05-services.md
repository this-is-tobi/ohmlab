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

Wireguard's web interface lets you create / delete / activate / deactivate VPN users, download their configuration file and display the user's QrCode. With this user configuration file, a user can access the local network to perform an ssh connection to the machines and then request the Kubernetes api server.

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

The app catalog under [argo-cd/apps/](../argo-cd/apps/) provides the following charts; each instance enables its own subset via the `enabled` flag in its [core.yaml](../argo-cd/instances/_example/core.yaml) / [tenant.yaml](../argo-cd/instances/_example/tenant.yaml) catalogs :

| Name                                                                              | Description                                                                                                  | Helm chart                                                                                                                                                                                                |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Actions-runner-controller](https://github.com/actions/actions-runner-controller) | Github Actions runners controller                                                                            | [actions-runner-controller/actions-runner-controller](https://artifacthub.io/packages/helm/actions-runner-controller/actions-runner-controller)                                                           |
| [Apisix](https://apisix.apache.org/)                                              | API gateway and Gateway API implementation, an alternative to Traefik                                        | [apisix/apisix](https://artifacthub.io/packages/helm/apisix/apisix)                                                                                                                                       |
| [ArgoCD](https://argo-cd.readthedocs.io/en/stable/)                               | GitOps continuous delivery tool                                                                              | [argo/argo-cd](https://artifacthub.io/packages/helm/argo/argo-cd)                                                                                                                                         |
| [Argo-workflows](https://argoproj.github.io/workflows/)                           | Workflow automation engine                                                                                   | [argo/argo-workflows](https://artifacthub.io/packages/helm/argo/argo-workflows)                                                                                                                           |
| [Cert-manager](https://cert-manager.io/)                                          | Cloud native certificate management                                                                          | [cert-manager/cert-manager](https://artifacthub.io/packages/helm/cert-manager/cert-manager)                                                                                                               |
| [Cilium](https://cilium.io/)                                                      | eBPF-based CNI with NetworkPolicy and Hubble                                                                 | [cilium/cilium](https://artifacthub.io/packages/helm/cilium/cilium)                                                                                                                                       |
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
| [Trust-manager](https://cert-manager.io/docs/trust/trust-manager/)                | Publishes Vault's CA as a ConfigMap for Traefik's backend TLS validation                                     | [cert-manager/trust-manager](https://artifacthub.io/packages/helm/cert-manager/trust-manager)                                                                                                             |
| [Vault](https://www.vaultproject.io/)                                             | Secret management service (standalone chart)                                                                 | [hashicorp/vault](https://artifacthub.io/packages/helm/hashicorp/vault)                                                                                                                                   |
| [Vault-operator](https://bank-vaults.dev/)                                        | Bank-Vaults operator (HA Vault cluster) + [VSO](https://developer.hashicorp.com/vault/docs/platform/k8s/vso) | [bank-vaults/vault-operator](https://artifacthub.io/packages/helm/bank-vaults/vault-operator) + [hashicorp/vault-secrets-operator](https://artifacthub.io/packages/helm/hashicorp/vault-secrets-operator) |
| [Vaultwarden](https://github.com/dani-garcia/vaultwarden)                         | Password management service                                                                                  | [vaultwarden/vaultwarden](https://artifacthub.io/packages/helm/vaultwarden/vaultwarden)                                                                                                                   |

> *__Node prerequisite:__ SonarQube's search engine needs `vm.max_map_count` of at least 262144 on the nodes that run it, or it fails its bootstrap checks and the pod restarts in a loop. The `common` / `hardening` Ansible role sets it. The chart's init container that used to do it ran privileged, which the baseline policy refuses, so it is off.*

### Versions

All services helm charts and versions are managed through ArgoCD ApplicationSets with configuration stored in:
- **App charts**: [./argo-cd/apps/](../argo-cd/apps/) — each app has its own `Chart.yaml` defining the chart version and dependencies.
- **Per-instance metadata**: [./argo-cd/instances/\<instance\>/instance.yaml](../argo-cd/instances/_example/instance.yaml) — cluster destination, env, repos, AppProject bindings.
- **Per-instance app catalog**: [./argo-cd/instances/\<instance\>/core.yaml](../argo-cd/instances/_example/core.yaml) and [tenant.yaml](../argo-cd/instances/_example/tenant.yaml) — enable/disable apps + per-app overrides (sync wave, namespace, release name, ...).
- **Per-instance values**: [./argo-cd/instances/\<instance\>/values/core/\<app\>.yaml](../argo-cd/instances/_example/values/core/) and [tenant/\<app\>.yaml](../argo-cd/instances/_example/values/tenant/) — values overrides applied on top of the chart defaults.

### Management

Services are managed by a **two-level** ApplicationSet hierarchy declared by the `ohmlab` chart in the `argocd-system` namespace:

- The root `manager` AppSet discovers each instance folder and emits one Application per instance pointing at the `instance-manager` chart.
- That chart renders two child AppSets per instance — `core-<instance>` (platform tier, bound to `admin-core` AppProject) and `tenant-<instance>` (apps tier, bound to `admin-tenant` AppProject).

To enable or disable a service, edit the relevant entry in [argo-cd/instances/\<instance\>/core.yaml](../argo-cd/instances/_example/core.yaml) or [tenant.yaml](../argo-cd/instances/_example/tenant.yaml) and flip `enabled: "true"` / `enabled: "false"`.

```mermaid
flowchart LR
    subgraph git["Git repository"]
        direction TB
        subgraph instFolder["argo-cd/instances/&lt;instance&gt;/"]
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
        instApp["Application: instance-mylab"]
        subgraph childAS["child AppSets"]
            direction LR
            coreAS["core-mylab<br/>(AppProject: admin-core)"]
            tenantAS["tenant-mylab<br/>(AppProject: admin-tenant)"]
        end
        subgraph leafApps["leaf Applications"]
            direction LR
            coreApp["mylab-&lt;coreApp&gt;"]
            tenantApp["mylab-&lt;tenantApp&gt;"]
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

#### Git webhooks

Argo CD polls Git every three minutes. No chart exposes an ApplicationSet webhook or opens the ApplicationSet controller's `webhook` port. For a push to trigger a sync at once, the instance:

1. Adds a gateway route to the controller's Service on port 7000: `argocd-applicationset-controller` for the core Argo CD, `<release>-argocd-applicationset-controller` for a tenant one.
2. Admits its gateway's namespace to that port with a `networkPolicy.extraIngress` group (the core Argo CD's ohmlab chart and the tenant `argo-cd` chart both take one; it renders only with `networkPolicy.enabled: true`, like the chart's other policies): `{name: appset-webhook, podSelector: {matchLabels: {app.kubernetes.io/name: argocd-applicationset-controller}}, rules: [{from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: <gateway namespace>}}}], ports: [{port: webhook, protocol: TCP}]}]}`.
3. Sets a webhook secret, the `webhook.github.secret` key of `argocd-secret`.
4. Adds the webhook to the Git repository, with that secret.

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

> Select the realm: `ohmlab` for the core services (Grafana, Longhorn, Hubble, the core Argo CD, Vault), the instance's realm, named after it, for the tenant services.

A default `admin` group grants admin-level access on every connected service; users not in this group get standard access.

```mermaid
flowchart LR
    user([User])
    kc[Keycloak<br/>realms: ohmlab, &lt;instance&gt;]
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

### In-cluster TLS

Traffic between pods is meant to be TLS wherever it carries credentials or data. Today, every app's CNPG PostgreSQL cluster serves TLS and refuses plaintext clients (`hostnossl all all all reject` in its `pg_hba`), and every client connects with `sslmode=require`; Gitea verifies Valkey's certificate and authenticates with a password; Vault and argocd-server serve TLS that traefik verifies upstream via BackendTLSPolicy (Vault against its own CA, Argo CD against its Let's Encrypt certificate); Teleport terminates TLS itself behind a Gateway API TLSRoute passthrough.

Two opt-in values give the cluster one internal CA for the rest, and the example instance turns both on:

- `internalCA.enabled` in the `cert-manager` app: a self-signed root in cert-manager's namespace (10 years, renewed a year early with the same key) and the ClusterIssuer `ohmlab-internal-ca` on it.
- `internalCaBundle.enabled` in the `trust-manager` app, which needs the first: the root as the ConfigMap `ohmlab-internal-ca` (key `ca.crt`) in every namespace listed in `trust-manager.app.targetNamespaces`, the only ones trust-manager may write to. It reads the root from an anchor certificate in its own namespace, so it never reads cert-manager's keys.

Each bundle reaches every target namespace unless its `namespaces` lists some of them (`internalCaBundle.namespaces`, `vaultCaBundle.namespaces`): list only the namespaces whose clients read it. trust-manager can create, change and delete every ConfigMap of a target namespace, so add a namespace only where a client needs the CA.

An app gets its serving certificate from the issuer (cert-manager's 90-day default applies), and its clients trust the ConfigMap:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-app-tls
spec:
  secretName: my-app-tls
  dnsNames:
  - my-app.my-namespace.svc
  issuerRef:
    kind: ClusterIssuer
    name: ohmlab-internal-ca
```

The `gitea` app takes this issuer for Valkey's certificate with `valkeyCertificate.issuerRef: {kind: ClusterIssuer, name: ohmlab-internal-ca}`; otherwise a CA of Gitea's namespace issues it. Gitea reads that CA when it starts: after changing the issuer of a running Valkey, restart Gitea once Valkey serves the new certificate. The CA reaches Gitea through three lists of the app's values, `gitea.deployment.env` (`SSL_CERT_DIR`), `gitea.extraVolumes` and `gitea.extraContainerVolumeMounts`: an instance that sets one of them replaces the catalog's entries and must keep them, or Gitea refuses Valkey's certificate when it starts. Mounts still set through the chart's deprecated `gitea.extraVolumeMounts` no longer reach Gitea's container, which reads only `extraContainerVolumeMounts` once it is set: move them there.

The `argo-cd` app (the tenant Argo CD) serves its Redis over TLS by default (`argo-cd.redis.tls.enabled: false` turns it off), which needs cert-manager. Redis then speaks TLS only, and the Argo CD server, repo-server and application controller verify it (the ApplicationSet controller does not use Redis). Its certificate comes from a CA of the app's namespace, or from this issuer with `redisCertificate.issuerRef: {kind: ClusterIssuer, name: ohmlab-internal-ca}`. Redis reads its certificate only when it starts, so a `tls-reload` sidecar makes it read each renewal; an instance that sets `argo-cd.redis.extraContainers` replaces the catalog's list and must keep that sidecar. The three components read the CA when they start: after changing the issuer of a running install, or if its namespace CA is re-created, restart them once Redis serves the new certificate. This covers the single node Redis, not the core Argo CD's redis-ha.

The `keycloak` app serves Keycloak over TLS with `keycloak.tls.enabled: true` and `keycloak.keycloak.httpsEnabled: true`, set together. Its certificate, for the Service's names, comes from `certificate.issuerRef` (this issuer by default), and Keycloak reloads it every hour. Keycloak then serves HTTPS on 8443 beside HTTP on 8080, its health and metrics go over HTTPS, and keycloak-config-cli verifies it once `keycloakConfigCli.env.KEYCLOAK_URL` is an https URL such as `https://keycloak:8443`, which the render asks for. Once one of the app's routes reaches the Service's `https` port, the app adds a BackendTLSPolicy that makes the gateway verify Keycloak against the ConfigMap `certificate.caConfigMap` (`ohmlab-internal-ca` by default). The policy comes with that route: Argo CD holds a sync until the policy has a status, which only a route using it gives. To switch: list Keycloak's namespace in `trust-manager.app.targetNamespaces` first, and in `internalCaBundle.namespaces` when that lists namespaces (without the ConfigMap, the gateway drops the route), turn TLS on, point the route's `backendPort` and the gateway's NetworkPolicy rule at 8443, then close HTTP with `keycloak.keycloak.httpEnabled: false`. An instance that sets `keycloakConfigCli.extraEnv`, `extraVolumes` or `extraVolumeMounts` replaces the catalog's lists and must keep their `KEYCLOAK_TLS_*` and `keycloak-tls` entries.

The other gateway backends follow one shape. The switch is a value of the app's own chart, and the app adds a certificate from its `issuerRef` (this issuer by default) for the Service's names, plus a BackendTLSPolicy once one of its routes reaches the Service's port. The certificate syncs before the pods that mount it (sync wave -1), except in the `cilium` and `longhorn` apps, which sync before cert-manager: there it comes with the pods, and the proxy restarts until it is issued. With progressive sync, turn on their TLS only once cert-manager runs. The policy syncs after the rollout (wave 1). These servers serve HTTP or HTTPS, never both, so the switch is one step with a short error window: where the Service keeps its port and the rollout starts new pods first (Gitea, Grafana), the gateway keeps reaching the old pods over HTTP until the policy lands, and errors only while it reaches new pods without it; the oauth2-proxies move to another port and RustFS stops its old pod first, so they are unreachable until the new pod is ready. The policy verifies the app against the ConfigMap its `caConfigMap` names (`ohmlab-internal-ca` by default), which must be in the app's namespace (`trust-manager.app.targetNamespaces`, and `internalCaBundle.namespaces` when that lists namespaces). An empty `caConfigMap` uses the `ca.crt` of the app's own certificate Secret instead: no trust bundle in the namespace, but it needs a gateway that reads Secrets there, as Traefik does, and after a change of the CA's root the gateway trusts a backend only once it serves its new certificate.

The `gitea` app serves Gitea over TLS with `gitea.gitea.config.server.PROTOCOL: https` and `gitea.gitea.metrics.serviceMonitor.scheme: https`, set together: the scrape uses Gitea's one listener and does not verify it. The certificate is `gitea-tls` (`certificate.issuerRef`, `certificate.caConfigMap`), and the route stays on port 3000. Gitea reads its certificate only when it starts: list `gitea-tls` in the controller's `certificates` target ([08-controller.md](08-controller.md#certificate-restarts)), which restarts Gitea after each renewal, or restart it yourself. During the switch, pushes handled by old pods can fail from the moment the first new pod starts until the old pods stop, a few minutes since Gitea starts slowly: their hooks read the shared `app.ini`, which the new pods have already moved to https. An instance that sets `gitea.extraVolumes` or `gitea.extraContainerVolumeMounts` replaces the catalog's lists and must keep their `gitea-tls` entries, which the render checks.

The `prometheus-stack` app serves Grafana over TLS once an instance sets this recipe, since Grafana's chart has no switch of its own; the render fails on any part left out:

```yaml
kube-prometheus-stack:
  grafana:
    grafana.ini:
      server: {protocol: https, cert_file: /etc/grafana/tls/tls.crt, cert_key: /etc/grafana/tls/tls.key, certs_watch_interval: 1h}
    extraSecretMounts: [{name: tls, mountPath: /etc/grafana/tls, secretName: grafana-tls, readOnly: true}]
    livenessProbe: {httpGet: {scheme: HTTPS}}
    readinessProbe: {httpGet: {scheme: HTTPS}}
    serviceMonitor: {scheme: https, tlsConfig: {insecureSkipVerify: true}}
    sidecar:
      dashboards: {reloadURL: "https://localhost:3000/api/admin/provisioning/dashboards/reload", env: {REQ_SKIP_TLS_VERIFY: "true"}}
      datasources: {reloadURL: "https://localhost:3000/api/admin/provisioning/datasources/reload", env: {REQ_SKIP_TLS_VERIFY: "true"}}
```

The app issues the certificate into the Secret of the `tls` mount (`grafanaCertificate.issuerRef`, `grafanaCertificate.caConfigMap`), and Grafana reloads it every `certs_watch_interval`. The sidecars reload the dashboards and datasources through localhost, so they skip verification, and so does the scrape (or give it a `tlsConfig.ca`). The route stays on the Service's port 80. Other mounts an instance sets in `extraSecretMounts` go beside the `tls` entry, in the same list.

The `rustfs` app serves the S3 API and the console over TLS with `rustfs.tls.enabled: true`, in standalone mode: one certificate (`rustfs-tls`, `certificate.issuerRef`, `certificate.caConfigMap`) serves both listeners, RustFS reloads it on renewal, and one BackendTLSPolicy covers both ports once a route reaches either. The routes keep their ports. The rollout stops the old pod before the new one starts, since the data volume is ReadWriteOnce, so S3 and the console are down for a minute or two. Clients that go through the gateway see no change; an in-cluster client that reaches the Service directly needs the CA (`AWS_CA_BUNDLE` for the AWS SDKs, `SSL_CERT_FILE` for Go, `NODE_EXTRA_CA_CERTS` for Node).

The `cilium` and `longhorn` apps serve their oauth2-proxy (Hubble UI's and Longhorn UI's sign-in) over TLS with `sso.httpScheme: https`, `sso.service.portNumber: 443` and `sso.service.appProtocol: https`. The proxy then serves HTTPS only, on container port 4443, from the certificate `<proxy>-tls` (`ssoCertificate.issuerRef`, `ssoCertificate.caConfigMap`). Point the route's `backendPort` at 443 and the gateway's NetworkPolicy rule at 4443 in the same change. The proxy reads its certificate only when it starts: list its certificate in the controller's `certificates` target ([08-controller.md](08-controller.md#certificate-restarts)), which restarts the proxy after each renewal, or restart it yourself. The hop from the proxy to the UI stays plain, inside one namespace. An instance that sets `sso.extraArgs` as a list, or `sso.extraVolumes` or `sso.extraVolumeMounts`, replaces the catalog's TLS entries, which the render checks; the TLS settings never go in `sso.config.configFile`, which instances replace whole.

`ohmlab audit tls` checks a running cluster against this ([cheat sheet](07-cheat-sheet.md#ohmlab-cli)): it probes every Service port through a port-forward and fails on a plaintext one that neither the rule (metrics, DNS) nor an exception of the instance's `ohmlab.yaml`, each with its reason, allows.

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

Each app's `VaultAuth` logs in with Kubernetes auth as the `vso` ServiceAccount of its namespace, under a Vault role bound to that ServiceAccount and namespace (roles in [argo-cd/apps/vault-operator/values.yaml](../argo-cd/apps/vault-operator/values.yaml)). VSO asks for that token with the audience `vault` (`kubernetes.audiences`), so the JWT Vault receives is not a Kubernetes API credential. Each role requires that audience (`audience: vault`): no other token of the ServiceAccount logs in, not even the API token of its pods. `scripts/ci/check-vault-access.sh` fails a `VaultAuth` whose role is missing, bound elsewhere or requires no audience, and one that does not request the role's audience.

VSO talks to Vault over TLS through the operator-wide `default` VaultConnection in `vault-operator-system`, which validates the server against the `vault-tls` secret (`defaultVaultConnection` in [argo-cd/apps/vault-operator/values.yaml](../argo-cd/apps/vault-operator/values.yaml)). No app `VaultAuth` sets `vaultConnectionRef`, and VSO falls back to that `default` connection when it is unset. Each app's `vso-utils` values still render a per-namespace `<release>-vso-default` VaultConnection pointing at the local `vault-tls` secret (the `ca.crt`-only copy bank-vaults writes into every namespace listed in the Vault resource's `caNamespaces`, `["*"]` here; an instance that restricts it must include every namespace holding a VaultConnection), but no `VaultAuth` references those connections: an invalid one only produces VSO error events and does not stop secret delivery. Traefik validates Vault's own certificate through a `BackendTLSPolicy`, which only accepts a ConfigMap CA reference: the `trust-manager` app publishes it as `vault-ca-bundle` in `vault-operator-system`, read from the same `vault-tls` secret. When secrets do stop syncing, `ohmlab check` reports the failing `VaultStaticSecret` resources first.

Each app also gets a dedicated least-privilege Vault policy
(read-only: `ohmlab/data/platforms/+/core/<app>` in the system mount, `<instance>/data/platforms/+/tenant/<app>` in the instance's) and a Kubernetes auth role
bound to the `vso` ServiceAccount **in the app's own namespace** — a role
bound to the wrong namespace fails with `403 namespace not authorized`.

Values that cannot be generated are declared as empty placeholders under `ohmlab.vault.secrets` and set by hand in Vault. The ohmlab controller keeps a key that exists, generates one that is missing, and leaves a placeholder declared as `""` for a person to fill in; to regenerate keys, set `rotate.token` on the entry ([docs/08-controller.md](08-controller.md)). The GitHub App of the runner scale sets is one: put its `id`, `installationId` and `privateKey` (the PEM) in a JSON file and patch them in, so the key never lands in the shell history, then delete the file. VSO builds the `github-app` Secret that every scale set references from them.

```sh
vault kv patch -mount=<instance> platforms/production/tenant/actions-runner-controller - < github-app.json
# {"githubApp": {"id": "...", "installationId": "...", "privateKey": "-----BEGIN RSA PRIVATE KEY-----\n..."}}
```

#### Multi-cluster (spoke clusters)

Everything above assumes the Vault Secrets Operator and Vault run in the same cluster, the default. A Vault can also serve instances on other clusters, which read it with the jwt method: [a remote Vault](09-topologies/40-remote-vault.md).

### Security policies

The main Kyverno policies guarding admissions (see
[argo-cd/apps/kyverno/templates/](../argo-cd/apps/kyverno/templates/)):

| Policy                    | Action  | Notes                                                                                          |
| ------------------------- | ------- | ---------------------------------------------------------------------------------------------- |
| `pod-security-baseline`   | Enforce | PSS baseline; infra namespaces needing hostPath excluded                                       |
| `require-non-root`        | Enforce | runAsNonRoot; infra namespaces whose pods need root excluded (crowdsec, longhorn, ...)         |
| `disallow-latest-tag`     | Enforce | `:latest` blocked; `kube-system` and `kyverno` excluded                                        |
| `require-resource-limits` | Audit   | stays Audit — blocking operator-created pods unexpectedly is worse than a report               |
| `verify-images`           | Enforce | this repo's images must be signed by its release workflow, with provenance naming this repo    |

Every policy is a CEL object of the `policies.kyverno.io` group (`ValidatingPolicy`, `ImageValidatingPolicy`, `GeneratingPolicy` and `PolicyException`), the types that replace `ClusterPolicy` and the legacy `PolicyException` Kyverno 1.20 removes; CI refuses a chart that renders the old ones. `Enforce` in the table is the `failureAction` value and means `validationActions: [Deny]`, `Audit` means `[Audit]`. The policies scope themselves inside their expressions instead of with a namespace selector: each policy with its own selector gets its own Kyverno webhook, which costs one more admission call per pod and policy. Policy results are the `kyverno_validating_policy_results_total` metric and reports with source `KyvernoValidatingPolicy`; the Kyverno dashboard and the `KyvernoPolicyDenials` alert read them.

Actions are configurable per instance via `policies.<name>.failureAction` in
the kyverno app values. Exceptions are GitOps-managed: the kyverno
`PolicyException` feature is enabled but restricted to the `kyverno`
namespace (a CEL `PolicyException` anywhere else is ignored), so every exception lives in
[policy-exceptions.yaml](../argo-cd/apps/kyverno/templates/policy-exceptions.yaml)
and workloads cannot self-exempt. In-cluster encryption is described in [In-cluster TLS](#in-cluster-tls).

Each built-in exception covers one app's pods in that app's default namespace. An instance that runs the app in another namespace of its cluster, under a namespace prefix or beside another instance, lists that namespace under the exception's name in the kyverno values' `exceptionNamespaces`, such as `rustfs-init-step-limits: [dev-rustfs, preprod-rustfs]`. The exception then covers that namespace too, with the same kinds, name prefix and image conditions. Two exceptions grant host access: `system-upgrade-jobs-baseline` lets the k3s upgrade Jobs use the host namespaces and host paths, and run privileged from the k3s-upgrade image, and `trivy-node-collector-baseline` lets trivy's node collectors see the node's processes and read its files. In a namespace they cover, whoever can create a pod named with their prefix (`apply-`, `node-collector-`) gets that access, so list only a namespace that app alone uses.

### Backups

Each CNPG app's Postgres cluster and Vault back up to an in-cluster RustFS store, from the claims the catalog gives the apps: WAL continuously and a daily base backup for Postgres, a daily raft snapshot for Vault, each kept 30 days. An instance turns them on with its `storage` block in `instance.yaml`; see [Object storage](10-object-storage.md).

### Monitoring

The cluster itself and the platform services are monitored using [Prometheus](https://prometheus.io/) and [Grafana](https://grafana.com/). Every component that exposes metrics ships a `ServiceMonitor`/`PodMonitor` (ArgoCD, Vault, Traefik, cert-manager, CrowdSec, Longhorn, Gitea, Keycloak, CNPG, sops, ...).

Every app that has a dashboard ships it in its own `dashboards/` folder: a template renders one ConfigMap per file, and Grafana's sidecar loads them when the app's `monitoring.dashboards.enabled` is true. The cluster-wide Kubernetes dashboards are the exception: they live in [argo-cd/apps/prometheus-stack/grafana-dashboards/](../argo-cd/apps/prometheus-stack/grafana-dashboards/) and load through the [dashboards.yaml](../argo-cd/apps/prometheus-stack/templates/dashboards.yaml) template of that app, where more can be added. The dashboards shipped:

| App                                                                      | Dashboards                                                              |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| [ohmlab](../charts/ohmlab/dashboards/)                                   | Argo CD                                                                 |
| [cert-manager](../argo-cd/apps/cert-manager/dashboards/)                 | cert-manager                                                            |
| [cloudnative-pg](../argo-cd/apps/cloudnative-pg/dashboards/)             | CloudNativePG                                                           |
| [gitea](../argo-cd/apps/gitea/dashboards/)                               | Gitea                                                                   |
| [kyverno](../argo-cd/apps/kyverno/dashboards/)                           | Kyverno                                                                 |
| [longhorn](../argo-cd/apps/longhorn/dashboards/)                         | Longhorn                                                                |
| [ohmlab-controller](../argo-cd/apps/ohmlab-controller/dashboards/)       | Ohmlab controller                                                       |
| [prometheus-stack](../argo-cd/apps/prometheus-stack/grafana-dashboards/) | K3s, Kubernetes (global, nodes, namespaces, pods), Loki Kubernetes logs |
| [teleport](../argo-cd/apps/teleport/dashboards/)                         | Teleport                                                                |
| [traefik](../argo-cd/apps/traefik/dashboards/)                           | Traefik                                                                 |
| [vault-operator](../argo-cd/apps/vault-operator/dashboards/)             | Hashicorp Vault                                                         |

#### Alerting

Alertmanager posts every alert except the always-firing `Watchdog` to a Mattermost incoming webhook in the channel `alerts`, through its Slack-compatible receiver (`kube-prometheus-stack.alertmanager.config` in [argo-cd/apps/prometheus-stack/values.yaml](../argo-cd/apps/prometheus-stack/values.yaml)). Mattermost generates the hook id, so the webhook URL cannot be seeded: create the channel and an incoming webhook locked to it in Mattermost, then store the URL once by hand in the Vault field `alertmanagerMattermostWebhookUrl` (mount `ohmlab`, the system mount of the core tier, path `platforms/production/core/prometheus-stack`) with `vault kv patch`, never `vault kv put`, which would replace the whole path and wipe the Grafana, Keycloak and Postgres credentials stored next to it:

```sh
printf '%s' 'http://mattermost.mattermost.svc.cluster.local:8065/hooks/<id>' | vault kv patch -mount=ohmlab platforms/production/core/prometheus-stack alertmanagerMattermostWebhookUrl=-
```

Use the in-cluster Service form of the URL shown above rather than the public one: delivery then doesn't depend on Traefik or the public hostname, and the Mattermost NetworkPolicy admits Alertmanager on that port. VSO delivers the field to the `alertmanager-mattermost` secret within its hourly refresh, and Alertmanager reads it on every notification, so no restart is needed. While the field is empty or missing, every notification fails (`unsupported protocol scheme ""` in the Alertmanager logs) and no alert reaches anyone.
