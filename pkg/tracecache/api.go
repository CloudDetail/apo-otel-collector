package tracecache

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceCache interface {
	IsEnable() bool

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
	Sample(id pcommon.TraceID, traces []*TraceData)
}

type TraceData struct {
	Resource *pcommon.Resource
	Spans    []*ptrace.Span
}

func NewTraceData(resource *pcommon.Resource, spans []*ptrace.Span) *TraceData {
	return &TraceData{
		Resource: resource,
		Spans:    spans,
	}
}

func BuildTraces(traceDatas []*TraceData) ptrace.Traces {
	traces := ptrace.NewTraces()
	for _, traceData := range traceDatas {
		rs := traces.ResourceSpans().AppendEmpty()
		traceData.Resource.CopyTo(rs.Resource())
		ils := rs.ScopeSpans().AppendEmpty()
		for _, span := range traceData.Spans {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)
		}
	}
	return traces
}
