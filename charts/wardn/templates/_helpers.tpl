{{/*
Chart name, optionally overridden.
*/}}
{{- define "wardn.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name (prefix for every resource).
*/}}
{{- define "wardn.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "wardn.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels applied to every object.
*/}}
{{- define "wardn.labels" -}}
helm.sh/chart: {{ include "wardn.chart" . }}
{{ include "wardn.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: wardn
{{- end -}}

{{/*
Base selector labels (component label is added per workload).
*/}}
{{- define "wardn.selectorLabels" -}}
app.kubernetes.io/name: {{ include "wardn.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name to use.
*/}}
{{- define "wardn.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "wardn.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Name of the Secret holding wardn's credentials (chart-created or user-supplied).
*/}}
{{- define "wardn.secretName" -}}
{{- if .Values.existingSecret -}}
{{- .Values.existingSecret -}}
{{- else -}}
{{- include "wardn.fullname" . -}}
{{- end -}}
{{- end -}}

{{/*
Component resource names.
*/}}
{{- define "wardn.backend.fullname" -}}{{ include "wardn.fullname" . }}-backend{{- end -}}
{{- define "wardn.frontend.fullname" -}}{{ include "wardn.fullname" . }}-frontend{{- end -}}
{{- define "wardn.postgres.fullname" -}}{{ include "wardn.fullname" . }}-postgres{{- end -}}

{{/*
Resolve an image ref: "<global.imageRegistry>/<repository>:<tag|appVersion>".
Usage: {{ include "wardn.image" (dict "img" .Values.backend.image "ctx" $) }}
*/}}
{{- define "wardn.image" -}}
{{- $img := .img -}}
{{- $ctx := .ctx -}}
{{- $tag := $img.tag | default $ctx.Chart.AppVersion -}}
{{- $repo := $img.repository -}}
{{- with $ctx.Values.global.imageRegistry -}}
{{- printf "%s/%s:%s" . $repo $tag -}}
{{- else -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end -}}
