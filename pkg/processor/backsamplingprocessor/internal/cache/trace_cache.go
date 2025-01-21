package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type OtelTraceSampler struct {
	logger *zap.Logger

	sampledIds      sync.Map // <traceId, expireTime>
	traceWriteCache *WriteableCache[*OtelTrace]
	traceReadCache  *ReadableCache[*OtelTrace]

	traceIdExpireTime int32
}

func NewOtelTraceSampler(logger *zap.Logger, traceIdExpireTime time.Duration, delayTime int) *OtelTraceSampler {
	sampler := &OtelTraceSampler{
		logger:            logger,
		traceWriteCache:   NewWriteableCache[*OtelTrace](),
		traceReadCache:    NewReadableCache[*OtelTrace]("OtelTrace", delayTime),
		traceIdExpireTime: int32(traceIdExpireTime.Seconds()),
	}
	return sampler
}

func (sampler *OtelTraceSampler) SetSampledTraceId(traceId string) {
	sampler.sampledIds.Store(traceId, &atomic.Int32{})
}

func (sampler *OtelTraceSampler) Cache(id pcommon.TraceID, trace *OtelTrace) {
	sampler.traceWriteCache.Write(id.String(), trace)
}

func (sampler *OtelTraceSampler) PickSampledTraces(logEnable bool) []*OtelTrace {
	traces := make([]*OtelTrace, 0)

	toWriteTraces := sampler.traceWriteCache.GetAndReset()
	sampler.traceReadCache.Copy(toWriteTraces)

	sampler.sampledIds.Range(func(k, v interface{}) bool {
		if sampledTraces := sampler.traceReadCache.PickMatchData(k.(string)); sampledTraces != nil {
			traces = append(traces, sampledTraces...)
		}
		holdTime := v.(*atomic.Int32)
		if holdTime.Add(1) >= sampler.traceIdExpireTime {
			sampler.sampledIds.Delete(k)
		}
		return true
	})
	sampler.traceReadCache.CleanExpireDatas(logEnable)

	return traces
}

type OtelTrace struct {
	Resource *pcommon.Resource
	Spans    []*ptrace.Span
}

func NewOtelTrace(resource *pcommon.Resource, spans []*ptrace.Span) *OtelTrace {
	return &OtelTrace{
		Resource: resource,
		Spans:    spans,
	}
}

func (trace *OtelTrace) ConvertToTraces() ptrace.Traces {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	trace.Resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range trace.Spans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}
	return traces
}

func BuildTraces(resource *pcommon.Resource, spans []*ptrace.Span) ptrace.Traces {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	resource.CopyTo(rs.Resource())
	ils := rs.ScopeSpans().AppendEmpty()
	for _, span := range spans {
		sp := ils.Spans().AppendEmpty()
		span.CopyTo(sp)
	}
	return traces
}
