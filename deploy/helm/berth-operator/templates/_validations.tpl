{{- define "berth-operator.validate" -}}

{{- if not .Values.berth.apiServer -}}
{{- fail "berth.apiServer is required — set it to the URL of the Berth API server, e.g. https://berth.example.com:8443." -}}
{{- end -}}

{{- if not .Values.clusterID -}}
{{- fail "clusterID is required for the cross-cluster singleton pattern. Set it to a value distinct from every other cluster running berth-operator (e.g. clusterID: cluster-east)." -}}
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

{{- end -}}
