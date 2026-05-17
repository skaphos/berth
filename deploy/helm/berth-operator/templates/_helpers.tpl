{{- define "berth-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "berth-operator.fullname" -}}
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

{{- define "berth-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "berth-operator.labels" -}}
helm.sh/chart: {{ include "berth-operator.chart" . }}
{{ include "berth-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: berth
{{- end -}}

{{- define "berth-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "berth-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "berth-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "berth-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Path the operator reads the bearer token from. Defaults to the sidecar
broker's tokenPath when the sidecar is enabled; otherwise honors an
explicit berth.tokenFile.path.
*/}}
{{- define "berth-operator.tokenFilePath" -}}
{{- if .Values.sidecarBroker.enabled -}}
{{- .Values.sidecarBroker.tokenPath -}}
{{- else -}}
{{- .Values.berth.tokenFile.path -}}
{{- end -}}
{{- end -}}
