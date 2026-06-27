{{- define "berth-operator.validate" -}}

{{- if not .Values.berth.apiServer -}}
{{- fail "berth.apiServer is required — set it to the URL of the Berth API server, e.g. https://berth.example.com:8443." -}}
{{- end -}}

{{- if not .Values.clusterID -}}
{{- fail "clusterID is required for the cross-cluster singleton pattern. Set it to a value distinct from every other cluster running berth-operator (e.g. clusterID: cluster-east)." -}}
{{- end -}}

{{- if and (gt (int .Values.replicaCount) 1) (not .Values.leaderElection.enabled) -}}
{{- fail "replicaCount > 1 requires leaderElection.enabled=true — without leader election multiple replicas would double the central Berth API load and race on BerthLease status writes. Set leaderElection.enabled: true or keep replicaCount: 1." -}}
{{- end -}}

{{- /* Token sources are mutually exclusive. apiKey + tokenFile + sidecarBroker
       all do the same job from the operator's point of view (one provides the
       bearer token), but at most one should be configured. */ -}}
{{- $apiKey := and .Values.berth.apiKey .Values.berth.apiKey.secretName -}}
{{- $tokenFile := .Values.berth.tokenFile.path -}}
{{- $broker := .Values.sidecarBroker.enabled -}}
{{- $count := 0 -}}
{{- if $apiKey }}{{- $count = add $count 1 -}}{{- end -}}
{{- if $tokenFile }}{{- $count = add $count 1 -}}{{- end -}}
{{- if $broker }}{{- $count = add $count 1 -}}{{- end -}}
{{- if gt $count 1 -}}
{{- fail "Configure at most one of berth.apiKey.secretName, berth.tokenFile.path, or sidecarBroker.enabled — they are mutually exclusive ways to supply the operator's bearer token." -}}
{{- end -}}

{{- if .Values.sidecarBroker.enabled -}}
  {{- $b := .Values.sidecarBroker.oidc -}}
  {{- if or (not $b.issuerURL) (not $b.clientID) (not $b.clientSecret.secretName) -}}
  {{- fail "sidecarBroker.enabled=true requires sidecarBroker.oidc.issuerURL, sidecarBroker.oidc.clientID, and sidecarBroker.oidc.clientSecret.secretName." -}}
  {{- end -}}
{{- end -}}

{{- if .Values.berth.tls.insecureSkipVerify -}}
  {{- /* informational; not a failure */ -}}
{{- end -}}

{{- /* Injection webhook (SKA-440). Only validated when enabled. */ -}}
{{- if .Values.injection.enabled -}}
  {{- if not .Values.injection.helper.repository -}}
  {{- fail "injection.enabled=true requires injection.helper.repository — the berth-acquire image stamped into opted-in pods." -}}
  {{- end -}}
  {{- if not (hasPrefix "/" .Values.injection.helper.stateDir) -}}
  {{- fail "injection.helper.stateDir must be an absolute path starting with '/' — it is injected as the shared-state Pod volumeMount.mountPath and used to build the probe marker paths." -}}
  {{- end -}}
  {{- /* Auth/CA file paths need a mountable source, or the helper points at a
         path that doesn't exist (SKA-444). Require each file with its source. */ -}}
  {{- $h := .Values.injection.helper -}}
  {{- if and $h.apiKeyFile (not (hasPrefix "/" $h.apiKeyFile)) -}}
  {{- fail "injection.helper.apiKeyFile must be an absolute path starting with '/' — its parent directory becomes the injected helper's volumeMount.mountPath." -}}
  {{- end -}}
  {{- if and $h.caBundleFile (not (hasPrefix "/" $h.caBundleFile)) -}}
  {{- fail "injection.helper.caBundleFile must be an absolute path starting with '/' — its parent directory becomes the injected helper's volumeMount.mountPath." -}}
  {{- end -}}
  {{- if and $h.apiKeyFile (not $h.apiKeySecret.name) -}}
  {{- fail "injection.helper.apiKeyFile is set but injection.helper.apiKeySecret.name is empty — the webhook needs a Secret to mount the token at that path." -}}
  {{- end -}}
  {{- if and $h.apiKeySecret.name (not $h.apiKeyFile) -}}
  {{- fail "injection.helper.apiKeySecret.name is set but injection.helper.apiKeyFile is empty — set the in-pod path the token mounts to." -}}
  {{- end -}}
  {{- if and $h.caBundleFile (not $h.caBundleConfigMap.name) -}}
  {{- fail "injection.helper.caBundleFile is set but injection.helper.caBundleConfigMap.name is empty — the webhook needs a ConfigMap to mount the CA bundle at that path." -}}
  {{- end -}}
  {{- if and $h.caBundleConfigMap.name (not $h.caBundleFile) -}}
  {{- fail "injection.helper.caBundleConfigMap.name is set but injection.helper.caBundleFile is empty — set the in-pod path the CA bundle mounts to." -}}
  {{- end -}}
  {{- $tls := .Values.injection.webhook.tls -}}
  {{- $cm := $tls.certManager -}}
  {{- if and $cm.enabled $tls.existingSecret -}}
  {{- fail "injection.webhook.tls.certManager.enabled and injection.webhook.tls.existingSecret are mutually exclusive — pick one serving-cert source." -}}
  {{- end -}}
  {{- if and (not $cm.enabled) (not $tls.existingSecret) -}}
  {{- fail "injection.enabled=true requires a webhook serving certificate. Set injection.webhook.tls.certManager.enabled=true (with an issuerRef) or injection.webhook.tls.existingSecret=<kubernetes.io/tls Secret>." -}}
  {{- end -}}
  {{- if and $cm.enabled (not $cm.issuerRef) -}}
  {{- fail "injection.webhook.tls.certManager.enabled=true but injection.webhook.tls.certManager.issuerRef is empty — set issuerRef to a cert-manager Issuer or ClusterIssuer reference." -}}
  {{- end -}}
  {{- if and $tls.existingSecret (not $tls.caBundle) -}}
  {{- fail "injection.webhook.tls.existingSecret is set but injection.webhook.tls.caBundle is empty. Supply the base64-encoded PEM CA chain so the API server can verify the webhook (cert-manager populates this automatically; existing Secrets cannot)." -}}
  {{- end -}}
{{- end -}}

{{- end -}}
