package backsamplingprocessor

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/adaptive"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/cache"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/metadata"
	grpc_model "github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/model"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/notify"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/profile"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/sampler"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/timeutils"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"go.uber.org/zap"
)

const (
	apmTypeOtel = "otel"

	envNodeName = "MY_NODE_NAME"
	envNodeIp   = "MY_NODE_IP"

	AttributeSkywalkingComponmentId string = "sw8.componment_id"
	ComponentUndertow               int64  = 84
)

var (
	attrNodeIp             = attribute.String("node_ip", getNodeIp())
	measureNodeIp          = metric.WithAttributes(attrNodeIp)
	measureSampleReceived  = metric.WithAttributes(attrNodeIp, attribute.String("sample", "received"))
	measureSampleDrop      = metric.WithAttributes(attrNodeIp, attribute.String("sample", "drop"))
	measureSampleSingle    = metric.WithAttributes(attrNodeIp, attribute.String("sample", "single"))
	measureAdaptiveSampled = metric.WithAttributes(attrNodeIp, attribute.String("sample", "adaptive"))
	measureTraceSampled    = metric.WithAttributes(attrNodeIp, attribute.String("sample", "sampled"))
)

type backSamplingProcessor struct {
	ctx context.Context

	cfg       Config
	telemetry *metadata.TelemetryBuilder

	logEnable           bool
	logger              *zap.Logger
	nodeIp              string
	nodeName            string
	profileQueryTime    int64
	slowThresholdTicker timeutils.TTicker
	sampleTicker        timeutils.TTicker
	sendTraceTicker     timeutils.TTicker
	cleanCacheTicker    timeutils.TTicker
	notifyProfileTicker timeutils.TTicker
	notifyTicker        timeutils.TTicker

	tailbaseCache *cache.ReadableCache[*model.TraceLabels]
	sampleApi     *sampler.SampleApi
	sampleCache   *sampler.SampleCache
	notifyApi     notify.Notify
	profileApi    profile.ProfileApi

	memoryCheck            *adaptive.MemoryCheck
	adaptiveSampler        *adaptive.AdaptiveSampler
	singleSampler          *sampler.SingleSampler
	otelTraceSampler       *cache.OtelTraceSampler
	cleanOtelTraceTicker   timeutils.TTicker
	querySampleValueTicker timeutils.TTicker
	nextConsumer           consumer.Traces
}

