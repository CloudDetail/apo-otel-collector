package tracecacheextension

import (
	"sync"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type traceData struct {
	lock        sync.Mutex
	traces      *ptrace.Traces
	spanMapping *spanTraceMapping
}

func newTraceData() *traceData {
	traces := ptrace.NewTraces()
	return &traceData{
		traces:      &traces,
		spanMapping: newSpanTraceMapping(),
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
	if traces == nil {
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

	if data.traces == nil {
		return traces
	}
	return nil
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

type DelayTracePool interface {
	Get() *delayTrace
	Free(delayTrace *delayTrace)
}

type delayTracePool struct {
	pool *sync.Pool
}

func createDelayTrace() interface{} {
	return &delayTrace{}
}

func NewDelayTracePool() DelayTracePool {
	return &delayTracePool{pool: &sync.Pool{New: createDelayTrace}}
}

func (p *delayTracePool) Get() *delayTrace {
	return p.pool.Get().(*delayTrace)
}

func (p *delayTracePool) Free(trace *delayTrace) {
	trace.id = [16]byte{}
	trace.traces = nil
	p.pool.Put(trace)
}

type delayTrace struct {
	id     pcommon.TraceID
	traces *ptrace.Traces
}

func (trace *delayTrace) With(id pcommon.TraceID, traces *ptrace.Traces) *delayTrace {
	trace.id = id
	trace.traces = traces
	return trace
}
