package redmetricsconnector

import "time"

type Config struct {
	// 是否采集服务端RED指标
	ServerEnabled bool `mapstructure:"server_enabled"`
	// 是否采集对外调用RED指标
	ExternalEnabled bool `mapstructure:"external_enabled"`
	// 是否采集DB调用RED指标
	DbEnabled bool `mapstructure:"db_enabled"`
	// 是否采集MQ调用RED指标
	MqEnabled bool `mapstructure:"mq_enabled"`
	// RED Key缓存的数量，超过则清理，避免内存泄漏。此外也用于清理已失效PID数据.
	DimensionsCacheSize int `mapstructure:"dimensions_cache_size"`
	// 指标采集周期（秒），默认60
	MetricsFlushInterval time.Duration `mapstructure:"metrics_flush_interval"`
	// 最大监控服务数，超过则将服务名打标为 overflow_service.
	MaxServicesToTrack int `mapstructure:"max_services_to_track"`
	// 每个服务下最大URL数，超过则将URL打标为 overflow_operation.
	MaxOperationsToTrackPerService int `mapstructure:"max_operations_to_track_per_service"`
	// Http对外调用URL收敛算法.
	HttpParser string `mapstructure:"http_parser"`
	// 指标类型，vm 或 pm.
	MetricsType string `mapstructure:"metrics_type"`
	// Promethues场景下需指定分桶.
	LatencyHistogramBuckets []time.Duration `mapstructure:"latency_histogram_buckets"`
}
