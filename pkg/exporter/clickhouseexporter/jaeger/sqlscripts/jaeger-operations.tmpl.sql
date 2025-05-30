CREATE TABLE IF NOT EXISTS {{.OperationsTable}}
    {{if .Cluster}}ON CLUSTER '{{.Cluster}}'{{end}}
(
    {{if .Multitenant -}}
      tenant LowCardinality(String),
    {{- end -}}
    date Date,
    service LowCardinality(String),
    operation LowCardinality(String),
    count UInt64,
    spankind String
    )
    ENGINE = {{if .Replication}}ReplicatedSummingMergeTree('/clickhouse/tables/{uuid}/{shard}', '{replica}'){{else}}SummingMergeTree{{end}}
    {{.TTLDate}}
    PARTITION BY (
    {{if .Multitenant -}}
      tenant,
    {{- end -}}
      toYYYYMM(date)
    )
    ORDER BY (
    {{if .Multitenant -}}
      tenant,
    {{- end -}}
      date,
      service,
      operation
     )
    SETTINGS index_granularity = 32
