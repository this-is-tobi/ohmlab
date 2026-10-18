{{/* The enabled targets, sorted, as a JSON array. */}}
{{- define "ohmlab-controller.enabledTargets" -}}
{{- $out := list -}}
{{- range $name, $t := .Values.targets -}}
{{- if $t.enabled -}}{{- $out = append $out $name -}}{{- end -}}
{{- end -}}
{{- $out | sortAlpha | toJson -}}
{{- end -}}

{{/*
Deployment name -> targets, as JSON. Split by default; a custom map must hold
every enabled target exactly once, anything else fails the render.
*/}}
{{- define "ohmlab-controller.deployments" -}}
{{- $enabled := include "ohmlab-controller.enabledTargets" . | fromJsonArray -}}
{{- $result := dict -}}
{{- if .Values.deployments -}}
  {{- $seen := dict -}}
  {{- range $dep, $list := .Values.deployments -}}
    {{- if not $list -}}{{- fail (printf "deployments.%s lists no target" $dep) -}}{{- end -}}
    {{- range $t := $list -}}
      {{- if not (has $t $enabled) -}}{{- fail (printf "deployments.%s lists %q, which is not an enabled target" $dep $t) -}}{{- end -}}
      {{- if hasKey $seen $t -}}{{- fail (printf "target %q is in deployments.%s and in deployments.%s" $t (get $seen $t) $dep) -}}{{- end -}}
      {{- $_ := set $seen $t $dep -}}
    {{- end -}}
    {{- $_ := set $result $dep $list -}}
  {{- end -}}
  {{- range $t := $enabled -}}
    {{- if not (hasKey $seen $t) -}}{{- fail (printf "target %q is enabled and in no deployment" $t) -}}{{- end -}}
  {{- end -}}
{{- else -}}
  {{- range $t := $enabled -}}{{- $_ := set $result $t (list $t) -}}{{- end -}}
{{- end -}}
{{- $result | toJson -}}
{{- end -}}

{{/* The pinned image; required once anything runs, and never a floating tag. */}}
{{- define "ohmlab-controller.image" -}}
{{- $image := required "image is required: pin the controller to an exact release and digest (ghcr.io/this-is-tobi/homelab/ohmlab-controller:0.2.0@sha256:...)" .Values.image -}}
{{- if and (not .Values.unsafeAllowUnpinnedImage) (not (contains "@sha256:" $image)) -}}
{{- fail (printf "image %q is not pinned by digest: add @sha256:..." $image) -}}
{{- end -}}
{{- $image -}}
{{- end -}}

{{/* The namespaces a deployment's targets watch, unique and sorted, as JSON. */}}
{{- define "ohmlab-controller.watchedNamespaces" -}}
{{- $root := .root -}}
{{- $set := dict -}}
{{- range $t := .targets -}}
{{- range $ns := (index $root.Values.targets $t).watchNamespaces -}}{{- $_ := set $set $ns true -}}{{- end -}}
{{- end -}}
{{- keys $set | sortAlpha | toJson -}}
{{- end -}}

{{/* RBAC rules inside a watched namespace, per target. */}}
{{- define "ohmlab-controller.namespaceRules" -}}
{{- if eq . "vault" -}}
- apiGroups: [ohmlab.fr]
  resources: [vaultseeds]
  verbs: [get, list, watch, patch, update]
- apiGroups: [ohmlab.fr]
  resources: [vaultseeds/status]
  verbs: [get, patch, update]
- apiGroups: [events.k8s.io]
  resources: [events]
  verbs: [create, patch]
{{- end -}}
{{- end -}}

{{/* The CRD file and name of a target. */}}
{{- define "ohmlab-controller.crd" -}}
{{- if eq . "vault" -}}ohmlab.fr_vaultseeds.yaml{{- end -}}
{{- end -}}

{{- define "ohmlab-controller.crdName" -}}
{{- if eq . "vault" -}}vaultseeds.ohmlab.fr{{- end -}}
{{- end -}}

{{- define "ohmlab-controller.labels" -}}
app.kubernetes.io/name: ohmlab-controller
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: ohmlab
{{- end -}}

{{/*
The resync period in seconds. The chart takes the simple forms 30s, 10m and 1h,
so alerts can be derived from it.
*/}}
{{- define "ohmlab-controller.resyncSeconds" -}}
{{- if not (regexMatch "^[0-9]+[smh]$" (toString .Values.resync)) -}}
{{- fail (printf "resync %q: write it as 30s, 10m or 1h" (toString .Values.resync)) -}}
{{- end -}}
{{- $n := int (regexReplaceAll "^([0-9]+)[smh]$" .Values.resync "${1}") -}}
{{- $unit := regexReplaceAll "^[0-9]+([smh])$" .Values.resync "${1}" -}}
{{- mul $n (get (dict "s" 1 "m" 60 "h" 3600) $unit) -}}
{{- end -}}
