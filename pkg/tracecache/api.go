package tracecache

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceCache interface {
	CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]SpanMapping
	GetTrace(id pcommon.TraceID) SpanMapping
}

type SpanMapping interface {
	GetEntrySpanName(spanId pcommon.SpanID) string
}
