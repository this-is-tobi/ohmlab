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
one, or only ohmlab.controller.kvMount when set.
cli/internal/seed/testdata/controller-policy*.hcl hold the text for one mount,
and scripts/ci/check-controller-policy.sh keeps the two equal.
*/}}
{{- define "vault-operator.controllerPolicy" -}}
{{- $mounts := list (include "vault-operator.systemMount" .) (include "vault-operator.kvMount" .) | uniq -}}
{{- with .Values.ohmlab.controller.kvMount }}{{ $mounts = list . }}{{ end -}}
{{- range $mounts -}}
path "{{ . }}/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "{{ . }}/metadata/platforms/+/+/*" {
  capabilities = ["read", "update"{{ if $.Values.ohmlab.controller.allowDelete }}, "delete"{{ end }}]
}
{{ end -}}
{{- end -}}

{{/*
The name of the VaultSeed for a KV path: the path without its mount and without
a leading "platforms/", slashes, dots and underscores turned into dashes
("ohmlab/platforms/production/core/keycloak" -> "production-core-keycloak").
*/}}
{{- define "vault-operator.seedName" -}}
{{- $n := regexReplaceAll "^[^/]+/" . "" -}}
{{- $n = regexReplaceAll "^platforms/" $n "" -}}
{{- $n = lower (regexReplaceAll "[/_.]" $n "-") -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $n) -}}
{{- fail (printf "ohmlab.vault.secrets: path %q cannot be turned into a Kubernetes name (got %q)" . $n) -}}
{{- end -}}
{{- if gt (len $n) 253 -}}{{- fail (printf "ohmlab.vault.secrets: the name for %q is longer than 253 characters" .) -}}{{- end -}}
{{- $n -}}
{{- end -}}
