package clickhouseexporter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/template"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/jaegertracing/jaeger/model"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/clickhouseexporter/jaeger"
	otlp2jaeger "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/jaeger"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func (e *tracesExporter) initJaegerDatabaseIfNotExist(logger *zap.Logger, db *sql.DB) error {
	cfg := e.cfg.JaegerCFG
	var (
		sqlStatements []string
		ttlTimestamp  string
		ttlDate       string
	)

	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s %s", e.cfg.Database, e.cfg.clusterString())
	_, err := e.client.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	if e.cfg.TTL > 0 {
		day := e.cfg.TTL % (24 * time.Hour)
		if day <= 0 {
			day = 1
		}
		ttlTimestamp = fmt.Sprintf("TTL timestamp + INTERVAL %d DAY DELETE", day)
		ttlDate = fmt.Sprintf("TTL date + INTERVAL %d DAY DELETE", day)
	}

	if cfg.InitSQLScriptsDir != "" {
		filePaths, err := jaeger.WalkMatch(cfg.InitSQLScriptsDir, "*.sql")
		if err != nil {
			return fmt.Errorf("could not list sql files: %q", err)
		}
		sort.Strings(filePaths)
		for _, f := range filePaths {
			sqlStatement, err := os.ReadFile(filepath.Clean(f))
			if err != nil {
				return err
			}
			sqlStatements = append(sqlStatements, string(sqlStatement))
		}
	}

	if cfg.InitTables {
		templates := template.Must(template.ParseFS(jaeger.SQLScripts, "sqlscripts/*.tmpl.sql"))

		args := jaeger.TableArgs{
			Database: e.cfg.Database,

			SpansIndexTable:     cfg.SpansIndexTable,
			SpansTable:          cfg.SpansTable,
			OperationsTable:     cfg.OperationsTable,
			OperationsViewTable: jaeger.DefaultOperationsTable.ToView(),
			SpansArchiveTable:   cfg.SpansTable + "_archive",

			TTLTimestamp: ttlTimestamp,
			TTLDate:      ttlDate,

			Multitenant: cfg.MultiTenant,
			Replication: false,
			Cluster:     e.cfg.ClusterName,
		}

		if e.cfg.ClusterName != "" {
			// Add "_local" to the local table names, and omit it from the distributed tables below
			args.SpansIndexTable = args.SpansIndexTable.ToLocal()
			args.SpansTable = args.SpansTable.ToLocal()
			args.OperationsTable = args.OperationsTable.ToLocal()
			args.SpansArchiveTable = args.SpansArchiveTable.ToLocal()
		}

		sqlStatements = append(sqlStatements, jaeger.Render(templates, "jaeger-index.tmpl.sql", args))
		sqlStatements = append(sqlStatements, jaeger.Render(templates, "jaeger-operations.tmpl.sql", args))
		sqlStatements = append(sqlStatements, jaeger.Render(templates, "jaeger-operations-view.tmpl.sql", args))
		sqlStatements = append(sqlStatements, jaeger.Render(templates, "jaeger-spans.tmpl.sql", args))
		sqlStatements = append(sqlStatements, jaeger.Render(templates, "jaeger-spans-archive.tmpl.sql", args))

		if e.cfg.ClusterName != "" {
			// Now these tables omit the "_local" suffix
			distargs := jaeger.DistributedTableArgs{
				Cluster:  e.cfg.ClusterName,
				Table:    cfg.SpansTable,
				Database: e.cfg.Database,
				Hash:     "cityHash64(traceID)",
			}
			sqlStatements = append(sqlStatements, jaeger.Render(templates, "distributed-table.tmpl.sql", distargs))

			distargs.Table = cfg.SpansIndexTable
			sqlStatements = append(sqlStatements, jaeger.Render(templates, "distributed-table.tmpl.sql", distargs))

			distargs.Table = cfg.SpansTable + "_archive"
			sqlStatements = append(sqlStatements, jaeger.Render(templates, "distributed-table.tmpl.sql", distargs))

			distargs.Table = cfg.OperationsTable
			distargs.Hash = "rand()"
			sqlStatements = append(sqlStatements, jaeger.Render(templates, "distributed-table.tmpl.sql", distargs))
		}
	}
	return jaeger.ExecuteScripts(logger, sqlStatements, db)
}

func (e *tracesExporter) pushJaegerTraceData(ctx context.Context, td ptrace.Traces) error {
	batches, err := otlp2jaeger.ProtoFromTraces(td)
	if err != nil {
		return err
	}
	start := time.Now()
	err = e.flushJaeger(e.client, e.cfg.tenant(ctx), batches)
	if err != nil {
		return err
	}
	duration := time.Since(start)
	e.logger.Debug("insert traces", zap.Int("records", td.SpanCount()),
		zap.String("cost", duration.String()))
	return err
}

func (e *tracesExporter) flushJaeger(db *sql.DB, tenant string, batches []*model.Batch) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			// Clickhouse does not support real rollback
			_ = tx.Rollback()
		}
	}()

	var query string
	if tenant == "" {
		query = fmt.Sprintf("INSERT INTO %s (timestamp, traceID, model) VALUES (?, ?, ?)", e.cfg.JaegerCFG.SpansTable)
	} else {
		query = fmt.Sprintf("INSERT INTO %s (tenant, timestamp, traceID, model) VALUES (?, ?, ?, ?)", e.cfg.JaegerCFG.SpansTable)
	}

	statement, err := tx.Prepare(query)
	if err != nil {
		return err
	}

	defer statement.Close()

	for _, batch := range batches {
		for _, span := range batch.Spans {
			var serialized []byte

			if e.cfg.JaegerCFG.Encoding == jaeger.JSONEncoding {
				serialized, err = json.Marshal(span)
			} else {
				serialized, err = proto.Marshal(span)
			}

			if err != nil {
				return err
			}

			if tenant == "" {
				_, err = statement.Exec(span.StartTime, span.TraceID.String(), serialized)
			} else {
				_, err = statement.Exec(tenant, span.StartTime, span.TraceID.String(), serialized)
			}
			if err != nil {
				return err
			}
		}
	}

	committed = true
	return tx.Commit()
}
