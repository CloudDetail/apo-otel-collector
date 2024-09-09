package fillprocextension

import "time"

type Config struct {
	Enable   bool          `mapstructure:"enable"`
	Interval time.Duration `mapstructure:"interval"`
}
