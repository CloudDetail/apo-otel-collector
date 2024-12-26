package parser

import (
	"fmt"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"github.com/CloudDetail/apo-otel-collector/pkg/sqlprune"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

type DbParser struct {
}

func NewDbParser() *DbParser {
	return &DbParser{}
}

func (parser *DbParser) Parse(logger *zap.Logger, pid string, containerId string, serviceName string, entryUrl string, span *ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string {
	if span.Kind() != ptrace.SpanKindClient {
		return ""
	}
	dbSystemAttr, systemExist := spanAttr.Get(conventions.AttributeDBSystem)
	if !systemExist {
		return ""
	}

	name := ""
	dbSystem := dbSystemAttr.Str()
	dbName := getAttrValueWithDefault(spanAttr, conventions.AttributeDBName, "")
	if dbSystem == "redis" || dbSystem == "memcached" || dbSystem == "aerospike" {
		name = span.Name()
	} else {
		dbOperateAttr, operateExist := spanAttr.Get(conventions.AttributeDBOperation)
		dbTableAttr, tableExist := spanAttr.Get(conventions.AttributeDBSQLTable)
		if tableExist && operateExist {
			if dbName != "" {
				// SELECT <db>.<table>
				name = fmt.Sprintf("%s %s.%s", dbOperateAttr.Str(), dbName, dbTableAttr.Str())
			} else {
				// SELECT <table>
				name = fmt.Sprintf("%s %s", dbOperateAttr.Str(), dbTableAttr.Str())
			}
		} else {
			if dbStatement, sqlExist := spanAttr.Get(conventions.AttributeDBStatement); sqlExist {
				if operationName, tableName := sqlprune.SQLParseOperationAndTableNEW(dbStatement.Str()); operationName != "" {
					if dbName != "" {
						name = fmt.Sprintf("%s %s.%s", operationName, dbName, tableName)
					} else {
						name = fmt.Sprintf("%s %s", operationName, tableName)
					}
				} else {
					logger.Info("UnParsed sql",
						zap.String("span", span.Name()),
						zap.String("type", dbSystem),
						zap.String("sql", dbStatement.Str()),
					)
				}
			}
		}
		if name == "" {
			name = span.Name()
		}
	}
	return buildDbKey(keyValue, pid, containerId, serviceName, entryUrl,
		dbSystem, dbName,
		name,                    // SELECT db.table
		GetClientPeer(spanAttr), // ip:port
		span.Status().Code() == ptrace.StatusCodeError, // IsError
	)
}
