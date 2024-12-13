package tracecacheextension

import (
	"sync"

	"github.com/CloudDetail/apo-otel-collector/pkg/tracecache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type traceData struct {
	lock        sync.RWMutex
	traces      []*tracecache.OtelTrace
	spanMapping map[pcommon.SpanID]*ptrace.Span
}

func newTraceData() *traceData {
	return &traceData{
		traces:      make([]*tracecache.OtelTrace, 0),
		spanMapping: make(map[pcommon.SpanID]*ptrace.Span),
	}
}

func (data *traceData) CacheSpanMapping(spans []*ptrace.Span) []*ptrace.Span {
	data.lock.Lock()
	defer data.lock.Unlock()

	newSpans := make([]*ptrace.Span, 0)
	for _, span := range spans {
		// 存在多个组件同时使用该Extension，避免数据重复插入
		if _, ok := data.spanMapping[span.SpanID()]; !ok {
			data.spanMapping[span.SpanID()] = span
			newSpans = append(newSpans, span)
		}
	}
	return newSpans
}

func (data *traceData) CacheTrace(otelTrace *tracecache.OtelTrace) (*tracecache.OtelTrace, bool) {
	if data.traces == nil {
		return otelTrace, false
	}

	data.lock.Lock()
	defer data.lock.Unlock()
	data.traces = append(data.traces, otelTrace)
	return nil, true
}

func (data *traceData) GetAndCleanCacheTrace() []*tracecache.OtelTrace {
	data.lock.Lock()
	defer data.lock.Unlock()

	result := data.traces
	data.traces = nil
	return result
}

func (data *traceData) GetEntrySpanName(spanId pcommon.SpanID) string {
	if len(data.spanMapping) == 0 {
		return ""
	}

	data.lock.RLock()
	defer data.lock.RUnlock()

	return data.getEntrySpanName(spanId)
}

func (data *traceData) getEntrySpanName(spanId pcommon.SpanID) string {
	span, found := data.spanMapping[spanId]
	if !found {
		return ""
	}
	if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
		return span.Name()
	}
	return data.getEntrySpanName(span.ParentSpanID())
}
