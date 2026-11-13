{{/*
Per-instance label set propagated to every generated Application.
*/}}
{{- define "instance-manager.labels" -}}
ohmlab.fr/instance-name: {{ .Values.instance.name | quote }}
ohmlab.fr/instance-env: {{ .Values.instance.env | default "" | quote }}
ohmlab.fr/instance-provider: {{ .Values.instance.provider | default "" | quote }}
ohmlab.fr/instance-region: {{ .Values.instance.region | default "" | quote }}
{{- end -}}

{{/*
Resolve the values repo. Defaults to .Values.repoURL when valuesRepoURL is empty.
*/}}
{{- define "instance-manager.valuesRepo" -}}
{{ default .Values.repoURL .Values.valuesRepoURL }}
{{- end -}}

{{/*
RollingSync strategy block (progressive sync). Steps come from
.Values.progressiveSync.steps; a final catch-all step matches every wave not
explicitly listed so no Application can be left unmatched.
*/}}
{{- define "instance-manager.rollingSyncStrategy" -}}
{{- if .Values.progressiveSync.enabled }}
{{- $all := list }}
{{- range $step := .Values.progressiveSync.steps }}
{{- range $w := $step }}
{{- $all = append $all (printf "w%v" $w) }}
{{- end }}
{{- end }}
strategy:
  type: RollingSync
  rollingSync:
    steps:
    {{- range $step := .Values.progressiveSync.steps }}
    - matchExpressions:
      - key: ohmlab.fr/sync-wave
        operator: In
        values:
        {{- range $w := $step }}
        - {{ printf "w%v" $w | quote }}
        {{- end }}
    {{- end }}
    - matchExpressions:
      - key: ohmlab.fr/sync-wave
        operator: NotIn
        values:
        {{- range $w := $all }}
        - {{ $w | quote }}
        {{- end }}
{{- end }}
{{- end -}}

{{/*
Catalogs. A catalogue entry is fetched from a catalog: the one it names with
`catalog:`, else `defaultCatalog`, else `git`, the repository of the instance
itself (`repoURL`, `targetRevision`). A catalog is either
  - a bundle: {repoURL, version}, a repository (git or an `oci://` artifact)
    with the layout of this repository, so an entry only changes where its
    `argo-cd/apps/<chart>` folder comes from, and `version` is the revision
    every entry takes unless it sets `targetRevision`; or
  - a registry path (a string): a Helm chart per app, `chart` at the exact
    version in `targetRevision`.
The helpers below return go-template expressions that Argo CD evaluates per
entry (no braces: the caller wraps them). An unknown catalog name gives an
empty repository, so the Application is invalid instead of silently coming
from git, and a missing version becomes a revision that cannot resolve.
*/}}
{{- define "instance-manager.catalogName" -}}
(default {{ .Values.defaultCatalog | default "git" | quote }} .catalog)
{{- end -}}

{{- define "instance-manager.catalogRepoURL" -}}
{{- $pairs := list (printf "%q %q" "git" (required ".Values.repoURL is required" .Values.repoURL)) -}}
{{- range $name, $c := .Values.catalogs -}}
{{- $url := "" -}}
{{- if kindIs "string" $c }}{{ $url = $c }}{{ else }}{{ $url = $c.repoURL }}{{ end -}}
{{- $pairs = append $pairs (printf "%q %q" $name $url) -}}
{{- end -}}
index (dict {{ join " " $pairs }}) {{ include "instance-manager.catalogName" . }} | default ""
{{- end -}}

{{- define "instance-manager.catalogRevision" -}}
{{- $pairs := list (printf "%q %q" "git" (.Values.targetRevision | default "main")) -}}
{{- range $name, $c := .Values.catalogs -}}
{{- $version := "" -}}
{{- if not (kindIs "string" $c) }}{{ $version = $c.version | default "" }}{{ end -}}
{{- $pairs = append $pairs (printf "%q %q" $name $version) -}}
{{- end -}}
index (dict {{ join " " $pairs }}) {{ include "instance-manager.catalogName" . }} | default "catalog-version-required"
{{- end -}}

