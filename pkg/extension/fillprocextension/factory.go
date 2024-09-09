package fillprocextension

import (
	"context"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/extension/fillprocextension/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

const (
	defaultSocketProcEnable   = true
	defaultSocketProcInterval = 5 * time.Second
)

func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createExtension(_ context.Context, settings extension.Settings, config component.Config) (extension.Extension, error) {
	return newSocketProcExtension(settings, config), nil
}

func createDefaultConfig() component.Config {
	return &Config{
		Enable:   defaultSocketProcEnable,
		Interval: defaultSocketProcInterval,
	}
}
