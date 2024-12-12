package tracecacheextension

import (
	"sync"

	"github.com/CloudDetail/apo-otel-collector/pkg/tracecache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type traceData struct {
	traceLock   sync.Mutex
	traces      []*tracecache.TraceData
	spanMapping *spanTraceMapping
}

func newTraceData() *traceData {
	return &traceData{
		traces:      make([]*tracecache.TraceData, 0),
		spanMapping: newSpanTraceMapping(),
	}
}

func (data *traceData) CacheTraceSpans(resource *pcommon.Resource, spans []*ptrace.Span, buildTrace bool) []*tracecache.TraceData {
	newSpans := make([]*ptrace.Span, 0)
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if data.spanMapping.addSpanMapping(span) {
			newSpans = append(newSpans, span)
		}
	}
	if len(newSpans) == 0 {
		return nil
	}

	if buildTrace {
		if data.traces == nil {
			return []*tracecache.TraceData{tracecache.NewTraceData(resource, newSpans)}
		} else {
			data.traceLock.Lock()
			data.traces = append(data.traces, tracecache.NewTraceData(resource, newSpans))
			data.traceLock.Unlock()
		}
	}
	return nil
}

func (data *traceData) GetAndCleanCacheTrace() []*tracecache.TraceData {
	data.traceLock.Lock()
	defer data.traceLock.Unlock()

	result := data.traces
	data.traces = nil
	return result
}

type spanTraceMapping struct {
	lock             sync.RWMutex
	spanIdMap        map[pcommon.SpanID]pcommon.SpanID // <spanId, pSpanId>
	entrySpanNameMap map[pcommon.SpanID]string         // <spanId, entryUrl>
}

func newSpanTraceMapping() *spanTraceMapping {
	return &spanTraceMapping{
		spanIdMap:        make(map[pcommon.SpanID]pcommon.SpanID),
		entrySpanNameMap: make(map[pcommon.SpanID]string),
	}
}

func (mapping *spanTraceMapping) addSpanMapping(span *ptrace.Span) bool {
	mapping.lock.Lock()
	defer mapping.lock.Unlock()

	if _, ok := mapping.spanIdMap[span.SpanID()]; ok {
		return false
	}

	mapping.spanIdMap[span.SpanID()] = span.ParentSpanID()
	if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
		mapping.entrySpanNameMap[span.SpanID()] = span.Name()
	}
	return true
}

func (mapping *spanTraceMapping) GetEntrySpanName(spanId pcommon.SpanID) string {
	mapping.lock.RLock()
	defer mapping.lock.RUnlock()

	if len(mapping.entrySpanNameMap) == 0 {
		return ""
	}
	return mapping.getEntrySpanName(spanId)
}

func (mapping *spanTraceMapping) getEntrySpanName(spanId pcommon.SpanID) string {
	if entrySpanName, found := mapping.entrySpanNameMap[spanId]; found {
		return entrySpanName
	}
	if parentSpanId, found := mapping.spanIdMap[spanId]; found {
		return mapping.GetEntrySpanName(parentSpanId)
	}
	return ""
}
