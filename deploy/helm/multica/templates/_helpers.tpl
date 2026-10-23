{{/*
Common labels for all resources.
*/}}
{{- define "multica.labels" -}}
app.kubernetes.io/name: multica
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Per-component resource names. Using Release.Name keeps the same name we used
under the kustomize layout when installed as `helm install multica ...`.
*/}}
{{- define "multica.backend.fullname" -}}
{{ .Release.Name }}-backend
{{- end -}}

{{- define "multica.frontend.fullname" -}}
{{ .Release.Name }}-frontend
{{- end -}}

{{- define "multica.postgres.fullname" -}}
{{ .Release.Name }}-postgres
{{- end -}}

{{/*
DATABASE_URL defaults to the embedded SQLite database path when postgres.enabled
is false, or the legacy postgres URL when postgres.enabled is true.
*/}}
{{- define "multica.databaseUrl" -}}
{{- if .Values.postgres.enabled -}}
postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@{{ include "multica.postgres.fullname" . }}:5432/$(POSTGRES_DB)?sslmode=disable
{{- else -}}
{{ default "file:/app/data/db/multica.db" .Values.backend.config.databaseUrl }}
{{- end -}}
{{- end -}}
