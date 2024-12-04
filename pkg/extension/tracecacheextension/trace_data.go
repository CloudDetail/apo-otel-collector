package tracecacheextension

import (
	"sync"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type traceData struct {
	lock            sync.Mutex
	sendImmediately bool
	traces          *ptrace.Traces
	spanMapping     *spanTraceMapping
}

func newTraceData() *traceData {
	traces := ptrace.NewTraces()
	return &traceData{
		sendImmediately: false,
		traces:          &traces,
		spanMapping:     newSpanTraceMapping(),
	}
}

func (data *traceData) CacheTraceSpans(resource *pcommon.Resource, spans []*ptrace.Span) *ptrace.Traces {
	newSpan := false
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if _, found := data.spanMapping.spanIdMap[span.SpanID()]; !found {
			newSpan = true
			break
		}
	}
	if !newSpan {
		return nil
	}

	data.lock.Lock()
	defer data.lock.Unlock()

	traces := data.traces
	if data.sendImmediately {
		ptraces := ptrace.NewTraces()
		traces = &ptraces
	}
	rs := traces.ResourceSpans().AppendEmpty()
	resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range spans {
		if _, found := data.spanMapping.spanIdMap[span.SpanID()]; !found {
			sp := ils.Spans().AppendEmpty()
			span.CopyTo(sp)

			// 记录Mapping映射关系
			if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
				data.spanMapping.entrySpanNameMap[span.SpanID()] = span.Name()
			}
			data.spanMapping.spanIdMap[span.SpanID()] = span.ParentSpanID()
		}
	}

	if data.sendImmediately {
		return traces
	}
	return nil
}

func (data *traceData) CleanCacheTrace() {
	data.lock.Lock()
	data.traces = nil
	data.sendImmediately = true
	data.lock.Unlock()
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
