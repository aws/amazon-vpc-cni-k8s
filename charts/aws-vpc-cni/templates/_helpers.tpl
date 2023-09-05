{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "aws-vpc-cni.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "aws-vpc-cni.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aws-vpc-cni.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "aws-vpc-cni.labels" -}}
app.kubernetes.io/name: {{ include "aws-vpc-cni.name" . }}
helm.sh/chart: {{ include "aws-vpc-cni.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
k8s-app: aws-node
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "aws-vpc-cni.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "aws-vpc-cni.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
The aws-vpc-cni-init image to use
*/}}
{{- define "aws-vpc-cni.initImage" -}}
{{- if .Values.init.image.override }}
{{- .Values.init.image.override }}
{{- else }}
{{- printf "%s.dkr.%s.%s.%s/amazon-k8s-cni-init:%s" .Values.init.image.account .Values.init.image.endpoint .Values.init.image.region .Values.init.image.domain .Values.init.image.tag }}
{{- end }}
{{- end }}

{{/*
The aws-vpc-cni image to use
*/}}
{{- define "aws-vpc-cni.image" -}}
{{- if .Values.image.override }}
{{- .Values.image.override }}
{{- else }}
{{- printf "%s.dkr.%s.%s.%s/amazon-k8s-cni:%s" .Values.image.account .Values.image.endpoint .Values.image.region .Values.image.domain .Values.image.tag }}
{{- end }}
{{- end }}

{{/*
The aws-network-policy-agent image to use
*/}}
{{- define "aws-vpc-cni.nodeAgentImage" -}}
{{- if .Values.nodeAgent.image.override }}
{{- .Values.nodeAgent.image.override }}
{{- else }}
{{- printf "%s.dkr.%s.%s.%s/amazon/aws-network-policy-agent:%s" .Values.nodeAgent.image.account .Values.nodeAgent.image.endpoint .Values.nodeAgent.image.region .Values.nodeAgent.image.domain .Values.nodeAgent.image.tag }}
{{- end -}}
{{- end -}}

{{/*
The aws-network-policy-agent port to bind to for metrics
*/}}
{{- define "aws-vpc-cni.nodeAgentMetricsBindAddr" -}}
{{- printf ":%s" .Values.nodeAgent.metricsBindAddr }}
{{- end -}}

{{/*
The aws-network-policy-agent port to bind to for health probes
*/}}
{{- define "aws-vpc-cni.nodeAgentHealthProbeBindAddr" -}}
{{- printf ":%s" .Values.nodeAgent.healthProbeBindAddr }}
{{- end -}}
