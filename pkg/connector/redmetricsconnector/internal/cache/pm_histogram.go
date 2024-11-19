package cache

import (
	"sort"
)

type PromHistogram struct {
	count         uint64
	sum           float64
	bucketCounts  []uint64
	latencyBounds []float64
}

func NewPromHistogram(latencyBounds []float64) *PromHistogram {
	return &PromHistogram{
		bucketCounts:  make([]uint64, len(latencyBounds)+1),
		latencyBounds: latencyBounds,
	}
}

func (h *PromHistogram) Update(v float64) {
	h.sum += v
	h.count++
	// Binary search to find the latencyInMilliseconds bucket index.
	index := sort.SearchFloat64s(h.latencyBounds, v)
	h.bucketCounts[index]++
}

func (h *PromHistogram) Read() HistogramValues {
	return h
}

func (values *PromHistogram) GetBuckets() map[string]uint64 {
	panic("unsupported GetBuckets")
}

func (values *PromHistogram) GetBucketCounts() []uint64 {
	return values.bucketCounts
}

func (values *PromHistogram) GetKey() string {
	return "le"
}

func (values *PromHistogram) GetCount() uint64 {
	return values.count
}

func (values *PromHistogram) GetSum() float64 {
	return values.sum
}
