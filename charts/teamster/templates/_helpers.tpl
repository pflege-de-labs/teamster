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
{{- if not (has $driver (list "sqlite" "postgres")) -}}
{{- fail (printf "database.driver=%s is not a known driver, use \"sqlite\" or \"postgres\"" $driver) -}}
{{- end -}}
{{- $kind := include "teamster.workloadKind" . -}}
{{- if not (has $kind (list "StatefulSet" "Deployment")) -}}
{{- fail (printf "workload.kind=%s is not supported, use \"StatefulSet\" or \"Deployment\"" $kind) -}}
{{- end -}}

{{- /* A secret written into the config file wins over the environment variable
       carrying it, so the chart refuses to render one rather than letting the
       precedence rule surprise somebody at three in the morning. */ -}}
{{- $settings := .Values.config.settings | default dict -}}
{{- range $section, $key := dict "webhook" "token" "admin" "password" "graph" "client-secret" -}}
{{- if dig $section $key "" $settings -}}
{{- fail (printf "config.settings.%s.%s is written into the config file, where it beats the environment variable that carries it. Use the credentials block." $section $key) -}}
{{- end -}}
{{- end -}}
{{- if dig "database" "postgres" "password" "" $settings -}}
{{- fail "config.settings.database.postgres.password is written into the config file, where it beats TEAMSTER_DATABASE_POSTGRES_PASSWORD. Use credentials.databasePassword or database.postgres.passwordFrom." -}}
{{- end -}}

{{- $metrics := dig "metrics" (dict) (.Values.config.settings | default dict) -}}
{{- if dig "enabled" false $metrics -}}
{{- /* Collecting with nowhere to send it is what teamster refuses on startup;
       refusing it here costs a render instead of a crash loop. */ -}}
{{- if and (not (dig "prometheus" true $metrics)) (not (dig "otlp-endpoint" "" $metrics)) -}}
{{- fail "config.settings.metrics.enabled=true with prometheus=false and no otlp-endpoint: metrics would be collected and exported nowhere, which teamster refuses on startup. Leave prometheus on to be scraped, or set otlp-endpoint to push." -}}
{{- end -}}
{{- /* addr only matters when something listens on it. A push-only deployment
       runs no listener, so its addr is never read and never wrong. */ -}}
{{- if include "teamster.metricsEnabled" . -}}
{{- $addr := dig "addr" ":9090" $metrics -}}
{{- $host := splitList ":" $addr | first -}}
{{- if or (eq $host "127.0.0.1") (eq $host "localhost") -}}
{{- fail (printf "config.settings.metrics.addr=%s binds loopback, which nothing outside the pod can reach — not the Service, not a ServiceMonitor, not a collector sidecar. Use \":%s\" and keep the listener private with a NetworkPolicy; it takes no credentials. (A push-only deployment — prometheus=false with an otlp-endpoint — runs no listener and is free of this.)" $addr (include "teamster.metricsPort" .)) -}}
{{- end -}}
{{- if eq (include "teamster.metricsPort" .) (include "teamster.containerPort" .) -}}
{{- fail (printf "config.settings.metrics.addr and server.addr both name port %s; the second listener would fail to bind and teamster would refuse the configuration." (include "teamster.metricsPort" .)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if and (dig "serviceMonitor" "enabled" false (.Values.metrics | default dict)) (not (include "teamster.metricsEnabled" .)) -}}
{{- fail "metrics.serviceMonitor.enabled needs a listener to scrape: set config.settings.metrics.enabled=true and leave config.settings.metrics.prometheus on." -}}
{{- end -}}

{{- if eq $driver "sqlite" -}}
{{- if gt (int .Values.replicaCount) 1 -}}
{{- fail "replicaCount must be 1 with database.driver=sqlite: SQLite takes a single writer, and a second replica would serve a database of its own. Set database.driver=postgres to run more than one." -}}
{{- end -}}
{{- if and (eq $kind "Deployment") .Values.persistence.enabled (not .Values.persistence.existingClaim) (not .Values.persistence.create) -}}
{{- fail "workload.kind=Deployment with database.driver=sqlite needs a volume that outlives the pod: set persistence.existingClaim, or persistence.create=true to have the chart manage the PersistentVolumeClaim." -}}
{{- end -}}
{{- else -}}
{{- if not .Values.database.postgres.host -}}
{{- fail "database.driver=postgres needs database.postgres.host. This chart does not deploy a Postgres; point it at one your cluster or your provider manages." -}}
{{- end -}}
{{- if and .Values.credentials.databasePassword .Values.database.postgres.passwordFrom.secretName -}}
{{- fail "set either credentials.databasePassword or database.postgres.passwordFrom, not both." -}}
{{- end -}}
{{- if and (not .Values.credentials.databasePassword) (not .Values.database.postgres.passwordFrom.secretName) (not .Values.credentials.existingSecret) -}}
{{- fail "database.driver=postgres needs a password: credentials.databasePassword, database.postgres.passwordFrom pointing at a secret somebody else manages, or TEAMSTER_DATABASE_POSTGRES_PASSWORD inside credentials.existingSecret." -}}
{{- end -}}
{{- /* persistence.enabled is ignored rather than refused, because Helm cannot
       tell its default true from an explicit one and --set database.driver=postgres
       on its own has to work. An existingClaim can only have been typed by hand. */ -}}
{{- if .Values.persistence.existingClaim -}}
{{- fail "database.driver=postgres keeps no local state, so persistence.existingClaim cannot be honoured. Remove it." -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
The workload kind, derived from the driver unless it is set: a StatefulSet for
sqlite, whose file lives on a claim that has to follow the pod, and a Deployment
for postgres, which keeps nothing locally at all.
*/}}
{{- define "teamster.workloadKind" -}}
{{- if .Values.workload.kind -}}
{{- .Values.workload.kind }}
{{- else if eq .Values.database.driver "postgres" -}}
Deployment
{{- else -}}
StatefulSet
{{- end -}}
{{- end }}

{{/*
Whether a data volume is mounted at all. Postgres deployments write nothing
outside /tmp.
*/}}
{{- define "teamster.usesDataVolume" -}}
{{- if eq .Values.database.driver "sqlite" -}}true{{- end -}}
{{- end }}

{{/*
How a Deployment rolls. A ReadWriteOnce volume admits one pod, so sqlite has to
stop the old one before starting the new; postgres has no such constraint, and
readiness already gates on the store and on draining, so no replica need be
given up during an update.
*/}}
{{- define "teamster.deploymentStrategy" -}}
{{- if .Values.workload.strategy -}}
{{- toYaml .Values.workload.strategy -}}
{{- else if eq .Values.database.driver "sqlite" -}}
type: Recreate
{{- else -}}
type: RollingUpdate
rollingUpdate:
  maxUnavailable: 0
  maxSurge: 1
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
{{- $database := dig "database" (dict) $settings -}}
{{- $_ := set $database "driver" .Values.database.driver -}}
{{- if eq .Values.database.driver "sqlite" -}}
{{- $_ := set $database "path" (include "teamster.databasePath" .) -}}
{{- else -}}
{{- $pg := dict
      "host" .Values.database.postgres.host
      "port" (int .Values.database.postgres.port)
      "dbname" .Values.database.postgres.dbname
      "user" .Values.database.postgres.user
      "sslmode" .Values.database.postgres.sslmode -}}
{{- with .Values.database.postgres.sslrootcert }}{{- $_ := set $pg "sslrootcert" . }}{{- end -}}
{{- /* No password key, ever: a config file value beats the environment
       variable that carries it, so writing one here would silently override
       the secret. */ -}}
{{- $_ := set $database "postgres" (merge (dig "postgres" (dict) $database) $pg) -}}
{{- end -}}
{{- with .Values.database.maxOpenConns }}{{- $_ := set $database "max-open-conns" (int .) }}{{- end -}}
{{- with .Values.database.maxIdleConns }}{{- $_ := set $database "max-idle-conns" (int .) }}{{- end -}}
{{- with .Values.database.connMaxLifetime }}{{- $_ := set $database "conn-max-lifetime" . }}{{- end -}}
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
Whether a metrics port exists to expose. The listener runs only when metrics
are on AND the Prometheus exporter is: an OTLP-only deployment pushes to a
collector and serves nothing, so there is no port to publish.
*/}}
{{- define "teamster.metricsEnabled" -}}
{{- $metrics := dig "metrics" (dict) (.Values.config.settings | default dict) -}}
{{- if and (dig "enabled" false $metrics) (dig "prometheus" true $metrics) -}}
true
{{- end -}}
{{- end }}

