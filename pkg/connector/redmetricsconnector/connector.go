package redmetricsconnector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/common/timeutils"
	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/parser"
	"github.com/tilinna/clock"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

const (
	metricKeySeparator  = string(byte(0))
	overflowServiceName = "overflow_service"
	overflowOperation   = "overflow_operation"

	AttributeSkywalkingSpanID = "sw8.span_id"
)

type metricKey string

type connectorImp struct {
	lock   sync.Mutex
	logger *zap.Logger
	config Config

	metricsConsumer consumer.Metrics

	// The starting time of the data points.
	startTimestamp pcommon.Timestamp

	// Parser
	dbParser   parser.Parser
	httpParser parser.Parser
	rpcParser  parser.Parser
	mqParser   parser.Parser

	// Histogram.
	serverHistograms       map[metricKey]cache.Histogram // 服务 指标
	dbCallHistograms       map[metricKey]cache.Histogram // db 指标
	externalCallHistograms map[metricKey]cache.Histogram // external 指标
	mqCallHistograms       map[metricKey]cache.Histogram // mq 指标
	bounds                 []float64

	keyValue *cache.ReusedKeyValue

	// An LRU cache of dimension key-value maps keyed by a unique identifier formed by a concatenation of its values:
	// e.g. { "foo/barOK": { "serviceName": "foo", "operation": "/bar", "status_code": "OK" }}
	metricKeyToDimensions             *cache.Cache[metricKey, pcommon.Map]
	dbCallMetricKeyToDimensions       *cache.Cache[metricKey, pcommon.Map]
	externalCallMetricKeyToDimensions *cache.Cache[metricKey, pcommon.Map]
	mqCallMetricKeyToDimensions       *cache.Cache[metricKey, pcommon.Map]

	ticker  *clock.Ticker
	done    chan struct{}
	started bool

	shutdownOnce sync.Once

	serviceToOperations                    map[string]map[string]struct{}
	maxNumberOfServicesToTrack             int
	maxNumberOfOperationsToTrackPerService int
	metricsType                            cache.MetricsType

	entryUrlCache       *cache.EntryUrlCache
	cleanUnMatcheTicker timeutils.TTicker
}

var (
	defaultLatencyHistogramBucketsMs = []float64{
		2_000_000, 4_000_000, 6_000_000, 8_000_000, 10_000_000, 50_000_000, 100_000_000, 200_000_000, 400_000_000, 800_000_000,
		1_000_000_000, 1_400_000_000, 2_000_000_000, 5_000_000_000, 10_000_000_000, 15_000_000_000, 30_000_000_000,
	}
)

