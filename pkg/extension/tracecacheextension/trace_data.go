package tracecacheextension

import (
	"sync"

	"github.com/CloudDetail/apo-otel-collector/pkg/tracecache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceData struct {
	lock         sync.Mutex
	traceMapping *tracecache.TraceMapping
}

func NewTraceData() *TraceData {
	return &TraceData{
		traceMapping: tracecache.NewTraceMapping(),
	}
}

func (traceData *TraceData) CacheTrace(resource *pcommon.Resource, spans []*ptrace.Span) bool {
	traceData.lock.Lock()
	defer traceData.lock.Unlock()

	return traceData.traceMapping.CacheTrace(resource, spans)
}
