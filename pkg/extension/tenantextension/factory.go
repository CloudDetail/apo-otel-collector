package tenantauth

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/CloudDetail/apo-otel-collector/pkg/extension/tenantextension/internal/metadata"
)

const (
	defaultHeader = "Authorization"
	defaultScheme = "Bearer"
)

// NewFactory creates a factory for the static bearer token Authenticator extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Header: defaultHeader,
		Scheme: defaultScheme,
	}
}

func createExtension(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return newBearerTokenAuth(cfg.(*Config), set.Logger), nil
}
