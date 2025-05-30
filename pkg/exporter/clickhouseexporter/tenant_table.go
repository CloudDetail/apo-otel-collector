package clickhouseexporter

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/client"
)

const DEFAULT_TENANT_DATABASE_PATTERN = `apo_tenant_{TENANT_ID}`

func (cfg *Config) tenant(ctx context.Context) string {
	info := client.FromContext(ctx)
	v := info.Metadata.Get("tenant_id")
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (cfg *Config) tenantDB(ctx context.Context) string {
	tenantID := cfg.tenant(ctx)
	if len(tenantID) <= 0 {
		return cfg.Database
	}
	if db, exist := cfg.TenantDBMap[tenantID]; exist {
		return db
	}

	if len(cfg.TenantDBPattern) > 0 {
		return strings.ReplaceAll(cfg.TenantDBPattern, "{TENANT_ID}", tenantID)
	}

	return strings.ReplaceAll(DEFAULT_TENANT_DATABASE_PATTERN, "{TENANT_ID}", tenantID)
}

func (cfg *Config) GetTracesTableName(ctx context.Context) string {
	if len(cfg.tenantDB(ctx)) == 0 {
		return cfg.TracesTableName
	}
	return fmt.Sprintf(`"%s"."%s"`, cfg.tenantDB(ctx), cfg.TracesTableName)
}

func (cfg *Config) GetLogsTableName(ctx context.Context) string {
	if len(cfg.tenantDB(ctx)) == 0 {
		return cfg.LogsTableName
	}
	return fmt.Sprintf(`"%s"."%s"`, cfg.tenantDB(ctx), cfg.LogsTableName)
}

func (cfg *Config) GetMetricTableName(ctx context.Context) string {
	if len(cfg.tenantDB(ctx)) == 0 {
		return cfg.MetricsTableName
	}
	return fmt.Sprintf(`"%s"."%s"`, cfg.tenantDB(ctx), cfg.MetricsTableName)
}
