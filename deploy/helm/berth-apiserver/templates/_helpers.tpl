{{/*
Expand the name of the chart.
*/}}
{{- define "berth-apiserver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Full qualified app name. Allow override via .Values.fullnameOverride.
*/}}
{{- define "berth-apiserver.fullname" -}}
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

{{/*
Chart label.
*/}}
{{- define "berth-apiserver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "berth-apiserver.labels" -}}
helm.sh/chart: {{ include "berth-apiserver.chart" . }}
{{ include "berth-apiserver.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: berth
{{- end -}}

{{/*
Selector labels (used by Deployment.spec.selector and Service.spec.selector;
must be stable and a strict subset of the full label set).
*/}}
{{- define "berth-apiserver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "berth-apiserver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name.
*/}}
{{- define "berth-apiserver.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "berth-apiserver.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
TLS Secret name. Either an externally-created Secret (existingSecret) or
the cert-manager-managed Secret named after the release.
*/}}
{{- define "berth-apiserver.tlsSecretName" -}}
{{- if .Values.tls.existingSecret -}}
{{- .Values.tls.existingSecret -}}
{{- else -}}
{{- printf "%s-tls" (include "berth-apiserver.fullname" .) -}}
{{- end -}}
{{- end -}}
