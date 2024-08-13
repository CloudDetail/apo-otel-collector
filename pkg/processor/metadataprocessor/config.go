package metadataprocessor

import (
	metacfg "github.com/CloudDetail/metadata/configs"
	"go.opentelemetry.io/collector/component"
)

// Config defines configuration for batch processor.
type Config struct {
	metacfg.MetaSourceConfig `mapstructure:",squash"`
	MetricPrefix             string `mapstructure:"metric_prefix"`
}

var _ component.Config = (*Config)(nil)

// Validate checks if the processor configuration is valid
func (cfg *Config) Validate() error {
	// TODO check config
	return nil
}
