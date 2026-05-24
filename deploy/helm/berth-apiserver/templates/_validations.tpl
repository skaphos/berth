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
{{- $backend := .Values.store.backend -}}
{{- if eq $backend "sql" -}}
  {{- if or $c.namespace $c.inCluster $c.kubeconfig.secretName -}}
  {{- fail "store.backend=sql does not use coordination.* settings. Remove coordination.namespace, coordination.inCluster, and coordination.kubeconfig.secretName." -}}
  {{- end -}}
{{- else if eq $backend "mem" -}}
  {{- if or $c.namespace $c.inCluster $c.kubeconfig.secretName -}}
  {{- fail "store.backend=mem does not use coordination.* settings. Remove coordination.namespace, coordination.inCluster, and coordination.kubeconfig.secretName." -}}
  {{- end -}}
  {{- if gt (int .Values.replicaCount) 1 -}}
  {{- fail "store.backend=mem requires replicaCount=1. The in-memory store is per-pod, so replicas would diverge." -}}
  {{- end -}}
{{- else -}}
  {{- if and $c.inCluster $c.kubeconfig.secretName -}}
  {{- fail "coordination.inCluster=true and coordination.kubeconfig.secretName are mutually exclusive. Pick the in-cluster ServiceAccount path OR an external kubeconfig — not both." -}}
  {{- end -}}
  {{- if and (eq $backend "k8s") (not $c.namespace) -}}
  {{- fail "store.backend=k8s requires coordination.namespace." -}}
  {{- end -}}
  {{- if and $c.namespace (not $c.inCluster) (not $c.kubeconfig.secretName) -}}
  {{- fail "coordination.namespace is set but no client is configured. Either set coordination.inCluster=true (the API server runs in the coordination cluster) or coordination.kubeconfig.secretName=<Secret with a kubeconfig> (external coordination cluster)." -}}
  {{- end -}}
  {{- if and (not $c.namespace) (gt (int .Values.replicaCount) 1) -}}
  {{- fail "coordination.namespace is empty (in-memory store, dev mode) but replicaCount > 1. The in-memory store is per-pod, so replicas would diverge. Set coordination.namespace and a backend, or set replicaCount=1." -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{- define "berth-apiserver.validateSQLStore" -}}
{{- if eq .Values.store.backend "sql" -}}
{{- $sql := .Values.store.sql -}}
  {{- if not $sql.driver -}}
  {{- fail "store.backend=sql requires store.sql.driver to be one of postgres, mysql, or sqlite." -}}
  {{- end -}}
  {{- if and $sql.dsn $sql.dsnSecret.name -}}
  {{- fail "store.sql.dsn and store.sql.dsnSecret.name are mutually exclusive. Prefer store.sql.dsnSecret.name in Kubernetes deployments." -}}
  {{- end -}}
  {{- if and (not $sql.dsn) (not $sql.dsnSecret.name) -}}
  {{- fail "store.backend=sql requires store.sql.dsnSecret.name or store.sql.dsn." -}}
  {{- end -}}
  {{- if and $sql.dsnSecret.name (not $sql.dsnSecret.key) -}}
  {{- fail "store.sql.dsnSecret.key is required when store.sql.dsnSecret.name is set." -}}
  {{- end -}}
  {{- if and (eq $sql.driver "sqlite") (gt (int .Values.replicaCount) 1) -}}
  {{- fail "store.backend=sql with store.sql.driver=sqlite requires replicaCount=1. SQLite is a single-writer backend and is not HA across API server replicas." -}}
  {{- end -}}
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
{{- include "berth-apiserver.validateSQLStore" . -}}
{{- include "berth-apiserver.validateAuth" . -}}
{{- end -}}
