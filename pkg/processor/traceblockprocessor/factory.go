package traceblockprocessor // import "github.com/CloudDetail/apo-otel-collector/pkg/processor/traceblockprocessor"

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

var (
	Type            = component.MustNewType("traceblock")
	TracesStability = component.StabilityLevelBeta
)

// NewFactory 创建 TraceBlockProcessor 的工厂。
func NewFactory() processor.Factory {
	return processor.NewFactory(
		Type,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, TracesStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		IdleTTL:        5 * time.Minute,
		BlockThreshold: 30 * time.Minute,
		BlockedIdleTTL: 10 * time.Minute,
	}
}

func createTracesProcessor(
	ctx context.Context,
	params processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (processor.Traces, error) {
	tCfg := cfg.(*Config)
	return newTraceBlockProcessor(ctx, params.TelemetrySettings, *tCfg, nextConsumer)
}
