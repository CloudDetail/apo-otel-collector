package metadataprocessor

import (
	"context"

	metacfg "github.com/CloudDetail/metadata/configs"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

var (
	Type             = component.MustNewType("metadata")
	MetricsStability = component.StabilityLevelDevelopment
)

// NewFactory returns a new factory for the Batch processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		Type,
		createDefaultConfig,
		processor.WithMetrics(createMetrics, MetricsStability))
}

func createDefaultConfig() component.Config {
	return &Config{
		MetaSourceConfig: metacfg.MetaSourceConfig{
			KubeSource: &metacfg.KubeSourceConfig{
				KubeAuthType:      "serviceAccount",
				IsEndpointsNeeded: true,
			},
			Querier: &metacfg.QuerierConfig{
				QueryServerPort: 0,
				IsSingleCluster: true,
			},
		},
		MetricPrefix: "kindling_",
	}
}

func createMetrics(
	_ context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	return newMetadataProcessor(set, nextConsumer, cfg.(*Config))
}
