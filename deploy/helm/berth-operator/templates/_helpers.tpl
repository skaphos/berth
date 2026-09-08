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
Port number from a controller-runtime bind address. Accepts exactly these
forms: ":<port>", "<host>:<port>" (host without colons or brackets),
"[<ipv6>]:<port>", a bare "<port>", and "0" or "" (both disable the listener
and yield "0"). Anything else fails rendering: a URL, an unbracketed IPv6
address, or any other shape that happens to end in ":<digits>" would render
a plausible containerPort and then fail to bind at runtime.
Usage:
  {{ include "berth-operator.bindPort" (dict "name" "metrics.bindAddress" "addr" .Values.metrics.bindAddress) }}
*/}}
{{- define "berth-operator.bindPort" -}}
{{- $addr := .addr | toString | trim -}}
{{- if or (eq $addr "") (eq $addr "0") -}}
0
{{- else -}}
{{- /* Brackets must enclose at least one character and no whitespace is
       allowed anywhere: "[]:9090" or "127.0.0.1 :9090" would render a
       plausible port and then fail to bind. */ -}}
{{- if not (regexMatch `^(\[[^\]\s]+\]|[^:\[\]\s]*)?:[0-9]+$|^[0-9]+$` $addr) -}}
{{- fail (printf "%s=%q is not a supported bind address: use \":<port>\", \"<host>:<port>\", \"[<ipv6>]:<port>\", a bare \"<port>\", or \"0\" to disable the listener" .name $addr) -}}
{{- end -}}
{{- $port := regexReplaceAll "^.*:" $addr "" -}}
{{- /* Strip leading zeros before conversion: sprig's int parses "08080" as
       octal and yields 0, and a raw "08080" literal is octal to YAML 1.1. */ -}}
{{- $port = regexReplaceAll "^0+" $port "" -}}
{{- if eq $port "" -}}{{- $port = "0" -}}{{- end -}}
{{- if or (lt (int $port) 1) (gt (int $port) 65535) -}}
{{- fail (printf "%s=%q: port %s is outside 1-65535" .name $addr $port) -}}
{{- end -}}
{{- /* Emit a normalized decimal so ":08080" renders containerPort: 8080
       rather than a leading-zero literal that YAML 1.1 may read as octal. */ -}}
{{- int $port -}}
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
