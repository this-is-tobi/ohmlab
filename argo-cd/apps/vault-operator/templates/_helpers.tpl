{{/*
The KV v2 mount of the platform secrets: the instance's name
(global.ohmlab.names.vaultMount), else the product default. The policies, the
secrets engine and the VSO and controller mounts all follow it, unless they
set their own.
*/}}
{{- define "vault-operator.kvMount" -}}
{{- dig "ohmlab" "names" "vaultMount" "homelab" (.Values.global | default dict) -}}
{{- end -}}

{{/*
The Vault policy of the ohmlab controller's Vault target: KV data and metadata
under platforms/+/+/*, nothing else. No identity, no sys, no policies, no
mounts, no auth, no tokens. Delete is on the metadata path alone, because
that is the one call the controller makes to remove a path (it removes every
version and the metadata), and only when the chart allows deletion.
utils/internal/seed/testdata/controller-policy*.hcl hold the same text, and
scripts/ci/check-controller-policy.sh keeps the two equal.
*/}}
{{- define "vault-operator.controllerPolicy" -}}
{{- $mount := .Values.ohmlab.controller.kvMount | default (include "vault-operator.kvMount" .) -}}
path "{{ $mount }}/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "{{ $mount }}/metadata/platforms/+/+/*" {
  capabilities = ["read", "update"{{ if .Values.ohmlab.controller.allowDelete }}, "delete"{{ end }}]
}
{{ end -}}

{{/*
The name of the VaultSeed for a KV path: the path without its mount and without
a leading "platforms/", slashes, dots and underscores turned into dashes
("homelab/platforms/production/core/keycloak" -> "production-core-keycloak").
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
