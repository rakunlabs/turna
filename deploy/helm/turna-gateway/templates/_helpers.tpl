{{- define "turna-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "turna-gateway.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "turna-gateway.proxyServiceAccount" -}}
{{- printf "%s-proxy" (include "turna-gateway.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "turna-gateway.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{- define "turna-gateway.labels" -}}
app.kubernetes.io/name: {{ include "turna-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "turna-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "turna-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* "true" when proxies are created in the namespace of their Gateway */}}
{{- define "turna-gateway.gatewayPlacement" -}}
{{- $placement := .Values.proxy.placement | default "controller" -}}
{{- if not (has $placement (list "controller" "gateway")) -}}
{{- fail "proxy.placement must be controller or gateway" -}}
{{- end -}}
{{- if and (eq $placement "gateway") (eq .Values.scope "namespace") -}}
{{- fail "proxy.placement gateway needs scope cluster" -}}
{{- end -}}
{{- and (eq $placement "gateway") (ne .Values.mode "standalone") -}}
{{- end -}}

{{/* "true" when every proxy gets its own ServiceAccount and a Role reading
only its certificate Secret; scope namespace cannot bind cluster roles and
keeps a shared ServiceAccount */}}
{{- define "turna-gateway.proxyIdentity" -}}
{{- and (ne .Values.mode "standalone") (eq .Values.scope "cluster") -}}
{{- end -}}

{{- define "turna-gateway.config" -}}
{{- $k8s := dict
  "enabled" true
  "mode" .Values.mode
  "controller_name" .Values.controllerName
  "controller_id" (printf "%s/%s" .Release.Namespace (include "turna-gateway.fullname" .))
  "namespaces" .Values.watchNamespaces
  "port_offset" .Values.portOffset
  "health_address" ":9901"
  "backend" (dict "allow_external_name" .Values.allowExternalNameBackends)
  "status" (dict
    "leader_election" (dict "namespace" .Release.Namespace "name" (include "turna-gateway.fullname" .))
  )
-}}
{{- if eq .Values.scope "namespace" -}}
{{- $_ := set $k8s "namespaces" (list .Release.Namespace) -}}
{{- $_ := set $k8s "gateway_class" .Values.gatewayClass.name -}}
{{- $_ := set $k8s "namespace_scoped" true -}}
{{- else if ne .Values.scope "cluster" -}}
{{- fail "scope must be cluster or namespace" -}}
{{- end -}}
{{- if eq .Values.mode "standalone" -}}
{{- $_ := set $k8s.status "service" (printf "%s/%s" .Release.Namespace (include "turna-gateway.fullname" .)) -}}
{{- else -}}
{{- $pullSecrets := list -}}
{{- range .Values.imagePullSecrets -}}
{{- $pullSecrets = append $pullSecrets .name -}}
{{- end -}}
{{- $_ := set $k8s "proxy" (dict
  "namespace" .Release.Namespace
  "image_pull_policy" .Values.image.pullPolicy
  "image_pull_secrets" $pullSecrets
  "service_account" (include "turna-gateway.proxyServiceAccount" .)
  "replicas" .Values.proxy.replicas
  "service_type" .Values.proxy.serviceType
  "addresses_annotation" (.Values.proxy.addressesAnnotation | default "")
  "log_level" .Values.proxy.logLevel
  "shutdown_delay" .Values.proxy.shutdownDelay
  "cpu_request" .Values.proxy.resources.cpuRequest
  "memory_request" .Values.proxy.resources.memoryRequest
  "cpu_limit" .Values.proxy.resources.cpuLimit
  "memory_limit" .Values.proxy.resources.memoryLimit
) -}}
{{- if eq (include "turna-gateway.gatewayPlacement" .) "true" -}}
{{- $_ := set $k8s.proxy "gateway_namespace" true -}}
{{- end -}}
{{- if eq (include "turna-gateway.proxyIdentity" .) "true" -}}
{{- $_ := set $k8s.proxy "cluster_role" (printf "%s-reader" (include "turna-gateway.fullname" .)) -}}
{{- $_ := unset $k8s.proxy "service_account" -}}
{{- end -}}
{{- end -}}
{{- $config := dict "log_level" .Values.logLevel "providers" (dict "kubernetes" $k8s) -}}
{{- mustMergeOverwrite $config .Values.extraConfig | toYaml -}}
{{- end -}}
