//go:generate mdatagen metadata.yaml

package backsamplingprocessor // import "github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"

	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/metadata"
)

// NewFactory returns a new factory for the Tail Sampling processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		metadata.Type,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, metadata.TracesStability))
}

func createDefaultConfig() component.Config {
	return &Config{
		EbpfPort: 0,
		Sampler: SamplerConfig{
			LogEnable:               false,
			NormalTopSample:         false,
			NormalSampleWaitTime:    60,
			OpenSlowSampling:        true,
			OpenErrorSampling:       true,
			EnableTailbaseProfiling: false,
			SampleRetryNum:          3,
			SampleWaitTime:          60,
			SampleIgnoreThreshold:   0,
			SampleSlowThreshold:     100,
			SilentPeriod:            5,
			SilentCount:             1,
			SilentMode:              "window",
		},
		Notify: NotifyCfg{
			Enabled: false,
		},
	}
}

func createTracesProcessor(
	ctx context.Context,
	params processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (processor.Traces, error) {
	tCfg := cfg.(*Config)
	return newTracesProcessor(ctx, params.TelemetrySettings, nextConsumer, *tCfg)
}
