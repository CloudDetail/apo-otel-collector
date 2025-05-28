package clickhouseexporter

import (
	"context"
	"fmt"
	"strings"
)

const DEFAULT_TENANT_DATABASE_PATTERN = `apo_tenant_{TENANT_ID}`

func (cfg *Config) tenantDB(ctx context.Context) string {
	v := ctx.Value("tenant_id")
	if v == nil {
		return cfg.Database
	}

	tenantID, ok := v.(string)
	if !ok || len(tenantID) == 0 {
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
