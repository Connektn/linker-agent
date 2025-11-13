{{/*
Expand the name of the chart.
*/}}
{{- define "connektn-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "connektn-gateway.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "connektn-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "connektn-gateway.labels" -}}
helm.sh/chart: {{ include "connektn-gateway.chart" . }}
{{ include "connektn-gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: gateway
app.kubernetes.io/part-of: connektn
{{- end }}

{{/*
Selector labels
*/}}
{{- define "connektn-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "connektn-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "connektn-gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "connektn-gateway.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the upstream service FQDN
*/}}
{{- define "connektn-gateway.upstreamService" -}}
{{- if .Values.config.upstream.namespace }}
{{- printf "%s.%s.svc.cluster.local" .Values.config.upstream.serviceName .Values.config.upstream.namespace }}
{{- else }}
{{- printf "%s.%s.svc.cluster.local" .Values.config.upstream.serviceName .Release.Namespace }}
{{- end }}
{{- end }}
