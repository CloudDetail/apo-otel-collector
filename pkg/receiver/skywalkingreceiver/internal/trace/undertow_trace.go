package trace

import (
	"fmt"
	"sync"

	"go.opentelemetry.io/collector/pdata/ptrace"
	agent "skywalking.apache.org/repo/goapi/collect/language/agent/v3"
)

const (
	ComponentUndertow   int32 = 84
	ComponentSpringMVC  int32 = 14
	OperateNameUndertow       = "UndertowDispatch"
)

type UndertowCache struct {
	ioTraceMap  sync.Map // <traceId-parentSegmentId, Trace>
	routeUrlMap sync.Map // <traceId-parentSegmentId, string>
}

func (c *UndertowCache) CheckUndertow(segment *agent.SegmentObject, ptd *ptrace.Traces) *ptrace.Traces {
	isUndertow := false
	routeUrl := ""
	parentSegmentId := ""
	for _, span := range segment.Spans {
		if span.ComponentId == ComponentUndertow && span.SpanId == 0 {
			isUndertow = true
			if span.OperationName == OperateNameUndertow {
				parentSegmentId = span.Refs[0].ParentTraceSegmentId
			}
		}
		if span.SpanType == agent.SpanType_Entry && span.ComponentId == ComponentSpringMVC {
			routeUrl = span.OperationName
		}
	}

	if !isUndertow {
		return ptd
	}

	if routeUrl == "" {
		// XNIO-1 I/O
		key := fmt.Sprintf("%s-%s", segment.TraceId, segment.TraceSegmentId)

		if cachedRouteUrl, exist := c.routeUrlMap.LoadAndDelete(key); exist {
			ptd.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).SetName(cachedRouteUrl.(string))
			return ptd
		} else {
			c.ioTraceMap.Store(key, ptd)
			return nil
		}
	} else {
		// XNIO-1 task
		key := fmt.Sprintf("%s-%s", segment.TraceId, parentSegmentId)
		if cachedTraceInterface, exist := c.ioTraceMap.LoadAndDelete(key); exist {
			catchedTrace := cachedTraceInterface.(*ptrace.Traces)
			spans := catchedTrace.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
			spans.At(0).SetName(routeUrl)
			il := ptd.ResourceSpans().At(0).ScopeSpans().AppendEmpty().Spans()
			spans.MoveAndAppendTo(il)
		} else {
			c.routeUrlMap.Store(key, routeUrl)
		}
		return ptd
	}
}
