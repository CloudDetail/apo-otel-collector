package tracecacheextension

import (
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type traceData struct {
	lock            sync.Mutex
	sendImmediately *atomic.Bool
	traces          *ptrace.Traces
	spanMapping     *spanTraceMapping
}

func newTraceData() *traceData {
	traces := ptrace.NewTraces()
	return &traceData{
		sendImmediately: &atomic.Bool{},
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
	if data.sendImmediately.Load() {
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

	if data.sendImmediately.Load() {
		return traces
	}
	return nil
}

func (data *traceData) CloseWriteTrace() {
	// 考虑2种场景 - 不更新traces
	// 1 - 数据刚更新，就触发采样，这类数据要快速延迟后通知采样
	// 2 - 数据清理后又来了新的数据，这类数据要快速延迟通知采样

	data.sendImmediately.Store(true)
}

func (data *traceData) CleanCacheTrace() {
	data.lock.Lock()
	data.traces = nil
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

type delayTrace struct {
	id     pcommon.TraceID
	traces *ptrace.Traces
}

func newDelayTrace(id pcommon.TraceID, traces *ptrace.Traces) *delayTrace {
	return &delayTrace{
		id:     id,
		traces: traces,
	}
}
