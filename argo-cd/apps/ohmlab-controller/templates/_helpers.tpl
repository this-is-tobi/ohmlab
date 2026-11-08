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
{{- $image := required "image is required: pin the controller to an exact release and digest (ghcr.io/this-is-tobi/ohmlab/controller:0.2.0@sha256:...)" .Values.image -}}
{{- if and (not .Values.unsafeAllowUnpinnedImage) (not (contains "@sha256:" $image)) -}}
{{- fail (printf "image %q is not pinned by digest: add @sha256:..." $image) -}}
{{- end -}}
{{- $image -}}
{{- end -}}

{{/*
The restarts of the certificates target, checked, as JSON. The checks are the
controller's own, so a mistake fails the render instead of the process.
*/}}
{{- define "ohmlab-controller.restarts" -}}
{{- $list := .Values.targets.certificates.restarts | default list -}}
{{- if not $list -}}{{- fail "targets.certificates.restarts lists nothing: list the Certificates whose workloads to restart" -}}{{- end -}}
{{- $certs := dict -}}
{{- $owners := dict -}}
{{- range $i, $r := $list -}}
{{- with omit $r "namespace" "certificate" "workloads" -}}{{- fail (printf "targets.certificates.restarts[%d] has unknown keys: %v" $i (keys . | sortAlpha)) -}}{{- end -}}
{{- if not (and $r.namespace $r.certificate) -}}{{- fail (printf "targets.certificates.restarts[%d] needs namespace and certificate" $i) -}}{{- end -}}
{{- if not $r.workloads -}}{{- fail (printf "targets.certificates.restarts[%d] (%s/%s) lists no workload" $i $r.namespace $r.certificate) -}}{{- end -}}
{{- $id := printf "%s/%s" $r.namespace $r.certificate -}}
{{- if hasKey $certs $id -}}{{- fail (printf "targets.certificates: %s is listed twice" $id) -}}{{- end -}}
{{- $_ := set $certs $id true -}}
{{- range $j, $w := $r.workloads -}}
{{- with omit $w "kind" "name" -}}{{- fail (printf "targets.certificates.restarts[%d] (%s): workloads[%d] has unknown keys: %v" $i $id $j (keys . | sortAlpha)) -}}{{- end -}}
{{- if not (and $w.kind $w.name) -}}{{- fail (printf "targets.certificates.restarts[%d] (%s/%s): workloads[%d] needs kind and name" $i $r.namespace $r.certificate $j) -}}{{- end -}}
{{- if not (has $w.kind (list "DaemonSet" "Deployment" "StatefulSet")) -}}{{- fail (printf "targets.certificates.restarts[%d] (%s/%s): kind %q is not one of DaemonSet, Deployment, StatefulSet" $i $r.namespace $r.certificate $w.kind) -}}{{- end -}}
{{- $key := printf "%s %s/%s" $w.kind $r.namespace $w.name -}}
{{- if hasKey $owners $key -}}{{- fail (printf "targets.certificates: %s is listed under %s and %s: a workload carries one certificate's revision" $key (get $owners $key) $id) -}}{{- end -}}
{{- $_ := set $owners $key $id -}}
{{- end -}}
{{- end -}}
{{- $list | toJson -}}
{{- end -}}

{{/* The namespaces one target reads, unique and sorted, as JSON. Takes (dict "root" $ "target" $t). */}}
{{- define "ohmlab-controller.targetNamespaces" -}}
{{- $set := dict -}}
{{- if eq .target "certificates" -}}
{{- range $r := (include "ohmlab-controller.restarts" .root | fromJsonArray) -}}{{- $_ := set $set $r.namespace true -}}{{- end -}}
{{- else -}}
{{- range $ns := (index .root.Values.targets .target).watchNamespaces -}}{{- $_ := set $set $ns true -}}{{- end -}}
{{- end -}}
{{- keys $set | sortAlpha | toJson -}}
{{- end -}}

{{/* The namespaces a deployment's targets watch, unique and sorted, as JSON. */}}
{{- define "ohmlab-controller.watchedNamespaces" -}}
{{- $root := .root -}}
{{- $set := dict -}}
{{- range $t := .targets -}}
{{- range $ns := (include "ohmlab-controller.targetNamespaces" (dict "root" $root "target" $t) | fromJsonArray) -}}{{- $_ := set $set $ns true -}}{{- end -}}
{{- end -}}
{{- keys $set | sortAlpha | toJson -}}
{{- end -}}

{{/* RBAC rules of one target inside one of its namespaces. Takes (dict "root" $ "target" $t "namespace" $ns). */}}
{{- define "ohmlab-controller.namespaceRules" -}}
{{- if eq .target "vault" -}}
- apiGroups: [ohmlab.fr]
  resources: [vaultseeds]
  verbs: [get, list, watch, patch, update]
- apiGroups: [ohmlab.fr]
  resources: [vaultseeds/status]
  verbs: [get, patch, update]
- apiGroups: [events.k8s.io]
  resources: [events]
  verbs: [create, patch]
{{- else if eq .target "certificates" -}}
{{- $names := dict -}}
{{- range $r := (include "ohmlab-controller.restarts" .root | fromJsonArray) -}}
{{- if eq $r.namespace $.namespace -}}
{{- range $w := $r.workloads -}}
{{- $_ := set $names $w.kind (append (get $names $w.kind | default list) $w.name) -}}
{{- end -}}
{{- end -}}
{{- end }}
- apiGroups: [cert-manager.io]
  resources: [certificates]
  verbs: [get, list, watch]
{{- range $kind := keys $names | sortAlpha }}
# By name only: a workload this list does not hold cannot be read or rolled.
- apiGroups: [apps]
  resources: [{{ printf "%ss" (lower $kind) }}]
  resourceNames: {{ get $names $kind | uniq | sortAlpha | toJson }}
  verbs: [get, patch]
{{- end }}
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