{{/*
The names of the registry catalogs (Helm chart per app), as the arguments of a
go-template `list`.
*/}}
{{- define "instance-manager.chartCatalogs" -}}
{{- $names := list -}}
{{- range $name, $c := .Values.catalogs -}}
{{- if kindIs "string" $c }}{{ $names = append $names ($name | quote) }}{{ end -}}
{{- end -}}
{{ join " " $names }}
{{- end -}}

{{/*
The names of the instance's own resources, as a YAML map: each derives from
the instance's name (its Vault KV mount, its Keycloak realm and the name that
realm shows, its Gateway), and `names` overrides any of them. The instance's
name must be a DNS label short enough for "<name>-gateway". An unknown key, an
empty name, or a name the resource it names cannot carry fails the render:
each would leave a broken or derived name in place unnoticed. So do quotes,
backslashes, braces and control characters: Argo CD evaluates the Application
template, and charts insert names into JSON and Vault policies as they are.
Keycloak's own realm and Vault's own mounts are refused, as a realm import or
a secrets engine there would land in the server's administration.
*/}}
{{- define "instance-manager.names" -}}
{{- $instance := .Values.instance.name | toString -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]{0,53}[a-z0-9])?$" $instance) -}}
{{- fail (printf "instance.name %q must be a lowercase DNS label of at most 55 characters: it names the instance's Gateway, Keycloak realm and Vault KV mount" $instance) -}}
{{- end -}}
{{- $names := dict "gateway" (printf "%s-gateway" $instance) "keycloakRealm" $instance "keycloakRealmDisplayName" (title $instance) "vaultMount" $instance -}}
{{- $known := keys $names | sortAlpha -}}
{{- $overrides := .Values.names | default dict -}}
{{- range $key, $name := $overrides -}}
{{- if not (has $key $known) -}}
{{- fail (printf "names.%s is not a name the product reads (%s)" $key (join ", " $known)) -}}
{{- end -}}
{{- if or (not (kindIs "string" $name)) (not $name) -}}
{{- fail (printf "names.%s must be a non-empty string" $key) -}}
{{- end -}}
{{- if regexMatch `["\\{}[:cntrl:]]` $name -}}
{{- fail (printf "names.%s cannot contain quotes, backslashes, braces or control characters" $key) -}}
{{- end -}}
{{- $_ := set $names $key $name -}}
{{- end -}}
{{- $formats := dict "gateway" "^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$" "keycloakRealm" "^[A-Za-z0-9][-A-Za-z0-9_.]*$" "vaultMount" "^[A-Za-z0-9][-A-Za-z0-9_.]*(/[-A-Za-z0-9_.]+)*$" -}}
{{- $reserved := dict "keycloakRealm" (list "Keycloak" (list "master")) "vaultMount" (list "Vault" (list "auth" "cubbyhole" "identity" "sys")) -}}
{{- range $key, $name := $names -}}
{{- $what := printf "names.%s %q" $key $name -}}
{{- if not (hasKey $overrides $key) -}}
{{- $what = printf "instance.name %q gives names.%s %q, which" $instance $key $name -}}
{{- end -}}
{{- with get $formats $key -}}
{{- if not (regexMatch . $name) -}}
{{- fail (printf "%s must match %s" $what .) -}}
{{- end -}}
{{- end -}}
{{- with get $reserved $key -}}
{{- if has (first (splitList "/" $name)) (index . 1) -}}
{{- fail (printf "%s is reserved by %s: set another names.%s in instance.yaml" $what (index . 0) $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml $names -}}
{{- end -}}

{{/*
The names of the instance as Helm parameters, one `global.ohmlab.names.<key>`
each, for every generated Application.
*/}}
{{- define "instance-manager.namesParameters" -}}
{{- range $key, $name := include "instance-manager.names" . | fromYaml }}
- name: {{ printf "global.ohmlab.names.%s" $key | quote }}
  value: {{ $name | quote }}
  forceString: true
{{- end }}
{{- end -}}

{{/*
The KV mount of a scope's secrets: the core tier reads the ohmlab system
mount, the tenant tier the instance's (names.vaultMount), and an instance with
vault.hub reads its own mount in both tiers. The literal ohmlab here and the
default of the vault-operator chart's systemMount are the two places that name
the system mount. Called with (list . <scope>).
*/}}
{{- define "instance-manager.vaultMount" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
{{- if or (dig "hub" "" ($root.Values.vault | default dict) | toString) (eq $scope "tenant") -}}
{{- (include "instance-manager.names" $root | fromYaml).vaultMount -}}
{{- else -}}
ohmlab
{{- end -}}
{{- end -}}

{{/*
The KV mount and path prefix of a scope's secrets as Helm parameters
(global.ohmlab.vault), from which every chart builds its secrets' mount and
path: the scope's mount (instance-manager.vaultMount), under
platforms/<instance.env>/<scope>, given only with instance.env. An instance
with vault.hub reads the Vault of that instance, whose spokeClusters declare
it, logging in with the jwt method at the mount jwt-<instance>, and every app
gets the hub's name (a remote Keycloak leaves out the realm the hub serves).
Called with (list . <scope>).
*/}}
{{- define "instance-manager.vaultParameters" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
{{- $env := $root.Values.instance.env | default "" | toString -}}
{{- if and $env (not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $env)) -}}
{{- fail (printf "instance.env %q must match ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$: it is a segment of every secret's path" $env) -}}
{{- end -}}
{{- $instance := $root.Values.instance.name | toString -}}
{{- $hub := dig "hub" "" ($root.Values.vault | default dict) | toString -}}
{{- if $hub -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$" $hub) -}}
{{- fail (printf "vault.hub %q must be a lowercase DNS label: it names the instance whose Vault this one reads" $hub) -}}
{{- end -}}
{{- if eq $hub $instance -}}
{{- fail (printf "vault.hub %q is this instance: an instance that runs its own Vault leaves vault.hub empty" $hub) -}}
{{- end -}}
{{- end -}}
{{- $mount := include "instance-manager.vaultMount" (list $root $scope) -}}
- name: "global.ohmlab.vault.mount"
  value: {{ $mount | quote }}
  forceString: true
{{- with $env }}
- name: "global.ohmlab.vault.pathPrefix"
  value: {{ printf "platforms/%s/%s" . $scope | quote }}
  forceString: true
{{- end }}
{{- if $hub }}
- name: "global.ohmlab.vault.auth.method"
  value: "jwt"
  forceString: true
- name: "global.ohmlab.vault.auth.mount"
  value: {{ printf "jwt-%s" $instance | quote }}
  forceString: true
- name: "global.ohmlab.vault.hub"
  value: {{ $hub | quote }}
  forceString: true
{{- end }}
{{- end -}}

{{/*
Refuses a catalog section that cannot work, so that the render of the manager
fails instead of Applications quietly coming from somewhere else.
*/}}
{{- define "instance-manager.validateCatalogs" -}}
{{- $catalogs := .Values.catalogs | default dict -}}
{{- if hasKey $catalogs "git" -}}
{{- fail "catalogs.git is reserved: it is the repository of the instance itself (repoURL, targetRevision)" -}}
{{- end -}}
{{- range $name, $c := $catalogs -}}
{{- if and (not (kindIs "string" $c)) (not $c.repoURL) -}}
{{- fail (printf "catalogs.%s.repoURL is required" $name) -}}
{{- end -}}
{{- end -}}
{{- $default := .Values.defaultCatalog | default "git" -}}
{{- if and (ne $default "git") (not (hasKey $catalogs $default)) -}}
{{- fail (printf "defaultCatalog %q is not declared under catalogs" $default) -}}
{{- end -}}
{{- end -}}

{{/*
The generator of a tier's ApplicationSet: the enabled apps of the tier file.
Called with (list . <scope>).
*/}}
{{- define "instance-manager.tierMatrix" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
- matrix:
    generators:
    - git:
        repoURL: {{ $root.Values.repoURL | quote }}
        revision: {{ $root.Values.targetRevision | default "main" | quote }}
        files:
        - path: 'argo-cd/instances/{{ $root.Values.instance.name }}/{{ $scope }}.yaml'
    - list:
        elementsYaml: "{{ `{{ .apps | toJson }}` }}"
      selector:
        matchExpressions:
        - key: enabled
          operator: In
          values:
          - "true"
{{- end -}}

{{/*
The instance's object storage, validated, as YAML: the stores a role names,
each with its endpoint's port, its region and the path of its root keys (the
record of the store's app), and the store of each role. Empty while no role
names a store, so that nothing of storage renders.
*/}}
{{- define "instance-manager.storage" -}}
{{- $s := .Values.storage | default dict -}}
{{- $env := .Values.instance.env | default "" | toString -}}
{{- $coreMount := include "instance-manager.vaultMount" (list . "core") -}}
{{- $stores := dict -}}
{{- range $name, $st := $s.stores | default dict -}}
{{- $st = $st | default dict -}}
{{- $type := $st.type | default "rustfs" | toString -}}
{{- if ne $type "rustfs" -}}
{{- fail (printf "storage.stores.%s.type %q: only rustfs stores exist so far" $name $type) -}}
{{- end -}}
{{- if not (and $st.app $st.namespace $st.endpoint $st.size) -}}
{{- fail (printf "storage.stores.%s needs app, namespace, endpoint and size" $name) -}}
{{- end -}}
{{- $endpoint := $st.endpoint | toString -}}
{{- $port := 0 -}}
{{- if regexMatch `^https://[a-z0-9]([-a-z0-9.]*[a-z0-9])?:[0-9]{1,5}$` $endpoint -}}
{{- $port = regexReplaceAll `^.*:` $endpoint "" | atoi -}}
{{- end -}}
{{- if or (lt $port 1) (gt $port 65535) -}}
{{- fail (printf "storage.stores.%s.endpoint %q must be https://<host>:<port>" $name $endpoint) -}}
{{- end -}}
{{- $size := $st.size | toString -}}
{{- if not (regexMatch `^[0-9]+(\.[0-9]+)?([KMGTPE]i|[kMGTPE])?$` $size) -}}
{{- fail (printf "storage.stores.%s.size %q must be a quantity like 50Gi" $name $size) -}}
{{- end -}}
{{- $_ := set $stores $name (dict "type" $type "app" ($st.app | toString) "namespace" ($st.namespace | toString) "endpoint" $endpoint "port" $port "region" ($st.region | default "us-east-1" | toString) "size" $size "storageClass" ($st.storageClass | default "" | toString) "rootPath" (printf "%s/platforms/%s/core/%s" $coreMount $env $st.app)) -}}
{{- end -}}
{{- $roles := dict -}}
{{- range $role := list "backups" "data" -}}
{{- $store := get $s $role | default "" | toString -}}
{{- if and $store (not (hasKey $stores $store)) -}}
{{- fail (printf "storage.%s %q is not a store of storage.stores" $role $store) -}}
{{- end -}}
{{- $_ := set $roles $role $store -}}
{{- end -}}
{{- range $app, $claims := $s.claims | default dict -}}
{{- range $claim, $c := $claims | default dict -}}
{{- $role := dig "role" "" ($c | default dict) | toString -}}
{{- if not (has $role (list "backups" "data")) -}}
{{- fail (printf "storage.claims.%s.%s.role %q is not backups or data" $app $claim $role) -}}
{{- end -}}
{{- $wiring := dig "wiring" "" ($c | default dict) | toString -}}
{{- if not (has $wiring (list "" "none" "cnpg" "backup-utils")) -}}
{{- fail (printf "storage.claims.%s.%s.wiring %q is not cnpg, backup-utils or none" $app $claim $wiring) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if or $roles.backups $roles.data -}}
{{- if not $env -}}
{{- fail "storage needs instance.env: it is a segment of every record's path" -}}
{{- end -}}
{{- if dig "hub" "" (.Values.vault | default dict) -}}
{{- fail "storage on an instance that reads another instance's Vault (vault.hub) is not supported yet" -}}
{{- end -}}
{{- $used := dict -}}
{{- range $role, $store := $roles }}{{ if $store }}{{ $_ := set $used $store (get $stores $store) }}{{ end }}{{ end -}}
{{- toYaml (dict "stores" $used "roles" $roles) -}}
{{- end -}}
{{- end -}}

{{/*
The claims of a scope, as YAML <app>: {<claim>: {...}}: each enabled claim
whose role has a store, over claimDefaults (a claim's false and 0 win too),
with its store, its bucket <mount>-<app>-<claim> and its record
<mount>/platforms/<env>/<scope>/<app>/s3, key <claim>. Every app of the
registry is in both scopes: the merge keeps those of the tier's apps. Called
with (list . <scope>).
*/}}
{{- define "instance-manager.tierClaims" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
{{- $model := include "instance-manager.storage" $root | fromYaml -}}
{{- if $model -}}
{{- $s := $root.Values.storage -}}
{{- $mount := include "instance-manager.vaultMount" (list $root $scope) -}}
{{- $out := dict -}}
{{- range $app, $claims := $s.claims | default dict -}}
{{- range $claim, $c := $claims | default dict -}}
{{- $r := mergeOverwrite (deepCopy ($s.claimDefaults | default dict)) (deepCopy ($c | default dict)) -}}
{{- $store := get $model.roles ($r.role | toString) -}}
{{- if and $store (ne (toString $r.enabled) "false") -}}
{{- $_ := set $r "store" $store -}}
{{- $_ := set $r "bucket" (printf "%s-%s-%s" $mount $app $claim) -}}
{{- $_ := set $r "record" (printf "%s/platforms/%s/%s/%s/s3" $mount $root.Values.instance.env $scope $app) -}}
{{- $_ := set $r "key" $claim -}}
{{- $appClaims := get $out $app | default dict -}}
{{- $_ := set $appClaims $claim $r -}}
{{- $_ := set $out $app $appClaims -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}
{{- end -}}

{{/*
The merge list of a scope's ApplicationSet, as YAML: {app, storageValues} for
each app with values to receive, sorted by app; empty when none. The core
scope gives the controller its stores, vault-operator the path of each
store's root keys, and each store's app its volume and region. With
wireConsumers, each claim gives its app global.ohmlab.storage.<claim> and its
wiring. Called with (list . <scope>).
*/}}
{{- define "instance-manager.storageElements" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
{{- $model := include "instance-manager.storage" $root | fromYaml -}}
{{- if $model -}}
{{- $els := dict -}}
{{- if eq $scope "core" -}}
{{- $targetStores := list -}}
{{- $paths := list -}}
{{- range $name, $st := $model.stores -}}
{{- $targetStores = append $targetStores (dict "name" $name "type" $st.type "endpoint" $st.endpoint "region" $st.region "pathStyle" true "bucketPrefix" "" "capacity" $st.size "namespace" $st.namespace "rootCredentials" (dict "path" $st.rootPath "key" "admin")) -}}
{{- $paths = append $paths (dict "name" $name "path" $st.rootPath) -}}
{{- $class := dict "dataStorageSize" $st.size -}}
{{- with $st.storageClass }}{{ $_ := set $class "name" . }}{{ end -}}
{{- $_ := set $els $st.app (dict "rustfs" (dict "storageclass" $class "config" (dict "rustfs" (dict "region" $st.region)))) -}}
{{- end -}}
{{- $_ := set $els "ohmlab-controller" (dict "targets" (dict "storage" (dict "enabled" true "stores" $targetStores))) -}}
{{- $_ := set $els "vault-operator" (dict "ohmlab" (dict "controller" (dict "storage" (dict "stores" $paths)))) -}}
{{- end -}}
{{- if ne (toString $root.Values.storage.wireConsumers) "false" -}}
{{- range $app, $claims := include "instance-manager.tierClaims" (list $root $scope) | fromYaml -}}
{{- $v := get $els $app | default dict -}}
{{- range $claim, $c := $claims -}}
{{- $st := get $model.stores $c.store -}}
{{- $global := get $v "global" | default dict -}}
{{- $ohmlab := get $global "ohmlab" | default dict -}}
{{- $storage := get $ohmlab "storage" | default dict -}}
{{- $_ := set $storage $claim (dict "enabled" true "bucket" $c.bucket "endpoint" $st.endpoint "region" $st.region "pathStyle" true) -}}
{{- $_ := set $ohmlab "storage" $storage -}}
{{- $_ := set $global "ohmlab" $ohmlab -}}
{{- $_ := set $v "global" $global -}}
{{- if eq (toString $c.wiring) "cnpg" -}}
{{- $cnpg := get $v "cnpg" | default dict -}}
{{- $_ := set $cnpg "backups" (dict "enabled" true "endpointURL" $st.endpoint "destinationPath" (printf "s3://%s/" $c.bucket) "endpointCA" (dict "name" (printf "%s-pg-cluster-backup" $app) "key" "ca.crt")) -}}
{{- /* The backup alerts read the series of the cluster's PodMonitor. */ -}}
{{- $_ := set $cnpg "cluster" (dict "monitoring" (dict "enabled" true)) -}}
{{- $_ := set $v "cnpg" $cnpg -}}
{{- else if eq (toString $c.wiring) "backup-utils" -}}
{{- $_ := set $v "backup-utils" (dict "enabled" true) -}}
{{- end -}}
{{- end -}}
{{- $_ := set $els $app $v -}}
{{- end -}}
{{- end -}}
{{- $list := list -}}
{{- range $app := keys $els | sortAlpha -}}
{{- $list = append $list (dict "app" $app "storageValues" (get $els $app)) -}}
{{- end -}}
{{- if $list }}{{ toYaml $list }}{{ end -}}
{{- end -}}
{{- end -}}

{{/*
The values of a scope's storage-claims Application, as YAML: the scope, the
namespace pattern of its apps, the controller's namespace, each store's
namespace, port and size, and the scope's claims. Called with (list . <scope>).
*/}}
{{- define "instance-manager.claimsValues" -}}
{{- $root := index . 0 -}}
{{- $scope := index . 1 -}}
{{- $model := include "instance-manager.storage" $root | fromYaml -}}
{{- $stores := dict -}}
{{- range $name, $st := $model.stores -}}
{{- $_ := set $stores $name (dict "namespace" $st.namespace "port" $st.port "size" $st.size) -}}
{{- end -}}
{{- $claims := dict -}}
{{- range $app, $cs := include "instance-manager.tierClaims" (list $root $scope) | fromYaml -}}
{{- $appClaims := dict -}}
{{- range $claim, $c := $cs -}}
{{- $_ := set $appClaims $claim (pick $c "store" "bucket" "record" "key" "quota" "versioning" "lifecycle" "replicate" "access" "clients") -}}
{{- end -}}
{{- $_ := set $claims $app $appClaims -}}
{{- end -}}
{{- toYaml (dict "scope" $scope "namespacePrefix" ($root.Values.namespacePrefix | default "") "namespaceSuffix" ($root.Values.namespaceSuffix | default "") "controller" (dict "namespace" "ohmlab-system") "networkPolicies" (eq (toString $root.Values.storage.networkPolicies) "true") "stores" $stores "claims" $claims) -}}
{{- end -}}

{{/*
Where the storage-claims chart comes from: the default catalog, a git
repository or a bundle with the layout of this one. A chart registry holds no
catalog app, so storage refuses it as the default catalog.
*/}}
{{- define "instance-manager.claimsSource" -}}
{{- $name := .Values.defaultCatalog | default "git" -}}
{{- if eq $name "git" -}}
repoURL: {{ .Values.repoURL | quote }}
targetRevision: {{ .Values.targetRevision | default "main" | quote }}
{{- else -}}
{{- $c := get (.Values.catalogs | default dict) $name -}}
{{- if kindIs "string" $c -}}
{{- fail (printf "storage needs a git or bundle default catalog: defaultCatalog %q is a chart registry, which holds no storage-claims chart" $name) -}}
{{- end -}}
repoURL: {{ $c.repoURL | quote }}
targetRevision: {{ $c.version | default "catalog-version-required" | quote }}
{{- end }}
path: argo-cd/apps/storage-claims
{{- end -}}
