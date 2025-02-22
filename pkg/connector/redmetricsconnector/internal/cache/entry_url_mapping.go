package cache

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

type EntryUrlCache struct {
	lock           sync.Mutex
	entryUrlMap    map[pcommon.TraceID]*entryUrlMapping
	unMatchedSpans []*ResourceSpan

	idBatch            *WriteableBatch
	idBuckets          *Buckets
	expireUnMatcheTime int64
}

func NewEntryUrlCache(cacheUrlTime int, expireTime int64) (*EntryUrlCache, error) {
	if cacheUrlTime < 0 {
		return nil, fmt.Errorf(
			"invalid cache_entry_url_time: %v, the number must be positive",
			cacheUrlTime,
		)
	}
	if expireTime < 0 {
		return nil, fmt.Errorf(
			"invalid unmatch_url_expire_time: %v, the number must be positive",
			expireTime,
		)
	}
	return &EntryUrlCache{
		entryUrlMap:        make(map[pcommon.TraceID]*entryUrlMapping),
		unMatchedSpans:     make([]*ResourceSpan, 0),
		idBatch:            NewWriteableBatch(),
		idBuckets:          NewBuckets(cacheUrlTime),
		expireUnMatcheTime: expireTime,
	}, nil
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
				// Record TraceId
				cache.idBatch.AddToBatch(id)
			}
			mapping.AddMapping(spans)
			cachedTraces[id] = mapping
		}
	}

	result := make([]*ResourceSpan, 0)
	for index := 0; index < len(cache.unMatchedSpans); index++ {
		unMatchedSpan := cache.unMatchedSpans[index]
		if traceMapping, found := cachedTraces[unMatchedSpan.Span.TraceID()]; found {
			if entryUrl := traceMapping.GetEntrySpanName(unMatchedSpan.Span.ParentSpanID()); entryUrl != "" {
				result = append(result, unMatchedSpan.SetEntryUrl(entryUrl))
				cache.unMatchedSpans = append(cache.unMatchedSpans[:index], cache.unMatchedSpans[index+1:]...)
				index--
			}
		}
	}

	expireTime := time.Now().Unix() + cache.expireUnMatcheTime
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		pid, serviceName, containerId := GetResourceAttribute(rss)
		if pid == "" || serviceName == "" {
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

func (cache *EntryUrlCache) CleanExpireBuckets() {
	toCleanIds := cache.idBuckets.CopyAndGetBatch(cache.idBatch.PickBatch())
	if len(toCleanIds) > 0 {
		cache.lock.Lock()
		for _, toCleanId := range toCleanIds {
			delete(cache.entryUrlMap, toCleanId)
		}
		cache.lock.Unlock()
	}
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
		} else if span.Kind() != ptrace.SpanKindClient && span.Kind() != ptrace.SpanKindProducer {
			// ExitSpan is not necessary to store parent relation, as it will check with it's parentSpanId.
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
		return
	}
	serviceAttr, ok := resourceAttr.Get(conventions.AttributeServiceName)
	if !ok {
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
