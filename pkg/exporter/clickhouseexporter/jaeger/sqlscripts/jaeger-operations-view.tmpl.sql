CREATE MATERIALIZED VIEW IF NOT EXISTS {{.OperationsViewTable}}
{{if .Cluster}}ON CLUSTER '{{.Cluster}}'{{end}}
TO {{.OperationsTable}}
AS SELECT
    {{if .Multitenant -}}
    tenant,
    {{- end -}}
    toDate(timestamp) AS date,
    service,
    operation,
    count() AS count,
    if(
        has(tags.key, 'span.kind'),
        tags.value[indexOf(tags.key, 'span.kind')],
        ''
    ) AS spankind
FROM {{.Database}}.{{.SpansIndexTable}}
GROUP BY
    {{if .Multitenant -}}
    tenant,
    {{- end -}}
    date,
    service,
    operation,
    tags.key,
    tags.value