func newConnector(logger *zap.Logger, config component.Config, ticker *clock.Ticker) (*connectorImp, error) {
	logger.Info("Building redmetricsconnector with config", zap.Any("config", config))
	pConfig := config.(*Config)

	if pConfig.DimensionsCacheSize <= 0 {
		return nil, fmt.Errorf(
			"invalid cache size: %v, the maximum number of the items in the cache should be positive",
			pConfig.DimensionsCacheSize,
		)
	}

	metricKeyToDimensionsCache, err := cache.NewCache[metricKey, pcommon.Map](pConfig.DimensionsCacheSize)
	if err != nil {
		return nil, err
	}

	dbMetricKeyToDimensionsCache, err := cache.NewCache[metricKey, pcommon.Map](pConfig.DimensionsCacheSize)
	if err != nil {
		return nil, err
	}

	externalMetricKeyToDimensionsCache, err := cache.NewCache[metricKey, pcommon.Map](pConfig.DimensionsCacheSize)
	if err != nil {
		return nil, err
	}

	mqMetricKeyToDimensionsCache, err := cache.NewCache[metricKey, pcommon.Map](pConfig.DimensionsCacheSize)
	if err != nil {
		return nil, err
	}

	bounds := defaultLatencyHistogramBucketsMs
	if pConfig.LatencyHistogramBuckets != nil {
		bounds = mapDurationsToNanos(pConfig.LatencyHistogramBuckets)
	}

	connector := &connectorImp{
		logger:                                 logger,
		config:                                 *pConfig,
		startTimestamp:                         pcommon.NewTimestampFromTime(time.Now()),
		dbParser:                               parser.NewDbParser(),
		httpParser:                             parser.NewHttpParser(pConfig.HttpParser),
		rpcParser:                              parser.NewRpcParser(),
		mqParser:                               parser.NewMqParser(),
		serverHistograms:                       make(map[metricKey]cache.Histogram),
		dbCallHistograms:                       make(map[metricKey]cache.Histogram),
		externalCallHistograms:                 make(map[metricKey]cache.Histogram),
		mqCallHistograms:                       make(map[metricKey]cache.Histogram),
		bounds:                                 bounds,
		keyValue:                               cache.NewReusedKeyValue(100), // 最大100的KeyValue 可重用Map
		metricKeyToDimensions:                  metricKeyToDimensionsCache,
		dbCallMetricKeyToDimensions:            dbMetricKeyToDimensionsCache,
		externalCallMetricKeyToDimensions:      externalMetricKeyToDimensionsCache,
		mqCallMetricKeyToDimensions:            mqMetricKeyToDimensionsCache,
		ticker:                                 ticker,
		done:                                   make(chan struct{}),
		serviceToOperations:                    make(map[string]map[string]struct{}),
		maxNumberOfServicesToTrack:             pConfig.MaxServicesToTrack,
		maxNumberOfOperationsToTrackPerService: pConfig.MaxOperationsToTrackPerService,
		metricsType:                            cache.GetMetricsType(pConfig.MetricsType),
	}
	if pConfig.ClientEntryUrlEnabled {
		connector.entryUrlCache = cache.NewEntryUrlCache(int64(pConfig.UnMatchUrlExpireTime.Seconds()))
		connector.cleanUnMatcheTicker = &timeutils.PolicyTicker{OnTickFunc: connector.cleanUnMatcheOnTick}
	}
	return connector, nil
}

func mapDurationsToNanos(vs []time.Duration) []float64 {
	vsm := make([]float64, len(vs))
	for i, v := range vs {
		vsm[i] = float64(v.Nanoseconds())
	}
	return vsm
}

// Start implements the component.Component interface.
func (p *connectorImp) Start(ctx context.Context, _ component.Host) error {
	if !p.config.ServerEnabled && !p.config.ExternalEnabled && !p.config.DbEnabled && !p.config.MqEnabled {
		p.logger.Warn("[Warning] Disable redmetrics connector")
		return nil
	}

	if p.config.ClientEntryUrlEnabled {
		p.cleanUnMatcheTicker.Start(time.Second)
	}

	p.started = true
	go func() {
		for {
			select {
			case <-p.done:
				return
			case <-p.ticker.C:
				p.exportMetrics(ctx)
			}
		}
	}()

	return nil
}

// Shutdown implements the component.Component interface.
func (p *connectorImp) Shutdown(context.Context) error {
	if p.config.ClientEntryUrlEnabled {
		p.cleanUnMatcheTicker.Stop()
	}

	p.shutdownOnce.Do(func() {
		p.logger.Info("Shutting down redmetrics connector")
		if p.started {
			p.logger.Info("Stopping ticker")
			p.ticker.Stop()
			p.done <- struct{}{}
			p.started = false
		}
	})
	return nil
}

// Capabilities implements the consumer interface.
func (p *connectorImp) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (p *connectorImp) cleanUnMatcheOnTick() {
	if resourceSpans := p.entryUrlCache.CleanUnMatchedSpans(); len(resourceSpans) > 0 {
		for _, resourceSpan := range resourceSpans {
			p.lock.Lock()
			// Send NotMatch ExitSpan With EntrySpan metric.
			p.aggregateMetricsForSpan(
				resourceSpan.Pid,
				resourceSpan.ContainerId,
				resourceSpan.ServiceName,
				"_unknown_",
				resourceSpan.Span,
			)
			p.lock.Unlock()
		}
	}
}

