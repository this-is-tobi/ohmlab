{{- /*
A Bundle's target: its ConfigMap in every target namespace of trust-manager,
or only in the `namespaces` its values list. Those must be targets: trust-manager
writes nowhere else. Takes (dict "root" $ "key" <values key>).
*/}}
{{- define "trust-manager-app.target" -}}
{{- $bundle := index .root.Values .key }}
{{- $targets := index .root.Values "trust-manager" "app" "targetNamespaces" | default list }}
{{- range $bundle.namespaces }}
{{- if and $targets (not (has . $targets)) }}
{{- fail (printf "%s.namespaces: %s is not in trust-manager.app.targetNamespaces, where trust-manager may write" $.key .) }}
{{- end }}
{{- end }}
configMap:
  key: ca.crt
{{- with $bundle.namespaces }}
namespaceSelector:
  matchExpressions:
  - key: kubernetes.io/metadata.name
    operator: In
    values:
    {{- toYaml . | nindent 4 }}
{{- end }}
{{- end }}
