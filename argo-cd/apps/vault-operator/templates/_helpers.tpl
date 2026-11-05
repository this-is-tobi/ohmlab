{{/*
The KV v2 mount of the instance's secrets, the tenant tier's: its name
(global.ohmlab.names.vaultMount, which the instance's manager gives), else
ohmlab.
*/}}
{{- define "vault-operator.kvMount" -}}
{{- dig "ohmlab" "names" "vaultMount" "ohmlab" (.Values.global | default dict) -}}
{{- end -}}

{{/*
The KV v2 mount of the core tier's secrets: the ohmlab system's (systemMount),
else ohmlab. The policies, the secrets engines and the controller's policy
cover it and the instance's mount; Vault's OIDC credentials are read from it.
*/}}
{{- define "vault-operator.systemMount" -}}
{{- .Values.systemMount | default "ohmlab" -}}
{{- end -}}

{{/*
The Vault policy of the ohmlab controller's Vault target: KV data and metadata
under platforms/+/+/*, nothing else. No identity, no sys, no policies, no
mounts, no auth, no tokens. Delete is on the metadata path alone, because
that is the one call the controller makes to remove a path (it removes every
version and the metadata), and only when the chart allows deletion. The same
two stanzas cover the system mount, then the instance's when it is another
one, or only ohmlab.controller.kvMount when set, then the mount of each spoke
instance, whose secrets the controller seeds too.
cli/internal/seed/testdata/controller-policy*.hcl hold the text for one mount,
and scripts/ci/check-controller-policy.sh keeps the two equal.
*/}}
{{- define "vault-operator.controllerPolicy" -}}
{{- $mounts := list (include "vault-operator.systemMount" .) (include "vault-operator.kvMount" .) | uniq -}}
{{- with .Values.ohmlab.controller.kvMount }}{{ $mounts = list . }}{{ end -}}
{{- range $c := .Values.vault.externalConfig.spokeClusters | default list }}{{ range $i := $c.instances | default list }}{{ $mounts = append $mounts ($i.mount | default $i.name | toString) }}{{ end }}{{ end -}}
{{- range $mounts | uniq -}}
path "{{ . }}/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "{{ . }}/metadata/platforms/+/+/*" {
  capabilities = ["read", "update"{{ if $.Values.ohmlab.controller.allowDelete }}, "delete"{{ end }}]
}
{{ end -}}
{{- end -}}

{{/*
The instances of the spoke clusters (vault.externalConfig.spokeClusters) as a
YAML list, each with its cluster's keys and, per app, the namespace, service
account, policy name and rules of its role. These derive from this Vault's
kubernetes role of the same name: its one namespace with the instance's prefix
and suffix, its one service account, and the rules of the policies it lists,
rendered with both KV mounts set to the instance's. The render fails on
whatever would let a remote cluster do more than read its own mount, or reuse
a name of this Vault. Called with (dict "Values" .Values "kv" <the policies'
tpl context> "auth" <the auth list>).
*/}}
{{- define "vault-operator.spokes" -}}
{{- $kv := .kv -}}
{{- $own := list $kv.systemMount $kv.kvMount -}}
{{- $reserved := list "audit" "auth" "cubbyhole" "identity" "sys" -}}
{{- $roles := dict -}}
{{- $authPaths := dict -}}
{{- range $a := .auth -}}
{{- $_ := set $authPaths (toString ($a.path | default $a.type) | trimSuffix "/") true -}}
{{- if eq $a.type "kubernetes" -}}
{{- range $r := $a.roles | default list -}}
{{- if not (hasKey $roles $r.name) }}{{ $_ := set $roles $r.name $r }}{{ end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $rulesOf := dict -}}
{{- $policyNames := dict "ohmlab-controller-vault" true -}}
{{- range $p := .Values.vault.externalConfig.policies | default list -}}
{{- $_ := set $rulesOf $p.name ($p.rules | default "") -}}
{{- $_ := set $policyNames $p.name true -}}
{{- end -}}
{{- /* Every mount a spoke instance's must stay clear of, neither equal nor nested, and what it is. */ -}}
{{- $taken := dict -}}
{{- range $m := $own }}{{ $_ := set $taken (trimSuffix "/" $m) "a KV mount of this Vault's own instance, which no remote cluster reads" }}{{ end -}}
{{- range $s := tpl (toYaml (.Values.vault.externalConfig.secrets | default list)) $kv | fromYamlArray -}}
{{- $path := toString ($s.path | default $s.type) | trimSuffix "/" -}}
{{- if not (hasKey $taken $path) }}{{ $_ := set $taken $path "a secrets engine of vault.externalConfig.secrets" }}{{ end -}}
{{- end -}}
{{- $names := dict -}}
{{- $out := list -}}
{{- range $cluster := .Values.vault.externalConfig.spokeClusters | default list -}}
{{- $cname := $cluster.name | required "name is required for every entry in vault.externalConfig.spokeClusters" -}}
{{- $discovery := $cluster.oidcDiscoveryUrl | default "" -}}
{{- $keys := $cluster.publicKeys | default list -}}
{{- if eq (empty $discovery) (empty $keys) -}}
{{- fail (printf "spoke cluster %s needs exactly one of oidcDiscoveryUrl and publicKeys" $cname) -}}
{{- end -}}
{{- if and $discovery (not (hasPrefix "https://" $discovery)) -}}
{{- fail (printf "spoke cluster %s: oidcDiscoveryUrl must be https: Vault fetches the cluster's keys from it" $cname) -}}
{{- end -}}
{{- /* The namespaces each instance of this cluster binds: two instances never share one. */ -}}
{{- $boundBy := dict -}}
{{- if empty $cluster.instances -}}
{{- fail (printf "instances is required for spoke cluster %s" $cname) -}}
{{- end -}}
{{- range $inst := $cluster.instances -}}
{{- $name := $inst.name | default "" | toString -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]{0,53}[a-z0-9])?$" $name) -}}
{{- fail (printf "spoke instance %q of cluster %s must be a lowercase DNS label of at most 55 characters, the instance's name" $name $cname) -}}
{{- end -}}
{{- if hasKey $names $name -}}
{{- fail (printf "spoke instance %s is declared twice, in clusters %s and %s" $name (get $names $name) $cname) -}}
{{- end -}}
{{- $_ := set $names $name $cname -}}
{{- $mount := $inst.mount | default $name | toString -}}
{{- if not (regexMatch "^[A-Za-z0-9][-A-Za-z0-9_.]*(/[-A-Za-z0-9_.]+)*$" $mount) -}}
{{- fail (printf "spoke instance %s: mount %q must match ^[A-Za-z0-9][-A-Za-z0-9_.]*(/[-A-Za-z0-9_.]+)*$" $name $mount) -}}
{{- end -}}
{{- range $n := list $name $mount -}}
{{- if has (first (splitList "/" $n)) $reserved -}}
{{- fail (printf "spoke instance %s: %s is reserved by Vault" $name $n) -}}
{{- end -}}
{{- end -}}
{{- if has $name $own -}}
{{- fail (printf "spoke instance %s: %s is a KV mount of this Vault's own instance, which no remote cluster reads" $name $name) -}}
{{- end -}}
{{- range $other, $what := $taken -}}
{{- if or (eq $mount $other) (hasPrefix (printf "%s/" $other) $mount) (hasPrefix (printf "%s/" $mount) $other) -}}
{{- fail (printf "spoke instance %s: mount %s overlaps %s, %s" $name $mount $other $what) -}}
{{- end -}}
{{- end -}}
{{- $_ := set $taken $mount (printf "the mount of spoke instance %s" $name) -}}
{{- if hasKey $authPaths (printf "jwt-%s" $name) -}}
{{- fail (printf "spoke instance %s: jwt-%s is already an auth mount of vault.externalConfig.auth, whose roles would accept the cluster's tokens" $name $name) -}}
{{- end -}}
{{- if empty $inst.apps -}}
{{- fail (printf "apps is required for spoke instance %s: a jwt mount with no roles breaks the configurer" $name) -}}
{{- end -}}
{{- $prefix := $inst.namespacePrefix | default "" | toString -}}
{{- $suffix := $inst.namespaceSuffix | default "" | toString -}}
{{- $skv := dict "kvMount" $mount "systemMount" $mount "Template" $kv.Template -}}
{{- $apps := list -}}
{{- range $entry := $inst.apps -}}
{{- $app := ternary (dict "name" $entry) $entry (kindIs "string" $entry) -}}
{{- $aname := $app.name | required (printf "every app of spoke instance %s needs a name" $name) | toString -}}
{{- $role := get $roles $aname -}}
{{- if not $role -}}
{{- fail (printf "spoke instance %s: app %s has no kubernetes role in vault.externalConfig.auth, from which its role and policy derive" $name $aname) -}}
{{- end -}}
{{- $ns := $app.namespace | default "" | toString -}}
{{- if not $ns -}}
{{- $bound := $role.bound_service_account_namespaces | default list -}}
{{- if or (ne (len $bound) 1) (contains "*" (toString (first $bound))) -}}
{{- fail (printf "spoke instance %s: app %s: its kubernetes role binds %s, not one namespace: set namespace on the app's entry" $name $aname (toJson $bound)) -}}
{{- end -}}
{{- $ns = printf "%s%s%s" $prefix (first $bound) $suffix -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$" $ns) -}}
{{- fail (printf "spoke instance %s: app %s: %q is not a namespace name" $name $aname $ns) -}}
{{- end -}}
{{- with get $boundBy $ns -}}
{{- if ne . $name -}}
{{- fail (printf "spoke instance %s: app %s: namespace %s is already bound by spoke instance %s of cluster %s, and its tokens would read both mounts" $name $aname $ns . $cname) -}}
{{- end -}}
{{- end -}}
{{- $_ := set $boundBy $ns $name -}}
{{- $sa := $app.serviceAccount | default "" | toString -}}
{{- if not $sa -}}
{{- $sas := $role.bound_service_account_names | default list -}}
{{- if or (ne (len $sas) 1) (contains "*" (toString (first $sas))) -}}
{{- fail (printf "spoke instance %s: app %s: its kubernetes role binds the service accounts %s, not one: set serviceAccount on the app's entry" $name $aname (toJson $sas)) -}}
{{- end -}}
{{- $sa = toString (first $sas) -}}
{{- end -}}
{{- $policy := printf "%s-%s" $name $aname -}}
{{- if hasKey $policyNames $policy -}}
{{- fail (printf "spoke instance %s: the policy %s of app %s is already a policy of this Vault" $name $policy $aname) -}}
{{- end -}}
{{- $_ := set $policyNames $policy true -}}
{{- $listed := $role.policies | default $role.token_policies | default list -}}
{{- if empty $listed -}}
{{- fail (printf "spoke instance %s: app %s: its kubernetes role lists no policy (policies or token_policies) to derive one from" $name $aname) -}}
{{- end -}}
{{- $rules := list -}}
{{- range $p := $listed -}}
{{- if not (hasKey $rulesOf $p) -}}
{{- fail (printf "spoke instance %s: app %s: policy %s of its kubernetes role is not in vault.externalConfig.policies" $name $aname $p) -}}
{{- end -}}
{{- $rules = append $rules (trim (tpl (get $rulesOf $p) $skv)) -}}
{{- end -}}
{{- $text := join "\n" $rules -}}
{{- /*
  The whole text, full-line comments aside, must be HCL stanzas that read
  or list paths under the instance's mount (deny takes access away): any
  other text, JSON, a comment inside a line or bank-vaults templating could
  hold more than these checks see. The finer messages only name the cause.
*/ -}}
{{- $stanza := printf `path\s+"%s/[^"\\$#\n]*"\s*\{\s*capabilities\s*=\s*\[\s*"(read|list|deny)"(\s*,\s*"(read|list|deny)")*\s*,?\s*\]\s*\}` (regexQuoteMeta $mount) -}}
{{- $bare := regexReplaceAll `(?m)^[ \t]*#.*$` $text "" -}}
{{- if not (regexMatch (printf `^\s*(%s\s*)+$` $stanza) $bare) -}}
{{- range $caps := regexFindAll `capabilities\s*=\s*\[[^\]]*\]` $bare -1 -}}
{{- range $c := regexFindAll `"[^"]*"` $caps -1 -}}
{{- if not (has (trimAll "\"" $c) (list "read" "list" "deny")) -}}
{{- fail (printf "spoke instance %s: app %s: policy %s would grant %s, and a remote cluster only reads (read, list)" $name $aname $policy $c) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- range $m := regexFindAll `path\s+"[^"]*"` $bare -1 -}}
{{- $path := regexReplaceAll `^path\s+"([^"]*)"$` $m "${1}" -}}
{{- if not (hasPrefix (printf "%s/" $mount) $path) -}}
{{- fail (printf "spoke instance %s: app %s: policy %s would open %s, outside the mount %s" $name $aname $policy $path $mount) -}}
{{- end -}}
{{- end -}}
{{- fail (printf "spoke instance %s: app %s: policy %s must be only HCL stanzas path \"%s/...\" { capabilities = [...] } with read, list or deny: no other text can be checked for a remote cluster" $name $aname $policy $mount) -}}
{{- end -}}
{{- $apps = append $apps (dict "name" $aname "namespace" $ns "serviceAccount" $sa "policy" $policy "rules" $text) -}}
{{- end -}}
{{- $i := dict "name" $name "cluster" $cname "mount" $mount "audience" ($cluster.audience | default "vault") "tokenTtl" ($cluster.tokenTtl | default "1h") "tokenMaxTtl" ($cluster.tokenMaxTtl | default "4h") "apps" $apps -}}
{{- $_ := set $i "issuer" ($cluster.issuer | default $discovery | default "https://kubernetes.default.svc.cluster.local") -}}
{{- if $discovery }}{{ $_ := set $i "oidcDiscoveryUrl" $discovery }}{{ else }}{{ $_ := set $i "publicKeys" $keys }}{{ end -}}
{{- $out = append $out $i -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
The name of the VaultSeed for a KV path, called with (dict "path" <path> "hub"
<this Vault's own mounts>): the path without a leading "platforms/" after its
mount, slashes, dots and underscores turned into dashes. The mount is left out
when it is one of the hub's ("ohmlab/platforms/production/core/keycloak" ->
"production-core-keycloak") and kept otherwise, so that two spoke instances
with one env do not collide ("dev/platforms/staging/tenant/gitea" ->
"dev-staging-tenant-gitea").
*/}}
{{- define "vault-operator.seedName" -}}
{{- $mount := regexFind "^[^/]+" .path -}}
{{- $n := regexReplaceAll "^[^/]+/" .path "" -}}
{{- $n = regexReplaceAll "^platforms/" $n "" -}}
{{- if and (contains "/" .path) (not (has $mount .hub)) }}{{ $n = printf "%s/%s" $mount $n }}{{ end -}}
{{- $n = lower (regexReplaceAll "[/_.]" $n "-") -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $n) -}}
{{- fail (printf "ohmlab.vault.secrets: path %q cannot be turned into a Kubernetes name (got %q)" .path $n) -}}
{{- end -}}
{{- if gt (len $n) 253 -}}{{- fail (printf "ohmlab.vault.secrets: the name for %q is longer than 253 characters" .path) -}}{{- end -}}
{{- $n -}}
{{- end -}}