// newTracesProcessor returns a processor.TracesProcessor that will perform tail sampling according to the given
// configuration.
func newTracesProcessor(ctx context.Context, settings component.TelemetrySettings, nextConsumer consumer.Traces, cfg Config) (processor.Traces, error) {
	settings.Logger.Info("Building backsamplingprocessor with config", zap.Any("config", cfg))
	telemetry, err := metadata.NewTelemetryBuilder(settings)
	if err != nil {
		return nil, err
	}
	sampleApi, err := sampler.NewSampleApi(cfg.Controller.Host, cfg.Controller.Port, cfg.Adaptive.Enable, cfg.Sampler.LogEnable)
	if err != nil {
		return nil, err
	}
	var notifyApi notify.Notify
	if cfg.Notify.Enabled {
		notifyApi, err = notify.NewUdsRegistryServer(cfg.Sampler.LogEnable)
		if err != nil {
			return nil, err
		}
	} else {
		notifyApi = &notify.NoopNotify{}
	}

	sampleCache := sampler.NewSampleCache(
		cfg.Sampler.OpenSlowSampling,
		cfg.Sampler.OpenErrorSampling,
		cfg.Sampler.SampleIgnoreThreshold*1000000,
	).WithNormalSampler(
		cfg.Sampler.NormalTopSample,
		cfg.Sampler.NormalSampleWaitTime,
		cfg.Sampler.LogEnable,
	).WithTailBaseSampler(
		cfg.Sampler.SampleRetryNum,
		cfg.Sampler.SampleWaitTime,
		cfg.Sampler.LogEnable,
	).WithSilent(
		cfg.Sampler.SilentCount,
		cfg.Sampler.SilentPeriod,
		cfg.Sampler.SilentMode,
	)

	bsp := &backSamplingProcessor{
		ctx:           ctx,
		cfg:           cfg,
		telemetry:     telemetry,
		logEnable:     cfg.Sampler.LogEnable,
		logger:        settings.Logger,
		nodeIp:        getNodeIp(),
		nodeName:      getNodeName(),
		tailbaseCache: cache.NewReadableCache[*model.TraceLabels]("SpanTrace", cfg.Sampler.SampleWaitTime),
		sampleApi:     sampleApi,
		sampleCache:   sampleCache,
		notifyApi:     notifyApi,
		profileApi:    profile.NewProfileApi(settings.Logger, getNodeIp(), cfg.EbpfPort),
		nextConsumer:  nextConsumer,
	}
	if cfg.Adaptive.Enable {
		bsp.singleSampler = sampler.NewSingleSampler(
			cfg.Sampler.OpenSlowSampling,
			cfg.Sampler.OpenErrorSampling,
			cfg.Sampler.SampleIgnoreThreshold*1000000,
		).WithNormalSampler(
			cfg.Sampler.NormalSampleWaitTime,
		).WithSilent(
			cfg.Sampler.SilentCount,
			cfg.Sampler.SilentPeriod,
			cfg.Sampler.SilentMode,
		)
		bsp.adaptiveSampler = adaptive.NewAdaptiveSampler(
			cfg.Adaptive.SlowThreshold,
			cfg.Adaptive.ServiceSampleWindow,
			cfg.Adaptive.ServiceSampleCount,
			cfg.Adaptive.TraceIdHoldTime,
		)
		bsp.otelTraceSampler = cache.NewOtelTraceSampler(bsp.logger,
			cfg.Adaptive.TraceIdHoldTime,
			cfg.Sampler.SampleWaitTime,
		)
		bsp.memoryCheck = adaptive.NewMemoryCheck(cfg.Adaptive.MemoryLimitMib)
		bsp.cleanOtelTraceTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.cleanOtelTraceOnTick}
		bsp.querySampleValueTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.querySampleValueOnTick}
	}
	bsp.slowThresholdTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.slowThresholdOnTick}
	bsp.sampleTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.samplingOnTick}
	bsp.sendTraceTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.sendTraceOnTick}
	bsp.cleanCacheTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.cleanCacheOnTick}
	if bsp.cfg.EbpfPort > 0 {
		bsp.notifyProfileTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.notifyProfileOnTicker}
	}
	if bsp.cfg.Notify.Enabled {
		bsp.notifyTicker = &timeutils.PolicyTicker{OnTickFunc: bsp.notifyOnTick}
	}
	return bsp, nil
}

func (bsp *backSamplingProcessor) Start(ctx context.Context, _ component.Host) error {
	bsp.slowThresholdTicker.Start(time.Duration(bsp.cfg.Controller.IntervalQuerySlow) * time.Second)
	bsp.sampleTicker.Start(time.Duration(bsp.cfg.Controller.IntervalQuerySample) * time.Second)
	bsp.sendTraceTicker.Start(time.Duration(bsp.cfg.Controller.IntervalSendTrace) * time.Second)
	bsp.cleanCacheTicker.Start(time.Second)
	if bsp.cfg.EbpfPort > 0 {
		bsp.notifyProfileTicker.Start(time.Second)
	}
	if bsp.cfg.Notify.Enabled {
		bsp.notifyTicker.Start(time.Second)
	}
	if bsp.cfg.Adaptive.Enable {
		bsp.cleanOtelTraceTicker.Start(time.Second)
		bsp.querySampleValueTicker.Start(bsp.cfg.Adaptive.MemoryCheckInterval)
	}
	return nil
}

func (bsp *backSamplingProcessor) Shutdown(context.Context) error {
	bsp.logger.Info("Shutting down backsampling processor")

	if bsp.cfg.Adaptive.Enable {
		bsp.cleanOtelTraceTicker.Stop()
		bsp.querySampleValueTicker.Stop()
	}
	bsp.slowThresholdTicker.Stop()
	bsp.sampleTicker.Stop()
	bsp.sendTraceTicker.Stop()
	bsp.cleanCacheTicker.Stop()
	if bsp.cfg.EbpfPort > 0 {
		bsp.notifyProfileTicker.Stop()
	}
	if bsp.cfg.Notify.Enabled {
		bsp.notifyTicker.Stop()
	}
	return nil
}