// ConsumeTraces implements the consumer.Traces interface.
// It aggregates the trace data to generate metrics.
func (p *connectorImp) ConsumeTraces(_ context.Context, traces ptrace.Traces) error {
	resourceSpans := traces.ResourceSpans()

	if p.entryUrlCache != nil {
		resourceSpans := p.entryUrlCache.GetMatchedResourceSpans(traces)
		for _, resourceSpan := range resourceSpans {
			p.lock.Lock()
			p.aggregateMetricsForSpan(
				resourceSpan.Pid,
				resourceSpan.ContainerId,
				resourceSpan.ServiceName,
				resourceSpan.EntryUrl,
				resourceSpan.Span,
			)
			p.lock.Unlock()
		}
	} else {
		for i := 0; i < resourceSpans.Len(); i++ {
			rss := resourceSpans.At(i)

			pid, serviceName, containerId := cache.GetResourceAttribute(rss)
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

					p.lock.Lock()
					p.aggregateMetricsForSpan(pid, containerId, serviceName, "", &span)
					p.lock.Unlock()
				}
			}
		}
	}

	return nil
}

func (p *connectorImp) exportMetrics(ctx context.Context) {
	p.lock.Lock()

	m, err := p.buildMetrics()

	// This component no longer needs to read the metrics once built, so it is safe to unlock.
	p.lock.Unlock()

	if err != nil {
		p.logger.Error("Failed to build metrics", zap.Error(err))
	}

	if err := p.metricsConsumer.ConsumeMetrics(ctx, m); err != nil {
		p.logger.Error("Failed ConsumeMetrics", zap.Error(err))
		return
	}
}

