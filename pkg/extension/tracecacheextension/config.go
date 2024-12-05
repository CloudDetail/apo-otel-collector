package tracecacheextension

import "time"

type Config struct {
	Enable bool `mapstructure:"enable"`

	CleanTime time.Duration `mapstructure:"clean_time"`
}
