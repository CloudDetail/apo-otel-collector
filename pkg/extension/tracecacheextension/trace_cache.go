package tracecacheextension

import (
	"context"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/common/timeutils"
	"github.com/CloudDetail/apo-otel-collector/pkg/extension/tracecacheextension/internal/idbucket"
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
	writeBatch      *idbucket.WriteableBatch // 缓存当前秒的TraceId列表
	bucket          *idbucket.Bucket         // 记录每秒TraceId分桶数据
	cleanTicker     timeutils.TTicker        // 定时清理分桶数据
	tickerFrequency time.Duration
	sampler         tracecache.Sampler
}

func newTraceCacheExtension(settings extension.Settings, cfg *Config) (*TraceCacheExtension, error) {
	numCleanBatches := int(cfg.CleanTime.Seconds())
	numSampleBatches := int(cfg.SampleTime.Seconds())

	bucket, err := idbucket.NewBucket(numSampleBatches, numCleanBatches)
	if err != nil {
		return nil, err
	}

	tce := &TraceCacheExtension{
		logger:          settings.Logger,
		writeBatch:      idbucket.NewWriteableBatch(),
		bucket:          bucket,
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
				tce.writeBatch.AddToBatch(id)
			}
			traceData := d.(*traceData)
			if toSendTraces := traceData.CacheTraceSpans(&resource, spans); toSendTraces != nil && tce.sampler != nil {
				// 针对超时场景 --- 超过sampleTime，后续到达的Trace数据，直接转发而不是再等待sampleTime
				tce.sampler.Sample(id, toSendTraces, false)
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

func (tce *TraceCacheExtension) SetSampler(sampler tracecache.Sampler) {
	if tce.sampler == nil {
		tce.sampler = sampler
	} else {
		tce.logger.Warn("more than one sampler for traceCache",
			zap.String("name[1]", tce.sampler.Name()),
			zap.String("name[2]", sampler.Name()))
	}
}

func (tce *TraceCacheExtension) cleanOnTick() {
	currentBatch := tce.writeBatch.GetAndReset()
	sampleBatch, expireBatch := tce.bucket.CopyAndGetBatches(currentBatch)

	if tce.sampler != nil {
		for _, id := range sampleBatch {
			d, ok := tce.idToTrace.Load(id)
			if !ok {
				continue
			}
			traceData := d.(*traceData)
			// 通知采样器分析Trace数据
			tce.sampler.Sample(id, traceData.traces, true)

			// 清理Trace 内存数据，对于后续TraceId数据直接发送不做缓存
			traceData.CleanCacheTrace()
		}
	}

	cleanCount := 0
	for _, id := range expireBatch {
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
