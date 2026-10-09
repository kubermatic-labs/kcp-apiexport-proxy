{{/*
Copyright The kcp-apiexport-proxy Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/}}

{{- define "kcp-apiexport-proxy.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kcp-apiexport-proxy.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kcp-apiexport-proxy.labels" -}}
{{ include "kcp-apiexport-proxy.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "kcp-apiexport-proxy.tokenSecretName" -}}
{{- .Values.token.existingSecret | default (printf "%s-token" (include "kcp-apiexport-proxy.fullname" .)) -}}
{{- end -}}

{{- define "kcp-apiexport-proxy.scheme" -}}
{{- if .Values.tls.enabled }}https{{ else }}http{{ end -}}
{{- end -}}

{{- define "kcp-apiexport-proxy.tlsSecretName" -}}
{{- printf "%s-tls" (include "kcp-apiexport-proxy.fullname" .) -}}
{{- end -}}
