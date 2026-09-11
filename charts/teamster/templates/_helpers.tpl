{{/*
Expand the name of the chart.
*/}}
{{- define "teamster.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "teamster.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "teamster.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "teamster.labels" -}}
helm.sh/chart: {{ include "teamster.chart" . }}
{{ include "teamster.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "teamster.selectorLabels" -}}
app.kubernetes.io/name: {{ include "teamster.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "teamster.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "teamster.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Reject value combinations the application cannot honour. Rendered from every
workload template, so a mistake fails at `helm template` rather than as a pod
that cannot start or a database that silently corrupts.
*/}}
{{- define "teamster.validate" -}}
{{- $driver := .Values.database.driver -}}
{{- if eq $driver "postgres" -}}
{{- fail "database.driver=postgres: teamster stores its state in SQLite only. Use database.driver=sqlite; see charts/teamster/README.md." -}}
{{- else if ne $driver "sqlite" -}}
{{- fail (printf "database.driver=%s is not a known driver, use \"sqlite\"" $driver) -}}
{{- end -}}
{{- if not (has .Values.workload.kind (list "StatefulSet" "Deployment")) -}}
{{- fail (printf "workload.kind=%s is not supported, use \"StatefulSet\" or \"Deployment\"" .Values.workload.kind) -}}
{{- end -}}
{{- if gt (int .Values.replicaCount) 1 -}}
{{- fail "replicaCount must be 1: SQLite takes a single writer, and a second replica would serve a database of its own." -}}
{{- end -}}
{{- if and (eq .Values.workload.kind "Deployment") .Values.persistence.enabled (not .Values.persistence.existingClaim) (not .Values.persistence.create) -}}
{{- fail "workload.kind=Deployment needs a volume that outlives the pod: set persistence.existingClaim, or persistence.create=true to have the chart manage the PersistentVolumeClaim." -}}
{{- end -}}
{{- end }}

{{/*
Name of the secret holding the rendered config.yaml.
*/}}
{{- define "teamster.configSecretName" -}}
{{- if .Values.config.existingSecret -}}
{{- .Values.config.existingSecret -}}
{{- else -}}
{{- printf "%s-config" (include "teamster.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Name of the secret holding the credentials injected as TEAMSTER_* environment
variables.
*/}}
{{- define "teamster.credentialsSecretName" -}}
{{- if .Values.credentials.existingSecret -}}
{{- .Values.credentials.existingSecret -}}
{{- else -}}
{{- printf "%s-credentials" (include "teamster.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Path of the SQLite file. It lives on the data volume, so the mount point and the
configured path cannot be set independently.
*/}}
{{- define "teamster.databasePath" -}}
{{- printf "%s/%s" (.Values.persistence.mountPath | trimSuffix "/") .Values.database.sqlite.fileName -}}
{{- end }}

{{/*
The config file the container reads, as YAML. The chart owns database.path,
because it is the one setting that has to agree with the volume mount; the rest
is .Values.config.settings verbatim, in teamster's own hyphenated key names.
*/}}
{{- define "teamster.configYaml" -}}
{{- $settings := deepCopy (.Values.config.settings | default dict) -}}
{{- $database := merge (dig "database" (dict) $settings) (dict "path" (include "teamster.databasePath" .)) -}}
{{- $_ := set $settings "database" $database -}}
{{- /* An empty section is noise in the rendered file; kong ignores it either way. */ -}}
{{- $out := dict -}}
{{- range $key, $value := $settings -}}
{{- if not (empty $value) -}}
{{- $_ := set $out $key $value -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end }}

{{/*
Container port, taken from the configured listen address so the Service, the
probes and the server cannot drift apart.
*/}}
{{- define "teamster.containerPort" -}}
{{- $addr := dig "server" "addr" ":8080" (.Values.config.settings | default dict) -}}
{{- $port := splitList ":" $addr | last -}}
{{- default 8080 $port -}}
{{- end }}

{{/*
Name of the claim carrying the SQLite file in Deployment mode.
*/}}
{{- define "teamster.claimName" -}}
{{- if .Values.persistence.existingClaim -}}
{{- .Values.persistence.existingClaim -}}
{{- else -}}
{{- printf "%s-data" (include "teamster.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Pod template shared by the StatefulSet and the Deployment. The two differ only
in how the data volume is attached: a volumeClaimTemplate there, a named claim
here, so the volume itself is passed in as the "dataVolume" key.
*/}}
{{- define "teamster.podTemplate" -}}
{{- $ := .root -}}
metadata:
  annotations:
    {{- /* roll the pod when the rendered config or credentials change */}}
    checksum/config: {{ include (print $.Template.BasePath "/secret-config.yaml") $ | sha256sum }}
    checksum/credentials: {{ include (print $.Template.BasePath "/secret-credentials.yaml") $ | sha256sum }}
    {{- with $.Values.podAnnotations }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  labels:
    {{- include "teamster.labels" $ | nindent 4 }}
    {{- with $.Values.podLabels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
spec:
  {{- with $.Values.imagePullSecrets }}
  imagePullSecrets:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  serviceAccountName: {{ include "teamster.serviceAccountName" $ }}
  {{- with $.Values.podSecurityContext }}
  securityContext:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- /* Must outlast server.shutdown-timeout, or draining is cut short. */}}
  terminationGracePeriodSeconds: {{ $.Values.terminationGracePeriodSeconds }}
  containers:
    - name: {{ $.Chart.Name }}
      image: "{{ $.Values.image.repository }}:{{ $.Values.image.tag | default $.Chart.AppVersion }}"
      imagePullPolicy: {{ $.Values.image.pullPolicy }}
      {{- with $.Values.securityContext }}
      securityContext:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      args:
        - serve
        {{- with $.Values.extraArgs }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      ports:
        - name: http
          containerPort: {{ include "teamster.containerPort" $ }}
          protocol: TCP
      envFrom:
        - secretRef:
            name: {{ include "teamster.credentialsSecretName" $ }}
        {{- with $.Values.envFrom }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      {{- with $.Values.env }}
      env:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- /* Off unless a deployment asks for it: see the note in values.yaml. */}}
      {{- with $.Values.startupProbe }}
      startupProbe:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $.Values.livenessProbe }}
      livenessProbe:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $.Values.readinessProbe }}
      readinessProbe:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $.Values.startupProbe }}
      startupProbe:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $.Values.resources }}
      resources:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      volumeMounts:
        {{- /* The system-wide XDG location teamster already searches. */}}
        - name: config
          mountPath: /etc/xdg/teamster
          readOnly: true
        - name: data
          mountPath: {{ $.Values.persistence.mountPath }}
        {{- /* SQLite writes journal files next to the database, but the
               modernc driver still needs a writable temporary directory
               under a read-only root filesystem. */}}
        - name: tmp
          mountPath: /tmp
        {{- with $.Values.volumeMounts }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
  volumes:
    - name: config
      secret:
        secretName: {{ include "teamster.configSecretName" $ }}
        items:
          - key: config.yaml
            path: config.yaml
    - name: tmp
      emptyDir: {}
    {{- with .dataVolume }}
    {{- toYaml (list .) | nindent 4 }}
    {{- end }}
    {{- with $.Values.volumes }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  {{- with $.Values.nodeSelector }}
  nodeSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $.Values.affinity }}
  affinity:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $.Values.tolerations }}
  tolerations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $.Values.topologySpreadConstraints }}
  topologySpreadConstraints:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $.Values.priorityClassName }}
  priorityClassName: {{ . }}
  {{- end }}
{{- end }}