func (bsp *backSamplingProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// ConsumeTraces is required by the processor.Traces interface.
func (bsp *backSamplingProcessor) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	if bsp.adaptiveSampler != nil {
		resourceSpans := traces.ResourceSpans()
		for i := 0; i < resourceSpans.Len(); i++ {
			rss := resourceSpans.At(i)
			resource := rss.Resource()
			pid, serviceName, containerId := getAppInfo(resource)
			if serviceName == "" {
				continue
			}
			swTraceId := getSkywalkingTraceId(resource)
			idToSpans := groupSpansByTraceId(rss)
			for traceId, spans := range idToSpans {
				bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(len(spans)), measureSampleReceived)
				// Adaptive PreSample
				adaptiveResult := bsp.adaptiveSampler.Sample(traceId, swTraceId, serviceName, spans)
				if adaptiveResult.IsDrop() {
					bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(len(spans)), measureSampleDrop)
					continue
				}
				reportType := adaptiveResult.GetReportType()
				for _, span := range spans {
					bsp.buildSpanTrace(pid, containerId, serviceName, span, reportType, bsp.adaptiveSampler.GetSampleValue())
				}

				if adaptiveResult.IsSingleStore() {
					bsp.nextConsumer.ConsumeTraces(ctx, cache.BuildTraces(&resource, spans))
					bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(len(spans)), measureSampleSingle)
				} else {
					bsp.otelTraceSampler.Cache(traceId, cache.NewOtelTrace(&resource, spans))
					bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(len(spans)), measureAdaptiveSampled)
				}
			}
		}
		return nil
	} else {
		for i := 0; i < traces.ResourceSpans().Len(); i++ {
			rspans := traces.ResourceSpans().At(i)
			resource := rspans.Resource()
			pid, serviceName, containerId := getAppInfo(resource)
			if serviceName == "" {
				continue
			}
			ilsSlice := rspans.ScopeSpans()
			for j := 0; j < ilsSlice.Len(); j++ {
				ils := ilsSlice.At(j)
				spans := ils.Spans()
				bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(spans.Len()), measureSampleReceived)
				for k := 0; k < spans.Len(); k++ {
					span := spans.At(k)
					bsp.buildSpanTrace(pid, containerId, serviceName, &span, 0, 0)
				}
			}
		}
		return bsp.nextConsumer.ConsumeTraces(ctx, traces)
	}
}

func (bsp *backSamplingProcessor) buildSpanTrace(pid uint32, containerId string, serviceName string, span *ptrace.Span, reportType uint32, sampleValue int64) {
	startTime := span.StartTimestamp()
	endTime := span.EndTimestamp()
	if endTime <= startTime {
		return
	}

	if span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer {
		spanAttr := span.Attributes()
		if swComponmentId, exist := span.Attributes().Get(AttributeSkywalkingComponmentId); exist && swComponmentId.Int() == ComponentUndertow {
			return
		}
		contentKey := span.Name()
		var threadId uint32 = 0
		if threadIdAttr, ok := spanAttr.Get(conventions.AttributeThreadID); ok {
			threadId = uint32(threadIdAttr.Int())
		}
		trace := &model.TraceLabels{
			Pid:         pid,
			Tid:         threadId,
			TopSpan:     span.ParentSpanID().IsEmpty(),
			Protocol:    "http",
			ServiceName: serviceName,
			Url:         contentKey,
			HttpUrl:     contentKey,
			IsSampled:   true,
			IsSlow:      false,
			IsServer:    true,
			IsError:     span.Status().Code() == ptrace.StatusCodeError,
			ReportType:  reportType,
			SampleValue: int(sampleValue),
			TraceId:     span.TraceID().String(),
			ApmType:     apmTypeOtel,
			ApmSpanId:   span.SpanID().String(),
			ContainerId: containerId,
			StartTime:   uint64(span.StartTimestamp()),
			Duration:    uint64(span.EndTimestamp() - span.StartTimestamp()),
			EndTime:     uint64(span.EndTimestamp()),
			NodeName:    bsp.nodeName,
			NodeIp:      bsp.nodeIp,
		}

		if reportType == 0 {
			sampleResult := bsp.sampleCache.GetSampleResult(trace)
			switch sampleResult {
			case sampler.SampleResultTrace, sampler.SampleResultTraceLog:
				bsp.storeTrace(trace)
			case sampler.SampleResultTraceProfile:
				bsp.storeTrace(trace)
				bsp.storeSlowProfile(trace)
			case sampler.SampleResultNormal:
				bsp.storeTrace(trace)
				bsp.storeNormalProfile(trace)
			case sampler.SampleResultCache:
				bsp.sampleCache.CacheTrace(trace)
			}
		} else if bsp.singleSampler != nil && bsp.singleSampler.Sample(trace) {
			bsp.storeTrace(trace)
			if trace.IsSlow {
				bsp.storeSlowProfile(trace)
			} else {
				bsp.storeNormalProfile(trace)
			}
		}

		if bsp.otelTraceSampler != nil && trace.IsProfiled {
			bsp.otelTraceSampler.SetSampledTraceId(trace.TraceId)
		}
	}
}

