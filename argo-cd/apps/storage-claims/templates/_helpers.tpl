{{/*
The tier file's entries that are enabled and have claims, as a YAML list of
{app, namespace, claims}, the namespace resolved; a bucket name that cannot
be one fails the render.
*/}}
{{- define "storage-claims.apps" -}}
{{- $out := list -}}
{{- range $entry := .Values.apps -}}
{{- $claims := get ($.Values.claims | default dict) ($entry.app | toString) -}}
{{- if and (eq (toString $entry.enabled) "true") $claims -}}
{{- range $claim, $c := $claims -}}
{{- if not (regexMatch `^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$` ($c.bucket | toString)) -}}
{{- fail (printf "storage claim %s.%s: bucket %q is not a valid bucket name (3 to 63 lowercase letters, digits or hyphens): shorten the instance name or set names.vaultMount" $entry.app $claim ($c.bucket | toString)) -}}
{{- end -}}
{{- end -}}
{{- $ns := $entry.namespace | default (printf "%s%s%s" ($.Values.namespacePrefix | default "") $entry.app ($.Values.namespaceSuffix | default "")) -}}
{{- $out = append $out (dict "app" $entry.app "namespace" $ns "claims" $claims) -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}
