package tracecacheextension

import (
	"context"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/extension/tracecacheextension/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createExtension(_ context.Context, settings extension.Settings, cfg component.Config) (extension.Extension, error) {
	tCfg := cfg.(*Config)
	return newTraceCacheExtension(settings, tCfg)
}

func createDefaultConfig() component.Config {
	return &Config{
		Enable:    true,
		CleanTime: 30 * time.Second,
	}
}
