# ohmlab

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

A Helm chart to deploy ohmlab.

**Homepage:** <https://this-is-tobi.com/homelab>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| this-is-tobi | <this-is-tobi@proton.me> | <https://this-is-tobi.com> |

## Source Code

* <https://github.com/this-is-tobi/homelab>

## Requirements

| Name | Repository |
|------|------------|
| argo-cd | https://argoproj.github.io/argo-helm |
| vso-utils (as vso) | https://this-is-tobi.github.io/helm-charts |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| argo-cd.applicationSet.enabled | bool | `true` |  |
| argo-cd.applicationSet.metrics.enabled | bool | `true` |  |
| argo-cd.applicationSet.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.applicationSet.replicas | int | `2` |  |
| argo-cd.applicationSet.resources.limits.cpu | string | `"500m"` |  |
| argo-cd.applicationSet.resources.limits.memory | string | `"512Mi"` |  |
| argo-cd.applicationSet.resources.requests.cpu | string | `"50m"` |  |
| argo-cd.applicationSet.resources.requests.memory | string | `"128Mi"` |  |
| argo-cd.configs.cm."resource.customizations" | string | `"networking.k8s.io/Ingress:\n  health.lua: |\n    hs = {}\n    hs.status = \"Healthy\"\n    return hs\nohmlab.fr/VaultSeed:\n  health.lua: |\n    hs = {}\n    if obj.status == nil or obj.status.conditions == nil then\n      hs.status = \"Progressing\"\n      hs.message = \"Waiting for the ohmlab controller\"\n      return hs\n    end\n    -- The controller has not looked at this version of the spec yet.\n    if obj.status.observedGeneration ~= nil and obj.metadata.generation ~= nil and obj.status.observedGeneration < obj.metadata.generation then\n      hs.status = \"Progressing\"\n      hs.message = \"Waiting for the ohmlab controller to reconcile the latest spec\"\n      return hs\n    end\n    local ready = nil\n    for _, c in ipairs(obj.status.conditions) do\n      if c.type == \"Stalled\" and c.status == \"True\" then\n        hs.status = \"Degraded\"\n        hs.message = c.message\n        return hs\n      end\n      if c.type == \"Ready\" then ready = c end\n    end\n    if ready == nil then\n      hs.status = \"Progressing\"\n      hs.message = \"Waiting for the ohmlab controller\"\n      return hs\n    end\n    if ready.status == \"True\" then\n      hs.status = \"Healthy\"\n      hs.message = ready.message\n      return hs\n    end\n    -- Observe-only mode reports drift on purpose; it is not a failure.\n    if ready.reason == \"ObserveOnly\" then\n      hs.status = \"Healthy\"\n      hs.message = ready.message\n      return hs\n    end\n    hs.status = \"Degraded\"\n    hs.message = ready.message\n    return hs\n"` |  |
| argo-cd.configs.cm.url | string | `""` |  |
| argo-cd.configs.params."applicationsetcontroller.enable.progressive.syncs" | bool | `true` |  |
| argo-cd.configs.params."server.insecure" | bool | `false` |  |
| argo-cd.configs.rbac."policy.csv" | string | `"p, role:none, *, *, */*, deny\np, role:admin, *, *, */*, allow\ng, /admin, role:admin\n"` |  |
| argo-cd.configs.rbac."policy.default" | string | `"role:none"` |  |
| argo-cd.configs.rbac.scopes | string | `"[groups]"` |  |
| argo-cd.configs.secret.argocdServerAdminPassword | string | `""` |  |
| argo-cd.controller.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.labelSelector.matchLabels."app.kubernetes.io/name" | string | `"argocd-application-controller"` |  |
| argo-cd.controller.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.topologyKey | string | `"kubernetes.io/hostname"` |  |
| argo-cd.controller.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].weight | int | `100` |  |
| argo-cd.controller.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].labelSelector.matchLabels."app.kubernetes.io/name" | string | `"prometheus"` |  |
| argo-cd.controller.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" | string | `"prometheus-stack"` |  |
| argo-cd.controller.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey | string | `"kubernetes.io/hostname"` |  |
| argo-cd.controller.metrics.enabled | bool | `true` |  |
| argo-cd.controller.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.controller.replicas | int | `2` |  |
| argo-cd.controller.resources.limits.cpu | string | `"2000m"` |  |
| argo-cd.controller.resources.limits.memory | string | `"3Gi"` |  |
| argo-cd.controller.resources.requests.cpu | string | `"250m"` |  |
| argo-cd.controller.resources.requests.memory | string | `"1Gi"` |  |
| argo-cd.crds.install | bool | `true` |  |
| argo-cd.crds.keep | bool | `true` |  |
| argo-cd.dex.enabled | bool | `false` |  |
| argo-cd.enabled | bool | `true` | Enable ArgoCD core deployment |
| argo-cd.fullnameOverride | string | `"argocd"` |  |
| argo-cd.notifications.metrics.enabled | bool | `true` |  |
| argo-cd.notifications.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.notifications.replicas | int | `1` |  |
| argo-cd.notifications.resources.limits.cpu | string | `"250m"` |  |
| argo-cd.notifications.resources.limits.memory | string | `"256Mi"` |  |
| argo-cd.notifications.resources.requests.cpu | string | `"25m"` |  |
| argo-cd.notifications.resources.requests.memory | string | `"64Mi"` |  |
| argo-cd.redis-ha.enabled | bool | `true` |  |
| argo-cd.redis-ha.fullnameOverride | string | `"argocd-redis-ha"` |  |
| argo-cd.redis-ha.haproxy.init.resources.limits.cpu | string | `"100m"` |  |
| argo-cd.redis-ha.haproxy.init.resources.limits.memory | string | `"64Mi"` |  |
| argo-cd.redis-ha.haproxy.resources.limits.cpu | string | `"250m"` |  |
| argo-cd.redis-ha.haproxy.resources.limits.memory | string | `"256Mi"` |  |
| argo-cd.redis-ha.haproxy.resources.requests.cpu | string | `"25m"` |  |
| argo-cd.redis-ha.haproxy.resources.requests.memory | string | `"64Mi"` |  |
| argo-cd.redis-ha.init.resources.limits.cpu | string | `"100m"` |  |
| argo-cd.redis-ha.init.resources.limits.memory | string | `"64Mi"` |  |
| argo-cd.redis-ha.redis.resources.limits.cpu | string | `"500m"` |  |
| argo-cd.redis-ha.redis.resources.limits.memory | string | `"512Mi"` |  |
| argo-cd.redis-ha.redis.resources.requests.cpu | string | `"50m"` |  |
| argo-cd.redis-ha.redis.resources.requests.memory | string | `"128Mi"` |  |
| argo-cd.redis-ha.sentinel.resources.limits.cpu | string | `"200m"` |  |
| argo-cd.redis-ha.sentinel.resources.limits.memory | string | `"128Mi"` |  |
| argo-cd.redis-ha.sentinel.resources.requests.cpu | string | `"25m"` |  |
| argo-cd.redis-ha.sentinel.resources.requests.memory | string | `"32Mi"` |  |
| argo-cd.redis-ha.splitBrainDetection.resources.limits.cpu | string | `"1"` |  |
| argo-cd.redis-ha.splitBrainDetection.resources.limits.memory | string | `"64Mi"` |  |
| argo-cd.redis-ha.splitBrainDetection.resources.requests.cpu | string | `"10m"` |  |
| argo-cd.redis-ha.splitBrainDetection.resources.requests.memory | string | `"32Mi"` |  |
| argo-cd.redis.metrics.enabled | bool | `true` |  |
| argo-cd.redis.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.redisSecretInit.resources.limits.cpu | string | `"200m"` |  |
| argo-cd.redisSecretInit.resources.limits.memory | string | `"128Mi"` |  |
| argo-cd.redisSecretInit.resources.requests.cpu | string | `"10m"` |  |
| argo-cd.redisSecretInit.resources.requests.memory | string | `"32Mi"` |  |
| argo-cd.repoServer.autoscaling.enabled | bool | `false` |  |
| argo-cd.repoServer.autoscaling.maxReplicas | int | `5` |  |
| argo-cd.repoServer.autoscaling.minReplicas | int | `2` |  |
| argo-cd.repoServer.autoscaling.targetCPUUtilizationPercentage | int | `75` |  |
| argo-cd.repoServer.autoscaling.targetMemoryUtilizationPercentage | int | `75` |  |
| argo-cd.repoServer.metrics.enabled | bool | `true` |  |
| argo-cd.repoServer.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.repoServer.replicas | int | `2` |  |
| argo-cd.repoServer.resources.limits.cpu | string | `"1000m"` |  |
| argo-cd.repoServer.resources.limits.memory | string | `"1Gi"` |  |
| argo-cd.repoServer.resources.requests.cpu | string | `"100m"` |  |
| argo-cd.repoServer.resources.requests.memory | string | `"256Mi"` |  |
| argo-cd.server.autoscaling.enabled | bool | `false` |  |
| argo-cd.server.autoscaling.maxReplicas | int | `5` |  |
| argo-cd.server.autoscaling.minReplicas | int | `2` |  |
| argo-cd.server.autoscaling.targetCPUUtilizationPercentage | int | `75` |  |
| argo-cd.server.autoscaling.targetMemoryUtilizationPercentage | int | `75` |  |
| argo-cd.server.ingress.enabled | bool | `false` |  |
| argo-cd.server.metrics.enabled | bool | `true` |  |
| argo-cd.server.metrics.serviceMonitor.enabled | bool | `true` |  |
| argo-cd.server.replicas | int | `2` |  |
| argo-cd.server.resources.limits.cpu | string | `"500m"` |  |
| argo-cd.server.resources.limits.memory | string | `"512Mi"` |  |
| argo-cd.server.resources.requests.cpu | string | `"50m"` |  |
| argo-cd.server.resources.requests.memory | string | `"128Mi"` |  |
| coredns.clusterIP | string | `"10.43.0.10"` |  |
| coredns.enabled | bool | `false` |  |
| coredns.image | string | `"rancher/mirrored-coredns-coredns:1.15.0"` |  |
| coredns.nodeHosts | list | `[]` |  |
| coredns.replicas | int | `1` |  |
| coredns.resources.limits.memory | string | `"170Mi"` |  |
| coredns.resources.requests.cpu | string | `"100m"` |  |
| coredns.resources.requests.memory | string | `"70Mi"` |  |
| gateway.backendTLSPolicies | list | `[]` |  |
| gateway.enabled | bool | `false` |  |
| gateway.extraParentRefs | list | `[]` |  |
| gateway.gatewayName | string | `""` |  |
| gateway.gatewayNamespace | string | `""` |  |
| gateway.routes | list | `[]` |  |
| monitoring.dashboards.enabled | bool | `false` |  |
| monitoring.dashboards.folder | string | `"Services"` |  |
| networkPolicy.egressDeny | bool | `true` |  |
| networkPolicy.enabled | bool | `false` |  |
| networkPolicy.extraEgress | list | `[]` |  |
| networkPolicy.extraIngress | list | `[]` |  |
| networkPolicy.prometheusNamespace | string | `"prometheus-stack"` |  |
| networkPolicy.prometheusScrapePorts | list | `[]` |  |
| projects.core.clusterResourceBlacklist | list | `[]` |  |
| projects.core.clusterResourceWhitelist[0].group | string | `""` |  |
| projects.core.clusterResourceWhitelist[0].kind | string | `"Namespace"` |  |
| projects.core.clusterResourceWhitelist[10].group | string | `"apiregistration.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[10].kind | string | `"APIService"` |  |
| projects.core.clusterResourceWhitelist[11].group | string | `"cert-manager.io"` |  |
| projects.core.clusterResourceWhitelist[11].kind | string | `"*"` |  |
| projects.core.clusterResourceWhitelist[12].group | string | `"kyverno.io"` |  |
| projects.core.clusterResourceWhitelist[12].kind | string | `"*"` |  |
| projects.core.clusterResourceWhitelist[13].group | string | `"policies.kyverno.io"` |  |
| projects.core.clusterResourceWhitelist[13].kind | string | `"*"` |  |
| projects.core.clusterResourceWhitelist[14].group | string | `"policy"` |  |
| projects.core.clusterResourceWhitelist[14].kind | string | `"PodSecurityPolicy"` |  |
| projects.core.clusterResourceWhitelist[15].group | string | `"gateway.networking.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[15].kind | string | `"*"` |  |
| projects.core.clusterResourceWhitelist[16].group | string | `"cilium.io"` |  |
| projects.core.clusterResourceWhitelist[16].kind | string | `"*"` |  |
| projects.core.clusterResourceWhitelist[17].group | string | `"postgresql.cnpg.io"` |  |
| projects.core.clusterResourceWhitelist[17].kind | string | `"ClusterImageCatalog"` |  |
| projects.core.clusterResourceWhitelist[18].group | string | `"trust.cert-manager.io"` |  |
| projects.core.clusterResourceWhitelist[18].kind | string | `"Bundle"` |  |
| projects.core.clusterResourceWhitelist[1].group | string | `"rbac.authorization.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[1].kind | string | `"ClusterRole"` |  |
| projects.core.clusterResourceWhitelist[2].group | string | `"rbac.authorization.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[2].kind | string | `"ClusterRoleBinding"` |  |
| projects.core.clusterResourceWhitelist[3].group | string | `"apiextensions.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[3].kind | string | `"CustomResourceDefinition"` |  |
| projects.core.clusterResourceWhitelist[4].group | string | `"admissionregistration.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[4].kind | string | `"MutatingWebhookConfiguration"` |  |
| projects.core.clusterResourceWhitelist[5].group | string | `"admissionregistration.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[5].kind | string | `"ValidatingWebhookConfiguration"` |  |
| projects.core.clusterResourceWhitelist[6].group | string | `"scheduling.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[6].kind | string | `"PriorityClass"` |  |
| projects.core.clusterResourceWhitelist[7].group | string | `"storage.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[7].kind | string | `"StorageClass"` |  |
| projects.core.clusterResourceWhitelist[8].group | string | `"storage.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[8].kind | string | `"CSIDriver"` |  |
| projects.core.clusterResourceWhitelist[9].group | string | `"networking.k8s.io"` |  |
| projects.core.clusterResourceWhitelist[9].kind | string | `"IngressClass"` |  |
| projects.core.description | string | `"Homelab platform/core tier (infra, identity, observability, security)"` |  |
| projects.core.destinations[0].namespace | string | `"*"` |  |
| projects.core.destinations[0].server | string | `"https://kubernetes.default.svc"` |  |
| projects.core.enabled | bool | `true` | Render the `core` AppProject. |
| projects.core.extraSourceRepos | list | `[]` | Added to the derived `sourceRepos` (registries, other catalogs). |
| projects.core.name | string | `"admin-core"` | Project name (referenced by `argo-cd/instances/<inst>/instance.yaml.projects.core`). |
| projects.core.namespaceResourceBlacklist | list | `[]` |  |
| projects.core.namespaceResourceWhitelist[0].group | string | `"*"` |  |
| projects.core.namespaceResourceWhitelist[0].kind | string | `"*"` |  |
| projects.core.roles[0].description | string | `"Admin keycloak group"` |  |
| projects.core.roles[0].groups[0] | string | `"admin"` |  |
| projects.core.roles[0].name | string | `"admin"` |  |
| projects.core.roles[0].policies[0] | string | `"p, proj:admin-core:admin, applications, *, admin-core/*, allow"` |  |
| projects.core.sourceNamespaces | list | `[]` |  |
| projects.core.sourceRepos | list | `[]` | Repositories the project's Applications may take sources from. Empty: `rootManager.repoURL.sources`, `rootManager.repoURL.values` and `extraSourceRepos`, without duplicates. A bundle is listed with its `oci://` scheme, a registry path without one. |
| projects.tenant.clusterResourceBlacklist | list | `[]` |  |
| projects.tenant.clusterResourceWhitelist[0].group | string | `""` |  |
| projects.tenant.clusterResourceWhitelist[0].kind | string | `"Namespace"` |  |
| projects.tenant.clusterResourceWhitelist[1].group | string | `"networking.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[1].kind | string | `"Ingress"` |  |
| projects.tenant.clusterResourceWhitelist[2].group | string | `"networking.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[2].kind | string | `"IngressClass"` |  |
| projects.tenant.clusterResourceWhitelist[3].group | string | `"rbac.authorization.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[3].kind | string | `"ClusterRole"` |  |
| projects.tenant.clusterResourceWhitelist[4].group | string | `"rbac.authorization.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[4].kind | string | `"ClusterRoleBinding"` |  |
| projects.tenant.clusterResourceWhitelist[5].group | string | `"cert-manager.io"` |  |
| projects.tenant.clusterResourceWhitelist[5].kind | string | `"*"` |  |
| projects.tenant.clusterResourceWhitelist[6].group | string | `"apiextensions.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[6].kind | string | `"CustomResourceDefinition"` |  |
| projects.tenant.clusterResourceWhitelist[7].group | string | `"gateway.networking.k8s.io"` |  |
| projects.tenant.clusterResourceWhitelist[7].kind | string | `"*"` |  |
| projects.tenant.description | string | `"Homelab tenant/apps tier (user-facing services)"` |  |
| projects.tenant.destinations[0].namespace | string | `"*"` |  |
| projects.tenant.destinations[0].server | string | `"https://kubernetes.default.svc"` |  |
| projects.tenant.enabled | bool | `true` | Render the `tenant` AppProject. |
| projects.tenant.extraSourceRepos | list | `[]` | Added to the derived `sourceRepos`. |
| projects.tenant.name | string | `"admin-tenant"` | Project name (referenced by `argo-cd/instances/<inst>/instance.yaml.projects.tenant`). |
| projects.tenant.namespaceResourceBlacklist | list | `[]` |  |
| projects.tenant.namespaceResourceWhitelist[0].group | string | `"*"` |  |
| projects.tenant.namespaceResourceWhitelist[0].kind | string | `"*"` |  |
| projects.tenant.roles[0].description | string | `"Admin keycloak group"` |  |
| projects.tenant.roles[0].groups[0] | string | `"admin"` |  |
| projects.tenant.roles[0].name | string | `"admin"` |  |
| projects.tenant.roles[0].policies[0] | string | `"p, proj:admin-tenant:admin, applications, *, admin-tenant/*, allow"` |  |
| projects.tenant.sourceNamespaces | list | `[]` |  |
| projects.tenant.sourceRepos | list | `[]` | Repositories the project's Applications may take sources from. Empty: derived like the `core` project's. |
| rootManager.enabled | bool | `true` | Enable the root `manager` ApplicationSet that discovers every instance under `argo-cd/instances/*` and renders one per-instance Application pointing at the `instance-manager` chart. |
| rootManager.repoURL.sources | string | `"https://github.com/this-is-tobi/homelab.git"` | Where the `instance-manager` chart comes from: a git repository, or an `oci://` bundle with the layout of this repository (the chart is read at `argo-cd/apps/instance-manager`). |
| rootManager.repoURL.values | string | `""` | Git repository holding `argo-cd/instances/*`: the instances are discovered there and their `instance.yaml` read from it. Defaults to `sources`; required when `sources` is an `oci://` bundle. |
| rootManager.sourcesRevision | string | `""` | Revision of `sources` alone: a git revision, or the exact version of an `oci://` bundle (required then). Defaults to `targetRevision`. |
| rootManager.targetRevision | string | `"main"` | Git revision (branch, tag, commit) of the `values` repository, and of `sources` when `sourcesRevision` is empty. |
| vso.enabled | bool | `false` | Enable VSO integration. Set to true in instance values AFTER vault-operator is deployed so VaultStaticSecret CRDs are available. |
| vso.extraObjects | list | `[]` |  |
| vso.vaultAuth.default.kubernetes.audiences | list | `["vault"]` | The audience of the token VSO logs in with: a token meant for Vault, not a Kubernetes API credential. The Vault role can require it. |
| vso.vaultAuth.default.kubernetes.role | string | `""` |  |
| vso.vaultAuth.default.kubernetes.serviceAccount | string | `"vso"` |  |
| vso.vaultAuth.default.method | string | `"kubernetes"` |  |
| vso.vaultAuth.default.mount | string | `"kubernetes"` |  |
| vso.vaultConnection.default.address | string | `"https://vault.vault-operator-system.svc.cluster.local:8200"` |  |
| vso.vaultConnection.default.caCertSecretRef | string | `"vault-tls"` |  |
| vso.vaultConnection.default.skipTLSVerify | bool | `false` |  |
| vso.vaultStaticSecrets | object | `{}` |  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
