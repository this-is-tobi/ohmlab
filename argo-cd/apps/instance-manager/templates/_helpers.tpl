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
The names of the instance as Helm parameters, one `global.ohmlab.names.<key>`
each, for every generated Application. An unknown key or an empty name fails
the render: either would leave the product default in place unnoticed. So do
quotes, backslashes and braces: Argo CD evaluates the Application template,
and charts insert names into JSON and Vault policies as they are.
*/}}
{{- define "instance-manager.namesParameters" -}}
{{- $known := list "gateway" "keycloakRealmDisplayName" "traefikGateway" "vaultMount" -}}
{{- range $key, $name := .Values.names | default dict }}
{{- if not (has $key $known) }}
{{- fail (printf "names.%s is not a name the product reads (%s)" $key (join ", " $known)) }}
{{- end }}
{{- if or (not (kindIs "string" $name)) (not $name) }}
{{- fail (printf "names.%s must be a non-empty string" $key) }}
{{- end }}
{{- if regexMatch `["\\{}]` $name }}
{{- fail (printf "names.%s cannot contain quotes, backslashes or braces" $key) }}
{{- end }}
- name: {{ printf "global.ohmlab.names.%s" $key | quote }}
  value: {{ $name | quote }}
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
