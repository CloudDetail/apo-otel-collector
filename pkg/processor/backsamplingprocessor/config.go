package backsamplingprocessor

import "time"

type Config struct {
	EbpfPort   int            `mapstructure:"ebpf_port"`
	Adaptive   AdaptiveConfig `mapstructure:"adaptive"`
	Sampler    SamplerConfig  `mapstructure:"sampler"`
	Controller ControllerCfg  `mapstructure:"controller"`
	Notify     NotifyCfg      `mapstructure:"notify"`
}

type AdaptiveConfig struct {
	Enable              bool          `mapstructure:"enable"`
	SlowThreshold       time.Duration `mapstructure:"span_slow_threshold"`
	ServiceSampleWindow time.Duration `mapstructure:"service_sample_window"`
	ServiceSampleCount  int           `mapstructure:"service_sample_count"`
	MemoryCheckInterval time.Duration `mapstructure:"memory_check_interval"`
	MemoryLimitMib      uint64        `mapstructure:"memory_limit_mib_threshold"`
	TraceIdHoldTime     time.Duration `mapstructure:"traceid_holdtime"`
}

type SamplerConfig struct {
	LogEnable               bool   `mapstructure:"log_enable"`
	NormalTopSample         bool   `mapstructure:"normal_top_sample"`
	NormalSampleWaitTime    int64  `mapstructure:"normal_sample_wait_time"`
	OpenSlowSampling        bool   `mapstructure:"open_slow_sampling"`
	OpenErrorSampling       bool   `mapstructure:"open_error_sampling"`
	EnableTailbaseProfiling bool   `mapstructure:"enable_tail_base_profiling"`
	SampleRetryNum          int    `mapstructure:"sample_trace_repeat_num"`
	SampleWaitTime          int    `mapstructure:"sample_trace_wait_time"`
	SampleIgnoreThreshold   uint64 `mapstructure:"sample_trace_ignore_threshold"`
	SilentPeriod            int64  `mapstructure:"silent_period"`
	SilentCount             int    `mapstructure:"silent_count"`
	SilentMode              string `mapstructure:"silent_mode"` // window / wait
}

type ControllerCfg struct {
	Host                string `mapstructure:"host"`
	Port                int    `mapstructure:"port"`
	IntervalQuerySlow   int64  `mapstructure:"interval_query_slow_threshold"`
	IntervalQuerySample int64  `mapstructure:"interval_query_sample"`
	IntervalSendTrace   int64  `mapstructure:"interval_send_trace"`
}

type NotifyCfg struct {
	Enabled bool `mapstructure:"enabled"`
}
