package adaptive

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/traceutil"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type SampleResult int

const (
	Sampled    SampleResult = 0
	SampleDrop SampleResult = 1
	SampleSlow SampleResult = 2
	SampleLow  SampleResult = 3
)

func (result SampleResult) IsDrop() bool {
	return result == SampleDrop
}

func (result SampleResult) IsSingleStore() bool {
	return result == SampleSlow || result == SampleLow
}

func (result SampleResult) GetReportType() uint32 {
	switch result {
	case SampleSlow | SampleLow:
		return 5
	default:
		return 0
	}
}

type AdaptiveSampler struct {
	sampleValue       *atomic.Int64
	slowThreshold     uint64
	serviceUrlCount   *atomic.Int32
	serviceTps        sync.Map // <service_url, tps>
	singleIds         sync.Map // <traceId, expireTime>
	traceIdExpireTime int32

	serviceWindow      int
	serviceWindowCount int32
	currentWindow      int
}

func NewAdaptiveSampler(slowThreshold time.Duration, serviceWindow time.Duration, serviceCount int, traceIdExpireTime time.Duration) *AdaptiveSampler {
	return &AdaptiveSampler{
		sampleValue:        &atomic.Int64{},
		slowThreshold:      uint64(slowThreshold.Nanoseconds()),
		serviceUrlCount:    &atomic.Int32{},
		serviceWindow:      int(serviceWindow.Seconds()),
		serviceWindowCount: int32(serviceCount),
		traceIdExpireTime:  int32(traceIdExpireTime.Seconds()),
		currentWindow:      0,
	}
}

func (sampler *AdaptiveSampler) GetSampleValue() int64 {
	return sampler.sampleValue.Load()
}

func (sampler *AdaptiveSampler) SetSampleValue(newValue int64) {
	sampler.sampleValue.Store(newValue)
}

func (sampler *AdaptiveSampler) Sample(traceId pcommon.TraceID, swTraceId string, serviceName string, spans []*ptrace.Span) SampleResult {
	if sampler.CheckAdaptiveSampleValue(traceId, swTraceId) {
		return Sampled
	}

	if _, ok := sampler.singleIds.Load(traceId); ok {
		return SampleLow
	}

	for _, span := range spans {
		if uint64(span.EndTimestamp()) > uint64(span.StartTimestamp())+sampler.slowThreshold {
			sampler.singleIds.Store(traceId, &atomic.Int32{})
			return SampleSlow
		}
		if sampler.CheckLowUrl(traceId, serviceName, span) {
			return SampleLow
		}
	}
	return SampleDrop
}

func (sampler *AdaptiveSampler) CheckAdaptiveSampleValue(traceId pcommon.TraceID, swTraceId string) bool {
	sampleValue := sampler.sampleValue.Load()
	if sampleValue == 0 {
		return true
	}

	index := 15
	if len(swTraceId) > 32 {
		// skywalking java
		index = 10
	}
	value := uint32(traceId[index-1])<<8 | uint32(traceId[index])
	return value&(1<<sampleValue-1) == 0
}

func (sampler *AdaptiveSampler) CheckLowUrl(traceId pcommon.TraceID, serviceName string, span *ptrace.Span) bool {
	if sampler.serviceWindowCount > 0 && traceutil.IsEntrySpan(span) {
		key := fmt.Sprintf("%s-%s", serviceName, span.Name())

		var tps *atomic.Int32
		if tpsInterface, found := sampler.serviceTps.Load(key); found {
			tps = tpsInterface.(*atomic.Int32)
		} else {
			tps = &atomic.Int32{}
			sampler.serviceTps.Store(key, tps)
			sampler.serviceUrlCount.Add(1)
		}
		if tps.Add(1) <= sampler.serviceWindowCount {
			sampler.singleIds.Store(traceId, &atomic.Int32{})
			return true
		}
	}

	return false
}

func (sampler *AdaptiveSampler) Reset() {
	sampler.currentWindow++
	if sampler.currentWindow >= sampler.serviceWindow {
		sampler.currentWindow = 0

		if sampler.serviceUrlCount.Load() >= 10000 {
			// Url is not convergence
			sampler.serviceTps.Range(func(k, v interface{}) bool {
				sampler.serviceTps.Delete(k)
				return true
			})
		} else {
			sampler.serviceTps.Range(func(k, v interface{}) bool {
				v.(*atomic.Int32).Store(0)
				return true
			})
		}
	}

	sampler.singleIds.Range(func(k, v interface{}) bool {
		holdTime := v.(*atomic.Int32)
		if holdTime.Add(1) >= sampler.traceIdExpireTime {
			sampler.singleIds.Delete(k)
		}
		return true
	})
}
