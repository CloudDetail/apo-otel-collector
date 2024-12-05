package tracecache

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceCache interface {
	CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]SpanMapping

	// 设置采样器
	SetSampler(sampler Sampler) error
}

type SpanMapping interface {
	GetEntrySpanName(spanId pcommon.SpanID) string
}

type Sampler interface {
	Name() string
	GetSampleTime() int
	GetDelayTime() int
	Sample(id pcommon.TraceID, traces *ptrace.Traces)
}