// buildMetrics collects the computed raw metrics data, builds the metrics object and
// writes the raw metrics data into the metrics object.
func (p *connectorImp) buildMetrics() (pmetric.Metrics, error) {
	m := pmetric.NewMetrics()
	ilm := m.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
	ilm.Scope().SetName("redmetricsconnector")

	if err := p.collectLatencyMetrics(ilm, p.metricKeyToDimensions, p.serverHistograms, "kindling_span_trace_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}
	if err := p.collectLatencyMetrics(ilm, p.dbCallMetricKeyToDimensions, p.dbCallHistograms, "kindling_db_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}
	if err := p.collectLatencyMetrics(ilm, p.externalCallMetricKeyToDimensions, p.externalCallHistograms, "kindling_external_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}
	if err := p.collectLatencyMetrics(ilm, p.mqCallMetricKeyToDimensions, p.mqCallHistograms, "kindling_mq_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}

	p.metricKeyToDimensions.RemoveEvictedItems()
	p.dbCallMetricKeyToDimensions.RemoveEvictedItems()
	p.externalCallMetricKeyToDimensions.RemoveEvictedItems()
	p.mqCallMetricKeyToDimensions.RemoveEvictedItems()

	for key := range p.serverHistograms {
		if !p.metricKeyToDimensions.Contains(key) {
			delete(p.serverHistograms, key)
		}
	}
	for key := range p.dbCallHistograms {
		if !p.dbCallMetricKeyToDimensions.Contains(key) {
			delete(p.dbCallHistograms, key)
		}
	}
	for key := range p.externalCallHistograms {
		if !p.dbCallMetricKeyToDimensions.Contains(key) {
			delete(p.externalCallHistograms, key)
		}
	}
	for key := range p.mqCallHistograms {
		if !p.mqCallMetricKeyToDimensions.Contains(key) {
			delete(p.mqCallHistograms, key)
		}
	}
	return m, nil
}

// collectLatencyMetrics collects the raw latency metrics, writing the data
// into the given instrumentation library metrics.
func (p *connectorImp) collectLatencyMetrics(ilm pmetric.ScopeMetrics,
	metricKeyToDimensions *cache.Cache[metricKey, pcommon.Map],
	histograms map[metricKey]cache.Histogram,
	prefix string) error {
	if len(histograms) == 0 {
		return nil
	}

	if p.metricsType == cache.MetricsProm {
		return p.collectPromLatencyMetrics(ilm, metricKeyToDimensions, histograms, prefix)
	} else {
		return collectVmLatencyMetrics(ilm, metricKeyToDimensions, histograms, prefix)
	}
}

func (p *connectorImp) collectPromLatencyMetrics(ilm pmetric.ScopeMetrics,
	metricKeyToDimensions *cache.Cache[metricKey, pcommon.Map],
	histograms map[metricKey]cache.Histogram,
	prefix string) error {

	mLatency := ilm.Metrics().AppendEmpty()
	mLatency.SetName(prefix)
	mLatency.SetUnit("ns")
	mLatency.SetEmptyHistogram().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dps := mLatency.Histogram().DataPoints()
	dps.EnsureCapacity(len(histograms))
	timestamp := pcommon.NewTimestampFromTime(time.Now())
	for key, hist := range histograms {
		dimensions, ok := metricKeyToDimensions.Get(key)
		if !ok {
			return fmt.Errorf("value not found in metricKeyToDimensions cache by key %q", key)
		}
		values := hist.Read()

		dpLatency := dps.AppendEmpty()
		dpLatency.SetStartTimestamp(p.startTimestamp)
		dpLatency.SetTimestamp(timestamp)
		dpLatency.ExplicitBounds().FromRaw(p.bounds)
		dpLatency.BucketCounts().FromRaw(values.GetBucketCounts())
		dpLatency.SetCount(values.GetCount())
		dpLatency.SetSum(values.GetSum())
		dimensions.CopyTo(dpLatency.Attributes())
	}
	return nil
}

func collectVmLatencyMetrics(ilm pmetric.ScopeMetrics,
	metricKeyToDimensions *cache.Cache[metricKey, pcommon.Map],
	histograms map[metricKey]cache.Histogram,
	prefix string) error {

	timestamp := pcommon.NewTimestampFromTime(time.Now())
	mLatency := ilm.Metrics().AppendEmpty()
	mLatency.SetName(fmt.Sprintf("%s_bucket", prefix))
	mLatency.SetEmptyGauge()
	dps := mLatency.Gauge().DataPoints()

	mLatencyCount := ilm.Metrics().AppendEmpty()
	mLatencyCount.SetName(fmt.Sprintf("%s_count", prefix))
	mLatencyCount.SetEmptyGauge()
	countDps := mLatencyCount.Gauge().DataPoints()

	mLatencySum := ilm.Metrics().AppendEmpty()
	mLatencySum.SetName(fmt.Sprintf("%s_sum", prefix))
	mLatencySum.SetEmptyGauge()
	sumDps := mLatencySum.Gauge().DataPoints()
	for key, hist := range histograms {
		dimensions, ok := metricKeyToDimensions.Get(key)
		if !ok {
			return fmt.Errorf("value not found in metricKeyToDimensions cache by key %q", key)
		}

		values := hist.Read()
		for bucket, count := range values.GetBuckets() {
			dpLatency := dps.AppendEmpty()
			dpLatency.SetTimestamp(timestamp)
			dpLatency.SetIntValue(int64(count))

			dpLatency.Attributes().EnsureCapacity(dimensions.Len() + 1)
			dimensions.CopyTo(dpLatency.Attributes())
			dpLatency.Attributes().PutStr("vmrange", bucket)
		}

		if values.GetCount() > 0 {
			dpLatencyCount := countDps.AppendEmpty()
			dpLatencyCount.SetTimestamp(timestamp)
			dpLatencyCount.SetIntValue(int64(values.GetCount()))
			dimensions.CopyTo(dpLatencyCount.Attributes())

			dpLatencySum := sumDps.AppendEmpty()
			dpLatencySum.SetTimestamp(timestamp)
			dpLatencySum.SetIntValue(int64(values.GetSum()))
			dimensions.CopyTo(dpLatencySum.Attributes())
		}
	}
	return nil
}

func (p *connectorImp) aggregateMetricsForSpan(pid string, containerId string, serviceName string, entryUrl string, span *ptrace.Span) {
	// Protect against end timestamps before start timestamps. Assume 0 duration.
	startTime := span.StartTimestamp()
	endTime := span.EndTimestamp()
	if endTime <= startTime {
		return
	}
	latencyInNanoseconds := float64(endTime - startTime)

	if p.config.ServerEnabled && (span.Kind() == ptrace.SpanKindServer || span.Kind() == ptrace.SpanKindConsumer) {
		swSpanId, exist := span.Attributes().Get(AttributeSkywalkingSpanID)
		// 过滤Skywalking Undertow task线程的SpringMVC Entry
		if !exist || swSpanId.Int() == 0 {
			key := metricKey(p.buildServerKey(pid, containerId, serviceName, span))
			if _, has := p.metricKeyToDimensions.Get(key); !has {
				p.metricKeyToDimensions.Add(key, p.keyValue.GetMap())
			}
			p.updateHistogram(p.serverHistograms, key, latencyInNanoseconds)
		}
	}

	spanAttr := span.Attributes()
	if span.Kind() == ptrace.SpanKindClient {
		if p.config.DbEnabled {
			if dbKey := p.dbParser.Parse(p.logger, pid, containerId, serviceName, entryUrl, span, spanAttr, p.keyValue); dbKey != "" {
				dbCallKey := metricKey(dbKey)
				if _, has := p.dbCallMetricKeyToDimensions.Get(dbCallKey); !has {
					p.dbCallMetricKeyToDimensions.Add(dbCallKey, p.keyValue.GetMap())
				}
				p.updateHistogram(p.dbCallHistograms, dbCallKey, latencyInNanoseconds)
			}
		}

		if p.config.ExternalEnabled {
			var externalKey string
			if httpKey := p.httpParser.Parse(p.logger, pid, containerId, serviceName, entryUrl, span, spanAttr, p.keyValue); httpKey != "" {
				externalKey = httpKey
			} else if rpcKey := p.rpcParser.Parse(p.logger, pid, containerId, serviceName, entryUrl, span, spanAttr, p.keyValue); rpcKey != "" {
				externalKey = rpcKey
			} else {
				_, dbSystemExist := spanAttr.Get(conventions.AttributeDBSystem)
				_, mqSystemExist := spanAttr.Get(conventions.AttributeMessagingSystem)
				if !dbSystemExist && !mqSystemExist {
					// 过滤DB 和 Mq
					externalKey = parser.BuildExternalKey(p.keyValue, pid, containerId, serviceName, entryUrl,
						span.Name(),
						parser.GetClientPeer(spanAttr),
						"unknown",
						span.Status().Code() == ptrace.StatusCodeError,
					)
				}
			}
			if externalKey != "" {
				externalCallKey := metricKey(externalKey)
				if _, has := p.externalCallMetricKeyToDimensions.Get(externalCallKey); !has {
					p.externalCallMetricKeyToDimensions.Add(externalCallKey, p.keyValue.GetMap())
				}
				p.updateHistogram(p.externalCallHistograms, externalCallKey, latencyInNanoseconds)
			}
		}
	}

	if p.config.MqEnabled {
		if mqKey := p.mqParser.Parse(p.logger, pid, containerId, serviceName, entryUrl, span, spanAttr, p.keyValue); mqKey != "" {
			mqCallKey := metricKey(mqKey)
			if _, has := p.mqCallMetricKeyToDimensions.Get(mqCallKey); !has {
				p.mqCallMetricKeyToDimensions.Add(mqCallKey, p.keyValue.GetMap())
			}
			p.updateHistogram(p.mqCallHistograms, mqCallKey, latencyInNanoseconds)
		}
	}
}

// buildKey Server Red指标
func (p *connectorImp) buildServerKey(pid string, containerId string, serviceName string, span *ptrace.Span) string {
	spanName := span.Name()
	if len(p.serviceToOperations) > p.maxNumberOfServicesToTrack {
		p.logger.Warn("Too many services to track, using overflow service name", zap.Int("maxNumberOfServicesToTrack", p.maxNumberOfServicesToTrack))
		serviceName = overflowServiceName
	}
	if len(p.serviceToOperations[serviceName]) > p.maxNumberOfOperationsToTrackPerService {
		p.logger.Warn("Too many operations to track, using overflow operation name", zap.Int("maxNumberOfOperationsToTrackPerService", p.maxNumberOfOperationsToTrackPerService))
		spanName = overflowOperation
	}

	if _, ok := p.serviceToOperations[serviceName]; !ok {
		p.serviceToOperations[serviceName] = make(map[string]struct{})
	}
	p.serviceToOperations[serviceName][spanName] = struct{}{}

	return parser.BuildServerKey(p.keyValue, pid, containerId, serviceName,
		span.Name(),
		span.ParentSpanID().IsEmpty(),
		span.Status().Code() == ptrace.StatusCodeError,
	)
}

// updateHistogram adds the histogram sample to the histogram defined by the metric key.
func (p *connectorImp) updateHistogram(histograms map[metricKey]cache.Histogram, key metricKey, latency float64) {
	histo, ok := histograms[key]
	if !ok {
		histo = cache.NewHistogram(p.metricsType, p.bounds)
		histograms[key] = histo
	}
	histo.Update(latency)
}