{{/*
Metrics port, taken from the configured listen address for the same reason the
container port is: the Service, the ServiceMonitor and the server cannot drift.
*/}}
{{- define "teamster.metricsPort" -}}
{{- $addr := dig "metrics" "addr" ":9090" (.Values.config.settings | default dict) -}}
{{- $port := splitList ":" $addr | last -}}
{{- default 9090 $port -}}
{{- end }}

{{/*
Path the exporter is served at.
*/}}
{{- define "teamster.metricsPath" -}}
{{- dig "metrics" "path" "/metrics" (.Values.config.settings | default dict) -}}
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
        {{- if include "teamster.metricsEnabled" $ }}
        - name: metrics
          containerPort: {{ include "teamster.metricsPort" $ }}
          protocol: TCP
        {{- end }}
      envFrom:
        - secretRef:
            name: {{ include "teamster.credentialsSecretName" $ }}
        {{- with $.Values.envFrom }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      {{- if or $.Values.env (and (eq $.Values.database.driver "postgres") $.Values.database.postgres.passwordFrom.secretName) }}
      env:
        {{- if and (eq $.Values.database.driver "postgres") $.Values.database.postgres.passwordFrom.secretName }}
        {{- /* env beats envFrom in Kubernetes, so this wins over whatever the
               credentials secret happens to carry. */}}
        - name: TEAMSTER_DATABASE_POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: {{ $.Values.database.postgres.passwordFrom.secretName }}
              key: {{ $.Values.database.postgres.passwordFrom.key }}
        {{- end }}
        {{- with $.Values.env }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
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
      {{- with $.Values.resources }}
      resources:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      volumeMounts:
        {{- /* The system-wide XDG location teamster already searches. */}}
        - name: config
          mountPath: /etc/xdg/teamster
          readOnly: true
        {{- if include "teamster.usesDataVolume" $ }}
        - name: data
          mountPath: {{ $.Values.persistence.mountPath }}
        {{- end }}
        {{- /* SQLite writes journal files next to the database, but the
               modernc driver still needs a writable temporary directory
               under a read-only root filesystem. */}}
        - name: tmp
          mountPath: /tmp
        {{- with $.Values.volumeMounts }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    {{- /* A collector sidecar shares the pod's network namespace, which is what
           makes otlp-endpoint: localhost:4318 the right answer there. */}}
    {{- with $.Values.sidecars }}
    {{- toYaml . | nindent 4 }}
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
    {{- if include "teamster.usesDataVolume" $ }}
    {{- with .dataVolume }}
    {{- toYaml (list .) | nindent 4 }}
    {{- end }}
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
