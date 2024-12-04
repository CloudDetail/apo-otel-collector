package tracecacheextension

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/common/timeutils"
	"github.com/CloudDetail/apo-otel-collector/pkg/extension/tracecacheextension/internal/idbatcher"
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
	batcher         idbatcher.Batcher // 记录每秒TraceId分桶数据
	cleanTicker     timeutils.TTicker // 定时清理分桶数据
	tickerFrequency time.Duration
}

func newTraceCacheExtension(settings extension.Settings, cfg *Config) (*TraceCacheExtension, error) {
	numDecisionBatches := uint64(cfg.WaitTime.Seconds())

	inBatcher, err := idbatcher.New(numDecisionBatches, 0, uint64(2*runtime.NumCPU()))
	if err != nil {
		return nil, err
	}

	tce := &TraceCacheExtension{
		logger:          settings.Logger,
		batcher:         inBatcher,
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
	tce.batcher.Stop()
	tce.cleanTicker.Stop()
	return nil
}

func (tce *TraceCacheExtension) CacheTrace(traces ptrace.Traces) map[pcommon.TraceID]*tracecache.TraceMapping {
	result := make(map[pcommon.TraceID]*tracecache.TraceMapping)

	resourceSpans := traces.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rss := resourceSpans.At(i)
		resource := rss.Resource()
		idToSpans := groupSpansByTraceId(rss)
		for id, spans := range idToSpans {
			// 先缓存Trace数据
			d, loaded := tce.idToTrace.Load(id)
			if !loaded {
				d, loaded = tce.idToTrace.LoadOrStore(id, NewTraceData())
			}
			if !loaded {
				// 新的Bucket记录TraceId
				tce.batcher.AddToCurrentBatch(id)
			}
			traceData := d.(*TraceData)
			traceData.CacheTrace(&resource, spans)

			result[id] = traceData.traceMapping
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

func (tce *TraceCacheExtension) GetTrace(id pcommon.TraceID) *tracecache.TraceMapping {
	if d, loaded := tce.idToTrace.Load(id); loaded {
		traceData := d.(*TraceData)
		return traceData.traceMapping
	}
	return nil
}

func (tce *TraceCacheExtension) cleanOnTick() {
	batch, _ := tce.batcher.CloseCurrentAndTakeFirstBatch()

	cleanCount := 0
	for _, id := range batch {
		_, ok := tce.idToTrace.Load(id)
		if !ok {
			continue
		}

		// 清理数据
		tce.idToTrace.Delete(id)
		cleanCount += 1
	}
	if cleanCount > 0 {
		tce.logger.Info("[Clean Trace]", zap.Int("count", cleanCount))
	}
}
