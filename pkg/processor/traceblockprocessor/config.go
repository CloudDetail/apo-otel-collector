package traceblockprocessor

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
)

type Config struct {
	IdleTTL        time.Duration `mapstructure:"idle_ttl"`
	BlockThreshold time.Duration `mapstructure:"block_threshold"`
	BlockedIdleTTL time.Duration `mapstructure:"blocked_idle_ttl"`
}

var _ component.Config = (*Config)(nil)

// Validate validates the configuration.
func (cfg *Config) Validate() error {
	if cfg.IdleTTL <= 0 {
		return errors.New("idle_ttl must be greater than 0")
	}
	if cfg.BlockThreshold <= 0 {
		return errors.New("block_threshold must be greater than 0")
	}
	if cfg.BlockedIdleTTL <= 0 {
		return errors.New("blocked_idle_ttl must be greater than 0")
	}
	// It's recommended that blocked_idle_ttl should be greater than idle_ttl to ensure logical correctness
	if cfg.BlockedIdleTTL <= cfg.IdleTTL {
		return errors.New("blocked_idle_ttl should be greater than idle_ttl to ensure blocking logic works properly")
	}
	// It's recommended that block_threshold should be greater than idle_ttl to ensure logical correctness
	if cfg.BlockThreshold <= cfg.IdleTTL {
		return errors.New("block_threshold should be greater than idle_ttl to ensure blocking logic works properly")
	}
	return nil
}
