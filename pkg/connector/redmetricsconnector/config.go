package redmetricsconnector

import "time"

type Config struct {
	ServerEnabled                  bool            `mapstructure:"server_enabled"`
	ExternalEnabled                bool            `mapstructure:"external_enabled"`
	DbEnabled                      bool            `mapstructure:"db_enabled"`
	MqEnabled                      bool            `mapstructure:"mq_enabled"`
	ClientEntryUrlEnabled          bool            `mapstructure:"client_entry_url_enabled"`
	CacheEntryUrlTime              time.Duration   `mapstructure:"cache_entry_url_time"`
	UnMatchUrlExpireTime           time.Duration   `mapstructure:"unmatch_url_expire_time"`
	DimensionsCacheSize            int             `mapstructure:"dimensions_cache_size"`
	MetricsFlushInterval           time.Duration   `mapstructure:"metrics_flush_interval"` // Default - 60
	MaxServicesToTrack             int             `mapstructure:"max_services_to_track"`
	MaxOperationsToTrackPerService int             `mapstructure:"max_operations_to_track_per_service"`
	HttpParser                     string          `mapstructure:"http_parser"`
	MetricsType                    string          `mapstructure:"metrics_type"` // vm or pm.
	LatencyHistogramBuckets        []time.Duration `mapstructure:"latency_histogram_buckets"`
}
