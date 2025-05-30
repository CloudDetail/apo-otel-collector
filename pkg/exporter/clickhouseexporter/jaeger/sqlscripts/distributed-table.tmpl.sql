CREATE TABLE IF NOT EXISTS {{.Table}}
    ON CLUSTER '{{.Cluster}}' AS {{.Database}}.{{.Table}}_local
    ENGINE = Distributed('{{.Cluster}}', {{.Database}}, {{.Table}}_local, {{.Hash}})
