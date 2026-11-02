{{/*
Chart full name (used for labelling).
*/}}
{{- define "ohmlab.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
