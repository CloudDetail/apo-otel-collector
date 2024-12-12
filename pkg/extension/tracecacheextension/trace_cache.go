package tracecacheextension

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/common/timeutils"
	"github.com/CloudDetail/apo-otel-collector/pkg/extension/tracecacheextension/internal/bucket"
	"github.com/CloudDetail/apo-otel-collector/pkg/tracecache"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type TraceCacheExtension struct {
	logger           *zap.Logger
	enable           bool
	idToTrace        *RWMap[pcommon.TraceID, *traceData]
	idBatch          *bucket.WriteableBatch // 缓存当前秒的TraceId列表
	idbucket         *bucket.Bucket         // 记录每秒TraceId分桶数据
	cleanTicker      timeutils.TTicker      // 定时清理分桶数据
	tickerFrequency  time.Duration
	activeTraceCount *atomic.Int64
	sampler          tracecache.Sampler
}

func newTraceCacheExtension(settings extension.Settings, cfg *Config) (*TraceCacheExtension, error) {
	numCleanBatches := int(cfg.CleanTime.Seconds())

	idbucket, err := bucket.NewBucket(numCleanBatches, "clean_time")
	if err != nil {
		return nil, err
	}

	tce := &TraceCacheExtension{
		logger:           settings.Logger,
		enable:           cfg.Enable,
		idToTrace:        NewRWMap[pcommon.TraceID, *traceData](),
		idBatch:          bucket.NewWriteableBatch(),
		idbucket:         idbucket,
		tickerFrequency:  time.Second,
		activeTraceCount: &atomic.Int64{},
	}
	return tce, nil
}

func (tce *TraceCacheExtension) IsEnable() bool {
	return tce.enable
}

func (tce *TraceCacheExtension) Start(context.Context, component.Host) error {
	tce.logger.Info("Start traceCache Extension", zap.Bool("enable", tce.enable))
	if tce.enable {
		tce.cleanTicker = &timeutils.PolicyTicker{OnTickFunc: tce.cleanOnTick}
		tce.cleanTicker.Start(tce.tickerFrequency)
	}
	return nil
}

func (tce *TraceCacheExtension) Shutdown(context.Context) error {
	if tce.enable {
		tce.cleanTicker.Stop()
	}
	return nil
}

func (tce *TraceCacheExtension) CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]tracecache.SpanMapping {
	result := make(map[pcommon.TraceID]tracecache.SpanMapping)
	if !tce.enable {
		return result
	}
	hasSampler := tce.sampler != nil
	resourceSpans := traces.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		resource := rss.Resource()
		idToSpans := groupSpansByTraceId(rss)
		for id, spans := range idToSpans {
			// 先缓存Trace数据
			traceData, loaded := tce.idToTrace.Load(id)
			if !loaded {
				traceData, loaded = tce.idToTrace.LoadOrStore(id, newTraceData())
			}
			if !loaded {
				tce.activeTraceCount.Add(1)
				// 新的Bucket记录TraceId
				tce.idBatch.AddToBatch(id)
			}
			toSendTraces := traceData.CacheTraceSpans(&resource, spans, hasSampler)
			if hasSampler && toSendTraces != nil {
				// 针对超时场景 --- 超过sampleTime，后续到达的Trace数据，不再缓存SampleTime.
				tce.sampler.Sample(id, toSendTraces)
			}
			result[id] = traceData.spanMapping
		}
	}
	return result
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

func (tce *TraceCacheExtension) SetSampler(sampler tracecache.Sampler) error {
	if tce.sampler == nil {
		if sampler.GetSampleTime() <= 0 {
			return errors.New("invalid number of sample_time, it must more than zero")
		}
		if sampler.GetSampleTime() >= tce.idbucket.GetCleanPeriod() {
			return fmt.Errorf("invalid number of sample_time, it must less than clean_period: %d", tce.idbucket.GetCleanPeriod())
		}
		tce.sampler = sampler
	} else {
		tce.logger.Warn("more than one sampler for traceCache",
			zap.String("name[1]", tce.sampler.Name()),
			zap.String("name[2]", sampler.Name()))
	}
	return nil
}

func (tce *TraceCacheExtension) cleanOnTick() {
	currentIdBatch := tce.idBatch.GetAndReset()

	if tce.sampler == nil {
		expireIdBatch := tce.idbucket.CopyAndGetBatch(currentIdBatch)
		tce.cleanExpireTraces(expireIdBatch)
	} else {
		sampleIdBatch, expireIdBatch := tce.idbucket.CopyAndGetBatches(currentIdBatch, tce.sampler.GetSampleTime())
		for _, id := range sampleIdBatch {
			if traceData, ok := tce.idToTrace.Load(id); ok {
				tce.sampler.Sample(id, traceData.traces)
				// 释放Trace 内存数据
				traceData.CleanCacheTrace()
			}
		}
		tce.cleanExpireTraces(expireIdBatch)
	}
}

func (tce *TraceCacheExtension) cleanExpireTraces(expireIdBatch bucket.Batch) {
	var cleanCount int64 = 0
	for _, id := range expireIdBatch {
		_, ok := tce.idToTrace.Load(id)
		if !ok {
			continue
		}

		// 清理数据
		tce.idToTrace.Delete(id)
		cleanCount += 1
	}
	if cleanCount > 0 {
		currentCount := tce.activeTraceCount.Add(-cleanCount)
		tce.logger.Info("[Clean CachedTrace]", zap.Int64("deleteNum", cleanCount), zap.Int64("activeNum", currentCount))
	}
}

type RWMap[K comparable, V any] struct {
	sync.RWMutex
	m map[K]V
}

func NewRWMap[K comparable, V any]() *RWMap[K, V] {
	return &RWMap[K, V]{
		m: make(map[K]V),
	}
}

func (m *RWMap[K, V]) Load(key K) (V, bool) {
	m.RLock()
	defer m.RUnlock()

	v, existed := m.m[key]
	return v, existed
}

func (m *RWMap[K, V]) LoadOrStore(key K, value V) (V, bool) {
	m.Lock()
	defer m.Unlock()

	if v, existed := m.m[key]; existed {
		return v, existed
	}

	m.m[key] = value
	return value, false
}

func (m *RWMap[K, V]) Delete(key K) {
	m.Lock()
	delete(m.m, key)
	m.Unlock()
}
