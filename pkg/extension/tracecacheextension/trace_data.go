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

func (data *traceData) CacheTraceSpans(resource *pcommon.Resource, spans []*ptrace.Span, buildTrace bool) *ptrace.Traces {
	newSpans := make([]*ptrace.Span, 0)
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if _, found := data.spanMapping.spanIdMap.Load(span.SpanID()); !found {
			newSpans = append(newSpans, span)
		}
	}
	if len(newSpans) == 0 {
		return nil
	}

	for _, span := range newSpans {
		// 记录Mapping映射关系
		if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
			data.spanMapping.addEntrySpan(span.SpanID(), span.Name())
		}
		data.spanMapping.addParentSpanId(span.SpanID(), span.ParentSpanID())
	}

	if buildTrace {
		if data.traces == nil {
			traces := ptrace.NewTraces()
			rs := traces.ResourceSpans().AppendEmpty()
			resource.CopyTo(rs.Resource())
			ils := rs.ScopeSpans().AppendEmpty()
			for _, span := range newSpans {
				sp := ils.Spans().AppendEmpty()
				span.CopyTo(sp)
			}
			return &traces
		} else {
			data.lock.Lock()
			rs := data.traces.ResourceSpans().AppendEmpty()
			resource.CopyTo(rs.Resource())
			ils := rs.ScopeSpans().AppendEmpty()
			for _, span := range newSpans {
				sp := ils.Spans().AppendEmpty()
				span.CopyTo(sp)
			}
			data.lock.Unlock()
		}
	}
	return nil
}

func (data *traceData) CleanCacheTrace() {
	data.lock.Lock()
	data.traces = nil
	data.lock.Unlock()
}

type spanTraceMapping struct {
	spanIdMap        sync.Map // <spanId, pSpanId>
	entrySpanNameMap sync.Map // <spanId, entryUrl>
}

func newSpanTraceMapping() *spanTraceMapping {
	return &spanTraceMapping{}
}

func (mapping *spanTraceMapping) addParentSpanId(spanId pcommon.SpanID, parentSpanId pcommon.SpanID) {
	mapping.spanIdMap.Store(spanId, parentSpanId)
}

func (mapping *spanTraceMapping) addEntrySpan(spanId pcommon.SpanID, spanName string) {
	mapping.entrySpanNameMap.Store(spanId, spanName)
}

func (mapping *spanTraceMapping) GetEntrySpanName(spanId pcommon.SpanID) string {
	if url, found := mapping.entrySpanNameMap.Load(spanId); found {
		return url.(string)
	}
	if parent, found := mapping.spanIdMap.Load(spanId); found {
		return mapping.GetEntrySpanName(parent.(pcommon.SpanID))
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
