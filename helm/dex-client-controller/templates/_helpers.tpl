{{/*
Copyright 2026 BitWise Media Group Ltd
SPDX-License-Identifier: MIT
*/}}

{{- define "dcc.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "dcc.fullname" -}}
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
{{- end }}

{{- define "dcc.serviceAccountName" -}}
{{- .Values.serviceAccount.name | default (include "dcc.fullname" .) -}}
{{- end }}

{{/*
Selector labels. Immutable on upgrade, so nothing release-varying beyond the
instance goes here.
*/}}
{{- define "dcc.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dcc.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Full label set. The bare `app` duplicates app.kubernetes.io/name for tooling
that predates the recommended-label set (kubescape C-0076 counts it).
*/}}
{{- define "dcc.labels" -}}
app: {{ include "dcc.name" . }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{ include "dcc.selectorLabels" . }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Object annotations: commonAnnotations plus optional per-object extras, which
win key-by-key. Context: dict "root" $ ["extra" <map>]. Empty output when both
maps are empty — wrap the annotations: key in `with`.
*/}}
{{- define "dcc.annotations" -}}
{{- $a := merge (dict) (.extra | default dict) (.root.Values.commonAnnotations | default dict) -}}
{{- if $a -}}
{{- toYaml $a -}}
{{- end -}}
{{- end }}

{{/*
Image reference: a digest pins (and beats the tag); the tag defaults to
v<appVersion>, which is how goreleaser tags the image.
*/}}
{{- define "dcc.image" -}}
{{- $img := .Values.image -}}
{{- if $img.digest -}}
{{- printf "%s@%s" $img.repository $img.digest -}}
{{- else -}}
{{- printf "%s:%s" $img.repository ($img.tag | default (printf "v%s" .Chart.AppVersion)) -}}
{{- end -}}
{{- end }}

{{/*
The Secret holding the controller's mTLS material for dex's gRPC API.
*/}}
{{- define "dcc.dexTLSSecretName" -}}
{{- .Values.dex.tls.secretName | default (printf "%s-dex-tls" (include "dcc.fullname" .)) -}}
{{- end }}
