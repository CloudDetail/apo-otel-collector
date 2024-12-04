package tracecacheextension

import (
	"sync"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceData struct {
	lock        sync.Mutex
	traces      *ptrace.Traces
	spanMapping *spanTraceMapping
}

func NewTraceData() *TraceData {
	traces := ptrace.NewTraces()
	return &TraceData{
		traces:      &traces,
		spanMapping: newSpanTraceMapping(),
	}
}

func (traceData *TraceData) CacheTrace(resource *pcommon.Resource, spans []*ptrace.Span) bool {
	newSpan := false
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if _, found := traceData.spanMapping.spanIdMap[span.SpanID()]; !found {
			newSpan = true
			break
		}
	}
	if !newSpan {
		return false
	}

	traceData.lock.Lock()
	rs := traceData.traces.ResourceSpans().AppendEmpty()
	resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range spans {
		if _, found := traceData.spanMapping.spanIdMap[span.SpanID()]; !found {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)

			// 记录Mapping映射关系
			if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
				traceData.spanMapping.entrySpanNameMap[span.SpanID()] = span.Name()
			}
			traceData.spanMapping.spanIdMap[span.SpanID()] = span.ParentSpanID()
		}
	}
	traceData.lock.Unlock()

	return true
}

type spanTraceMapping struct {
	spanIdMap        map[pcommon.SpanID]pcommon.SpanID // <spanId, pSpanId>
	entrySpanNameMap map[pcommon.SpanID]string         // <spanId, entryUrl>
}

func newSpanTraceMapping() *spanTraceMapping {
	return &spanTraceMapping{
		spanIdMap:        make(map[pcommon.SpanID]pcommon.SpanID),
		entrySpanNameMap: make(map[pcommon.SpanID]string),
	}
}

func (mapping *spanTraceMapping) GetEntrySpanName(spanId pcommon.SpanID) string {
	if url, found := mapping.entrySpanNameMap[spanId]; found {
		return url
	}
	if parent, found := mapping.spanIdMap[spanId]; found {
		return mapping.GetEntrySpanName(parent)
	}
	return ""
}
