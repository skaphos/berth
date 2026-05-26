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

{{/*
Name of the injection webhook's Service and MutatingWebhookConfiguration.
*/}}
{{- define "berth-operator.webhookName" -}}
{{- printf "%s-injection" (include "berth-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Serving-cert Secret for the webhook listener. Either an externally-created
Secret (webhook.tls.existingSecret) or the cert-manager-managed Secret
named after the webhook.
*/}}
{{- define "berth-operator.webhookTLSSecretName" -}}
{{- with .Values.injection.webhook.tls.existingSecret -}}
{{- . -}}
{{- else -}}
{{- printf "%s-tls" (include "berth-operator.webhookName" .) -}}
{{- end -}}
{{- end -}}

{{/*
Directory the controller-runtime webhook server reads tls.crt/tls.key from.
*/}}
{{- define "berth-operator.webhookCertDir" -}}
/tmp/k8s-webhook-server/serving-certs
{{- end -}}
