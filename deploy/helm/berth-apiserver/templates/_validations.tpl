{{- /*
  Render-time invariant checks. Called from the top of templates that
  render real resources, so any mis-configuration fails the helm install
  with a clear message rather than producing a broken pod spec.
*/ -}}

{{- define "berth-apiserver.validateTLS" -}}
{{- $cm := .Values.tls.certManager -}}
{{- $existing := .Values.tls.existingSecret -}}
{{- if and $cm.enabled $existing -}}
{{- fail "tls.certManager.enabled and tls.existingSecret are mutually exclusive — pick one TLS source." -}}
{{- end -}}
{{- if and (not $cm.enabled) (not $existing) -}}
{{- fail "TLS is required. Set tls.certManager.enabled=true (with an issuerRef) or tls.existingSecret=<name of a kubernetes.io/tls Secret>." -}}
{{- end -}}
{{- if and $cm.enabled (not $cm.issuerRef) -}}
{{- fail "tls.certManager.enabled=true but tls.certManager.issuerRef is empty — set issuerRef to a cert-manager Issuer or ClusterIssuer reference." -}}
{{- end -}}
{{- end -}}

{{- define "berth-apiserver.validateCoordination" -}}
{{- $c := .Values.coordination -}}
{{- if and $c.inCluster $c.kubeconfig.secretName -}}
{{- fail "coordination.inCluster=true and coordination.kubeconfig.secretName are mutually exclusive. Pick the in-cluster ServiceAccount path OR an external kubeconfig — not both." -}}
{{- end -}}
{{- if and $c.namespace (not $c.inCluster) (not $c.kubeconfig.secretName) -}}
{{- fail "coordination.namespace is set but no client is configured. Either set coordination.inCluster=true (the API server runs in the coordination cluster) or coordination.kubeconfig.secretName=<Secret with a kubeconfig> (external coordination cluster)." -}}
{{- end -}}
{{- if and (not $c.namespace) (gt (int .Values.replicaCount) 1) -}}
{{- fail "coordination.namespace is empty (in-memory store, dev mode) but replicaCount > 1. The in-memory store is per-pod, so replicas would diverge. Set coordination.namespace and a backend, or set replicaCount=1." -}}
{{- end -}}
{{- end -}}

{{- define "berth-apiserver.validateAuth" -}}
{{- $a := .Values.auth -}}
{{- if eq $a.mode "static-keys" -}}
  {{- if not $a.staticKeys.secretName -}}
  {{- fail "auth.mode=static-keys but auth.staticKeys.secretName is empty. Create a Secret with a single data key containing '<key-id>:<sha256-hex>' lines, then set auth.staticKeys.secretName." -}}
  {{- end -}}
{{- end -}}
{{- if eq $a.mode "oidc" -}}
  {{- if or (not $a.oidc.issuerURL) (not $a.oidc.audience) -}}
  {{- fail "auth.mode=oidc requires both auth.oidc.issuerURL and auth.oidc.audience." -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{- define "berth-apiserver.validate" -}}
{{- include "berth-apiserver.validateTLS" . -}}
{{- include "berth-apiserver.validateCoordination" . -}}
{{- include "berth-apiserver.validateAuth" . -}}
{{- end -}}
