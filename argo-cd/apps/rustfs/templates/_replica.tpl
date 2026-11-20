{{/*
A store's cloud replica, checked: each template of the replica includes this
first, so a missing value fails the render and names the key.
*/}}
{{- define "rustfs.replica.check" -}}
{{- $r := .Values.replica -}}
{{- if not .Values.store.enabled -}}{{- fail "replica.enabled needs store.enabled: only a store of instance.yaml's storage block has buckets that replicate" -}}{{- end -}}
{{- range $key := list "endpoint" "region" "bucket" "prefix" "store" -}}
{{- if not (get $r $key) -}}{{- fail (printf "replica.%s is required with replica.enabled (the instance-manager sets it from instance.yaml's replica block)" $key) -}}{{- end -}}
{{- end -}}
{{- range $key := list "mount" "record" "target" "crypt" -}}
{{- if not (get $r.vault $key) -}}{{- fail (printf "replica.vault.%s is required with replica.enabled (the instance-manager sets it)" $key) -}}{{- end -}}
{{- end -}}
{{- if not (regexMatch "^https://[^/:@?#]+(:[0-9]{1,5})?$" (toString $r.endpoint)) -}}{{- fail "replica.endpoint must be https://<host>" -}}{{- end -}}
{{- end -}}

{{/* The labels of the replica's objects; the job's pods carry its component. */}}
{{- define "rustfs.replica.labels" -}}
app.kubernetes.io/name: rustfs
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: replica
{{- end -}}
