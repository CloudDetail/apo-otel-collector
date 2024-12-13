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

	SetConnector(connector Connector)

	HasConnector() bool
}

type Connector interface {
	Name() string
}

type SpanMapping interface {
	GetEntrySpanName(spanId pcommon.SpanID) string
}

type Sampler interface {
	Name() string
	GetSampleTime() int

	Sample(id pcommon.TraceID, trace *OtelTrace)
	BatchSample(id pcommon.TraceID, traces []*OtelTrace)
}

type OtelTrace struct {
	Resource *pcommon.Resource
	Spans    []*ptrace.Span
}

func NewOtelTrace(resource *pcommon.Resource, spans []*ptrace.Span) *OtelTrace {
	return &OtelTrace{
		Resource: resource,
		Spans:    spans,
	}
}

func BuildTrace(trace *OtelTrace) ptrace.Traces {
	result := ptrace.NewTraces()
	rs := result.ResourceSpans().AppendEmpty()
	trace.Resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range trace.Spans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}
	return result
}

func BuildTraces(traces []*OtelTrace) ptrace.Traces {
	result := ptrace.NewTraces()
	for _, trace := range traces {
		rs := result.ResourceSpans().AppendEmpty()
		trace.Resource.CopyTo(rs.Resource())
		ils := rs.ScopeSpans().AppendEmpty()
		for _, span := range trace.Spans {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)
		}
	}
	return result
}
