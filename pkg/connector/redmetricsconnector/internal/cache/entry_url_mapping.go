package cache

import (
	"strconv"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

type EntryUrlCache struct {
	lock               sync.Mutex
	entryUrlMap        map[pcommon.TraceID]*entryUrlMapping
	unMatchedSpans     []*ResourceSpan
	expireUnMatcheTime int64
}

func NewEntryUrlCache(expireTime int64) *EntryUrlCache {
	return &EntryUrlCache{
		entryUrlMap:        make(map[pcommon.TraceID]*entryUrlMapping),
		unMatchedSpans:     make([]*ResourceSpan, 0),
		expireUnMatcheTime: expireTime,
	}
}

func (cache *EntryUrlCache) GetMatchedResourceSpans(traces ptrace.Traces) []*ResourceSpan {
	cache.lock.Lock()
	defer cache.lock.Unlock()

	cachedTraces := make(map[pcommon.TraceID]*entryUrlMapping)
	resourceSpans := traces.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		idToSpans := groupSpansByTraceId(rss)
		for id, spans := range idToSpans {
			mapping, ok := cache.entryUrlMap[id]
			if !ok {
				mapping = newEntryUrlMapping()
				cache.entryUrlMap[id] = mapping
			}
			mapping.AddMapping(spans)
			cachedTraces[id] = mapping
		}
	}

	result := make([]*ResourceSpan, 0)
	// 遍历未匹配的ExitSpan列表
	for index := 0; index < len(cache.unMatchedSpans); index++ {
		unMatchedSpan := cache.unMatchedSpans[index]
		if traceMapping, found := cachedTraces[unMatchedSpan.Span.TraceID()]; found {
			if entryUrl := traceMapping.GetEntrySpanName(unMatchedSpan.Span.ParentSpanID()); entryUrl != "" {
				result = append(result, unMatchedSpan.SetEntryUrl(entryUrl))

				// 删除已匹配的
				cache.unMatchedSpans = append(cache.unMatchedSpans[:index], cache.unMatchedSpans[index+1:]...)
				index--
			}
		}
	}

	expireTime := time.Now().Unix() + cache.expireUnMatcheTime
	// 基于Resource遍历
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		pid, serviceName, containerId := GetResourceAttribute(rss)
		if pid == "" || serviceName == "" {
			// 必须存在PID 和 服务名
			continue
		}
		ilsSlice := rss.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if span.Kind() == ptrace.SpanKindClient || span.Kind() == ptrace.SpanKindProducer {
					if mapping, ok := cache.entryUrlMap[span.TraceID()]; ok {
						parentSpanId := span.ParentSpanID()
						if parentSpanId.IsEmpty() {
							// ExitSpan as root span data.
							result = append(result, NewResourceSpan(expireTime, pid, containerId, serviceName, span.Name(), &span))
						} else {
							entryUrl := mapping.GetEntrySpanName(parentSpanId)
							if entryUrl == "" {
								cache.unMatchedSpans = append(cache.unMatchedSpans, NewResourceSpan(expireTime, pid, containerId, serviceName, "", &span))
							} else {
								result = append(result, NewResourceSpan(expireTime, pid, containerId, serviceName, entryUrl, &span))
							}
						}
					}
				} else if span.Kind() == ptrace.SpanKindConsumer || span.Kind() == ptrace.SpanKindServer {
					result = append(result, NewResourceSpan(expireTime, pid, containerId, serviceName, span.Name(), &span))
				}
			}
		}
	}
	return result
}

func (cache *EntryUrlCache) CleanUnMatchedSpans() []*ResourceSpan {
	if len(cache.unMatchedSpans) == 0 {
		return nil
	}

	now := time.Now().Unix()
	expireIndex := 0
	size := len(cache.unMatchedSpans)
	for index := 0; index < size; index++ {
		if cache.unMatchedSpans[index].ExpireTime < now {
			expireIndex++
		} else {
			break
		}
	}

	if expireIndex == 0 {
		return nil
	}
	unknownSpans := cache.unMatchedSpans[0:expireIndex]

	cache.lock.Lock()
	cache.unMatchedSpans = cache.unMatchedSpans[expireIndex:]
	cache.lock.Unlock()

	return unknownSpans
}

type entryUrlMapping struct {
	entrySpanNames map[pcommon.SpanID]string
	pspanIds       map[pcommon.SpanID]pcommon.SpanID
}

func newEntryUrlMapping() *entryUrlMapping {
	return &entryUrlMapping{
		entrySpanNames: make(map[pcommon.SpanID]string),
		pspanIds:       make(map[pcommon.SpanID]pcommon.SpanID),
	}
}

func (mapping *entryUrlMapping) AddMapping(spans []*ptrace.Span) {
	for _, span := range spans {
		if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
			mapping.entrySpanNames[span.SpanID()] = span.Name()
		}
		// ExitSpan is not necessary to store parent relation, as it will check with it's parentSpanId.
		if span.Kind() != ptrace.SpanKindClient && span.Kind() == ptrace.SpanKindProducer {
			mapping.pspanIds[span.SpanID()] = span.ParentSpanID()
		}
	}
}

func (mapping *entryUrlMapping) GetEntrySpanName(spanId pcommon.SpanID) string {
	if url, found := mapping.entrySpanNames[spanId]; found {
		return url
	}
	if parent, found := mapping.pspanIds[spanId]; found {
		return mapping.GetEntrySpanName(parent)
	}
	return ""
}

type ResourceSpan struct {
	ExpireTime  int64
	Pid         string
	ContainerId string
	ServiceName string
	EntryUrl    string
	Span        *ptrace.Span
}

func NewResourceSpan(time int64, pid string, containerId string, servcieName string, entryUrl string, span *ptrace.Span) *ResourceSpan {
	return &ResourceSpan{
		ExpireTime:  time,
		Pid:         pid,
		ContainerId: containerId,
		ServiceName: servcieName,
		EntryUrl:    entryUrl,
		Span:        span,
	}
}

func (span *ResourceSpan) SetEntryUrl(entryUrl string) *ResourceSpan {
	span.EntryUrl = entryUrl
	return span
}

func groupSpansByTraceId(resourceSpans ptrace.ResourceSpans) map[pcommon.TraceID][]*ptrace.Span {
	idToSpans := make(map[pcommon.TraceID][]*ptrace.Span)
	ilss := resourceSpans.ScopeSpans()
	resourceSpansLen := ilss.Len()
	for i := 0; i < resourceSpansLen; i++ {
		spans := ilss.At(i).Spans()
		spansLen := spans.Len()
		for j := 0; j < spansLen; j++ {
			span := spans.At(j)
			key := span.TraceID()
			idToSpans[key] = append(idToSpans[key], &span)
		}
	}
	return idToSpans
}

func GetResourceAttribute(rss ptrace.ResourceSpans) (pid string, serviceName string, containerId string) {
	resource := rss.Resource()
	resourceAttr := resource.Attributes()
	pidIntValue := fillproc.GetPid(resourceAttr)
	if pidIntValue <= 0 {
		// 必须存在PID
		return
	}
	serviceAttr, ok := resourceAttr.Get(conventions.AttributeServiceName)
	if !ok {
		// 必须存在服务名
		return
	}
	pid = strconv.FormatInt(pidIntValue, 10)
	serviceName = serviceAttr.Str()
	containerId = fillproc.GetContainerId(resourceAttr)
	if len(containerId) > 12 {
		containerId = containerId[:12]
	}
	return
}
