package tracecacheextension

import "time"

type Config struct {
	Enable bool `mapstructure:"enable"`

	WaitTime time.Duration `mapstructure:"wait_time"`
}
