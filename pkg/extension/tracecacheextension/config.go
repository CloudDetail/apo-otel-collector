package tracecacheextension

import "time"

type Config struct {
	Enable bool `mapstructure:"enable"`

	SampleTime time.Duration `mapstructure:"sample_time"`

	CleanTime time.Duration `mapstructure:"clean_time"`
}
