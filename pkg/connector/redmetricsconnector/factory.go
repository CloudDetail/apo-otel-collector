//go:generate mdatagen metadata.yaml

// Package fileexporter exports data to files.
package redmetricsconnector // import "github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector"

import (
	"context"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/metadata"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"
)

// NewFactory creates a factory for the spanmetrics connector.
func NewFactory() connector.Factory {
	return connector.NewFactory(
		metadata.Type,
		createDefaultConfig,
		connector.WithTracesToMetrics(createTracesToMetricsConnector, metadata.TracesToMetricsStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		ServerEnabled:                  false,
		DbEnabled:                      false,
		DimensionsCacheSize:            1000,
		MetricsFlushInterval:           60 * time.Second,
		MaxServicesToTrack:             256,
		MaxOperationsToTrackPerService: 2048,
	}
}

func createTracesToMetricsConnector(ctx context.Context, params connector.Settings, cfg component.Config, nextConsumer consumer.Metrics) (connector.Traces, error) {
	c, err := newConnector(params.Logger, cfg, metricsTicker(ctx, cfg))
	if err != nil {
		return nil, err
	}
	c.metricsConsumer = nextConsumer
	return c, nil
}

func metricsTicker(ctx context.Context, cfg component.Config) *clock.Ticker {
	return clock.FromContext(ctx).NewTicker(cfg.(*Config).MetricsFlushInterval)
}
