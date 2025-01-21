package traceutil

import "go.opentelemetry.io/collector/pdata/ptrace"

const (
	AttributeSkywalkingComponmentId string = "sw8.componment_id"
	ComponentUndertow               int64  = 84
)

func IsEntrySpan(span *ptrace.Span) bool {
	if span.Kind() != ptrace.SpanKindServer && span.Kind() != ptrace.SpanKindConsumer {
		return false
	}
	if swComponmentId, exist := span.Attributes().Get(AttributeSkywalkingComponmentId); exist && swComponmentId.Int() == ComponentUndertow {
		return false
	}
	return true
}
