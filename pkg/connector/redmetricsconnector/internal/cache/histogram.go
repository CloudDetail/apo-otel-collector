package cache

type MetricsType string

const (
	MetricsProm MetricsType = "prom"
	MetricsVm   MetricsType = "vm"
)

func GetMetricsType(metricsType string) MetricsType {
	if metricsType == string(MetricsProm) {
		return MetricsProm
	}
	return MetricsVm
}

type Histogram interface {
	Update(value float64)
	Read() HistogramValues
}

type HistogramValues interface {
	GetBuckets() map[string]uint64
	GetBucketCounts() []uint64
	GetKey() string
	GetCount() uint64
	GetSum() float64
}

func NewHistogram(metricType MetricsType, latencyBounds []float64) Histogram {
	if metricType == MetricsProm {
		return NewPromHistogram(latencyBounds)
	}
	return NewVmHistogram()
}
