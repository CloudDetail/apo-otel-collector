package tracecacheextension

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	logger          *zap.Logger
	idToTrace       sync.Map
	idBatch         *bucket.WriteableBatch[pcommon.TraceID] // 缓存当前秒的TraceId列表
	idbucket        *bucket.Bucket[pcommon.TraceID]         // 记录每秒TraceId分桶数据
	traceBatch      *bucket.WriteableBatch[*delayTrace]     // 缓存当前秒待发送的Trace列表
	traceBucket     *bucket.Bucket[*delayTrace]             // 缓存当前秒待发送的Trace分桶数据
	cleanTicker     timeutils.TTicker                       // 定时清理分桶数据
	tickerFrequency time.Duration
	sampler         tracecache.Sampler
	delayTracePool  DelayTracePool
}

func newTraceCacheExtension(settings extension.Settings, cfg *Config) (*TraceCacheExtension, error) {
	numCleanBatches := int(cfg.CleanTime.Seconds())

	idbucket, err := bucket.NewBucket[pcommon.TraceID](numCleanBatches, "clean_time")
	if err != nil {
		return nil, err
	}

	tce := &TraceCacheExtension{
		logger:          settings.Logger,
		idBatch:         bucket.NewWriteableBatch[pcommon.TraceID](),
		idbucket:        idbucket,
		tickerFrequency: time.Second,
	}

	tce.cleanTicker = &timeutils.PolicyTicker{OnTickFunc: tce.cleanOnTick}
	return tce, nil
}

func (tce *TraceCacheExtension) Start(context.Context, component.Host) error {
	tce.logger.Info("Start traceCache Extension")

	tce.cleanTicker.Start(tce.tickerFrequency)
	return nil
}

func (tce *TraceCacheExtension) Shutdown(context.Context) error {
	tce.cleanTicker.Stop()
	return nil
}

func (tce *TraceCacheExtension) CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]tracecache.SpanMapping {
	result := make(map[pcommon.TraceID]tracecache.SpanMapping)

	resourceSpans := traces.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		resource := rss.Resource()
		idToSpans := groupSpansByTraceId(rss)
		for id, spans := range idToSpans {
			// 先缓存Trace数据
			d, loaded := tce.idToTrace.Load(id)
			if !loaded {
				d, loaded = tce.idToTrace.LoadOrStore(id, newTraceData())
			}
			if !loaded {
				// 新的Bucket记录TraceId
				tce.idBatch.AddToBatch(id)
			}
			traceData := d.(*traceData)
			toSendTraces := traceData.CacheTraceSpans(&resource, spans)
			if tce.sampler != nil && toSendTraces != nil {
				// 针对超时场景 --- 超过sampleTime，后续到达的Trace数据，不再缓存SampleTime，而是DelayTime.
				tce.traceBatch.AddToBatch(tce.delayTracePool.Get().With(id, toSendTraces))
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
		if sampler.GetDelayTime() >= sampler.GetSampleTime() {
			return fmt.Errorf("invalid number of delay_time, it must less than sample_time: %d", sampler.GetSampleTime())

		}
		traceBucket, err := bucket.NewBucket[*delayTrace](sampler.GetDelayTime(), "delay_time")
		if err != nil {
			return err
		}
		tce.delayTracePool = NewDelayTracePool()
		tce.traceBatch = bucket.NewWriteableBatch[*delayTrace]()
		tce.traceBucket = traceBucket
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
		tce.cleanExpireTraceIds(expireIdBatch)
	} else {
		sampleIdBatch, expireIdBatch := tce.idbucket.CopyAndGetBatches(currentIdBatch, tce.sampler.GetSampleTime())
		for _, id := range sampleIdBatch {
			if d, ok := tce.idToTrace.Load(id); ok {
				traceData := d.(*traceData)
				// 延迟推送
				tce.traceBatch.AddToBatch(tce.delayTracePool.Get().With(id, traceData.traces))
				// 释放Trace 内存数据
				traceData.CleanCacheTrace()
			}
		}
		tce.cleanExpireTraceIds(expireIdBatch)

		// 获取 当前推送Trace
		currentTraceBatch := tce.traceBatch.GetAndReset()
		expireTraceBatch := tce.traceBucket.CopyAndGetBatch(currentTraceBatch)
		for _, delayTrace := range expireTraceBatch {
			tce.sampler.Sample(delayTrace.id, delayTrace.traces)
			tce.delayTracePool.Free(delayTrace)
		}
	}
}

func (tce *TraceCacheExtension) cleanExpireTraceIds(expireIdBatch bucket.Batch[pcommon.TraceID]) {
	cleanCount := 0
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
		tce.logger.Info("[Clean CachedTrace]", zap.Int("count", cleanCount))
	}
}
