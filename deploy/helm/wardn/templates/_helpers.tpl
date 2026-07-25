{{/*
Common labels for every object in the chart.
*/}}
{{- define "wardn.labels" -}}
app.kubernetes.io/part-of: wardn
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}
