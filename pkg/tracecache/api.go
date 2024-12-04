package tracecache

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceCache interface {
	CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]*TraceMapping
	GetTrace(id pcommon.TraceID) *TraceMapping
}

type TraceMapping struct {
	Traces  *ptrace.Traces
	SpanMap map[pcommon.SpanID]*ptrace.Span // <spanId, Span>
}

func NewTraceMapping() *TraceMapping {
	traces := ptrace.NewTraces()
	return &TraceMapping{
		Traces:  &traces,
		SpanMap: make(map[pcommon.SpanID]*ptrace.Span),
	}
}

func (mapping *TraceMapping) CacheTrace(resource *pcommon.Resource, spans []*ptrace.Span) bool {
	newSpan := false
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if _, found := mapping.SpanMap[span.SpanID()]; !found {
			newSpan = true
			break
		}
	}
	if !newSpan {
		return false
	}

	rs := mapping.Traces.ResourceSpans().AppendEmpty()
	resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range spans {
		if _, found := mapping.SpanMap[span.SpanID()]; !found {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)

			mapping.SpanMap[sp.SpanID()] = &sp
			newSpan = true
		}
	}

	return true
}

func (mapping *TraceMapping) GetMatchedEntrySpanName(span *ptrace.Span) string {
	if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
		// MQ 消费者的请求URL 就是当前自身
		return span.Name()
	}

	if entrySpan := getEntrySpan(mapping.SpanMap, span.ParentSpanID()); entrySpan != nil {
		return entrySpan.Name()
	}
	return ""
}

func getEntrySpan(spanMap map[pcommon.SpanID]*ptrace.Span, spanId pcommon.SpanID) *ptrace.Span {
	if parentSpan, found := spanMap[spanId]; found {
		if parentSpan.Kind() == ptrace.SpanKindServer || parentSpan.Kind() == ptrace.SpanKindConsumer {
			return parentSpan
		}
		return getEntrySpan(spanMap, parentSpan.ParentSpanID())
	}
	return nil
}