func (bsp *backSamplingProcessor) storeTrace(trace *model.TraceLabels) {
	bsp.sampleApi.StoreTrace(trace)
	if trace.IsError || trace.IsSlow || trace.IsProfiled {
		bsp.notifyApi.SendSignal(trace)
	}
}

func (bsp *backSamplingProcessor) storeSlowProfile(trace *model.TraceLabels) {
	bsp.profileApi.AddProfileSignal(trace, 3)
}

func (bsp *backSamplingProcessor) storeNormalProfile(trace *model.TraceLabels) {
	bsp.profileApi.AddProfileSignal(trace, 1)
}

func (bsp *backSamplingProcessor) slowThresholdOnTick() {
	if resp, err := bsp.sampleApi.QuerySlowThreshold(bsp.nodeIp); err == nil {
		bsp.sampleCache.UpdateSlowThreshold(resp.Datas)
	}
}

func (bsp *backSamplingProcessor) samplingOnTick() {
	slowIds, errorIds, normalIds := bsp.sampleCache.GetSampledTraceIds()
	query := &grpc_model.ProfileQuery{
		QueryTime:      bsp.profileQueryTime,
		NodeIp:         bsp.nodeIp,
		SlowTraceIds:   slowIds,
		ErrorTraceIds:  errorIds,
		NormalTraceIds: normalIds,
	}
	if resp, err := bsp.sampleApi.QueryProfiles(query); err == nil {
		bsp.profileQueryTime = resp.QueryTime
		bsp.sampleCache.UpdateSampledTraceIds(resp, len(slowIds), len(errorIds), len(normalIds))

		if bsp.otelTraceSampler != nil {
			for _, slowTraceId := range resp.SlowTraceIds {
				bsp.otelTraceSampler.SetSampledTraceId(slowTraceId)
			}
			for _, errorTraceId := range resp.ErrorTraceIds {
				bsp.otelTraceSampler.SetSampledTraceId(errorTraceId)
			}
			for _, normalTraceId := range resp.NormalTraceIds {
				bsp.otelTraceSampler.SetSampledTraceId(normalTraceId)
			}
		}
	} else {
		bsp.sampleCache.UpdateUnSentCount(len(slowIds), len(errorIds), len(normalIds))
	}
}

func (bsp *backSamplingProcessor) sendTraceOnTick() {
	bsp.sampleApi.SendTraces()
}

func (bsp *backSamplingProcessor) cleanCacheOnTick() {
	now := time.Now().Unix()

	sampledIds := bsp.sampleCache.GetAndCleanTailbaseTraces()

	bsp.tailbaseCache.Copy(sampledIds.TraceCache)
	errorTraces := bsp.tailbaseCache.PickMatchDatas(sampledIds.ErrorIds)
	slowTraces := bsp.tailbaseCache.PickMatchDatas(sampledIds.SlowIds)
	normalTraces := bsp.tailbaseCache.PickMatchDatas(sampledIds.NormalIds)

	bsp.tailbaseCache.CleanExpireDatas(bsp.logEnable)

	for _, errorTrace := range errorTraces {
		if bsp.logEnable {
			log.Printf("[%s] %s is stored by error tailBase.", errorTrace.ServiceName, errorTrace.TraceId)
		}
		bsp.storeTrace(errorTrace)
		bsp.sampleCache.UpdateNoramlTime(errorTrace)
	}

	for _, slowTrace := range slowTraces {
		if bsp.logEnable {
			log.Printf("[%s] %s is stored by slow tailBase.", slowTrace.ServiceName, slowTrace.TraceId)
		}
		bsp.storeTrace(slowTrace)
		if bsp.cfg.Sampler.EnableTailbaseProfiling {
			bsp.storeSlowProfile(slowTrace)
		}
		bsp.sampleCache.UpdateNoramlTime(slowTrace)
	}

	for _, normalTrace := range normalTraces {
		if bsp.logEnable {
			log.Printf("[%s] %s is stored by normal tailBase.", normalTrace.ServiceName, normalTrace.TraceId)
		}
		bsp.storeTrace(normalTrace)
		bsp.sampleCache.UpdateNoramlTime(normalTrace)
	}

	bsp.sampleCache.CheckWindow(now)
}

func (bsp *backSamplingProcessor) cleanOtelTraceOnTick() {
	now := time.Now().Unix()

	if bsp.otelTraceSampler != nil {
		traces := bsp.otelTraceSampler.PickSampledTraces(bsp.logEnable)
		for _, trace := range traces {
			// Send Sampled Trace
			bsp.telemetry.ProcessorBackSamplingSpanCount.Add(bsp.ctx, int64(len(trace.Spans)), measureTraceSampled)
			bsp.nextConsumer.ConsumeTraces(context.Background(), trace.ConvertToTraces())
		}
	}
	if bsp.singleSampler != nil {
		bsp.singleSampler.CheckWindow(now)
	}

	if bsp.adaptiveSampler != nil {
		bsp.adaptiveSampler.Reset()
	}
}

func (bsp *backSamplingProcessor) querySampleValueOnTick() {
	if bsp.memoryCheck != nil {
		metric := &grpc_model.SampleMetric{
			QueryTime:   time.Now().Unix(),
			NodeIp:      bsp.nodeIp,
			CacheSecond: int64(bsp.cfg.Sampler.SampleWaitTime),
			Memory:      bsp.memoryCheck.GetCurrentMemory(),
			MemoryLimit: bsp.memoryCheck.GetMemoryLimit(),
		}
		if result, _ := bsp.sampleApi.GetSampleValue(metric); result != nil {
			bsp.telemetry.ProcessorBackSamplingSampleValue.Record(bsp.ctx, result.Value, measureNodeIp)
			oldSampleValue := bsp.adaptiveSampler.GetSampleValue()
			if result.Value >= 0 && result.Value < 20 && oldSampleValue != result.Value {
				bsp.logger.Info("Update SampleValue", zap.Int64("old", oldSampleValue), zap.Int64("new", result.Value))
				bsp.adaptiveSampler.SetSampleValue(result.Value)
			}
		}
	}
}

func (bsp *backSamplingProcessor) notifyProfileOnTicker() {
	bsp.profileApi.ConsumeProfileSignals()
}

func (bsp *backSamplingProcessor) notifyOnTick() {
	bsp.notifyApi.BatchSendSignals()
}

func getNodeName() string {
	if nodeNameFromEnv, exist := os.LookupEnv(envNodeName); exist {
		return nodeNameFromEnv
	}
	if hostName, err := os.Hostname(); err == nil {
		return hostName
	}
	return "Unknown"
}

func getNodeIp() string {
	if nodeIpFromEnv, exist := os.LookupEnv(envNodeIp); exist {
		return nodeIpFromEnv
	}
	return ""
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

func getAppInfo(resource pcommon.Resource) (pid uint32, serviceName string, containerId string) {
	resourceAttr := resource.Attributes()
	pidIntValue := fillproc.GetPid(resourceAttr)
	if pidIntValue <= 0 {
		return
	}
	serviceAttr, ok := resourceAttr.Get(conventions.AttributeServiceName)
	if !ok {
		return
	}
	pid = uint32(pidIntValue)
	serviceName = serviceAttr.Str()
	containerId = fillproc.GetContainerId(resourceAttr)
	if len(containerId) > 12 {
		containerId = containerId[:12]
	}
	return
}

func getSkywalkingTraceId(resource pcommon.Resource) string {
	resourceAttr := resource.Attributes()
	if swTraceIdAttr, ok := resourceAttr.Get("sw8.trace_id"); ok {
		return swTraceIdAttr.Str()
	}
	return ""
}
