package redmetricsconnector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"github.com/CloudDetail/apo-otel-collector/pkg/sqlprune"
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

	envNodeName = "MY_NODE_NAME"
	envNodeIP   = "MY_NODE_IP"

	keyServiceName = "svc_name"
	keyContentKey  = "content_key"
	keyNodeName    = "node_name"
	keyNodeIp      = "node_ip"
	keyTopSpan     = "top_span"
	keyIsError     = "is_error"
	keyPid         = "pid"
	keyContainerId = "container_id"

	keyName     = "name"
	keyDbSystem = "db_system"
	keyDbName   = "db_name"
	keyDbUrl    = "db_url"

	AttributeSkywalkingSpanID = "sw8.span_id"
)

type metricKey string

type connectorImp struct {
	lock   sync.Mutex
	logger *zap.Logger
	config Config

	metricsConsumer consumer.Metrics

	// 主机名
	nodeName string
	nodeIp   string

	// Histogram.
	serverHistograms map[metricKey]*cache.Histogram // 服务 指标
	dbCallHistograms map[metricKey]*cache.Histogram // db 指标

	keyValue *ReusedKeyValue

	// An LRU cache of dimension key-value maps keyed by a unique identifier formed by a concatenation of its values:
	// e.g. { "foo/barOK": { "serviceName": "foo", "operation": "/bar", "status_code": "OK" }}
	metricKeyToDimensions       *cache.Cache[metricKey, pcommon.Map]
	dbCallMetricKeyToDimensions *cache.Cache[metricKey, pcommon.Map]

	ticker  *clock.Ticker
	done    chan struct{}
	started bool

	shutdownOnce sync.Once

	serviceToOperations                    map[string]map[string]struct{}
	maxNumberOfServicesToTrack             int
	maxNumberOfOperationsToTrackPerService int
}

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

	return &connectorImp{
		logger:                                 logger,
		config:                                 *pConfig,
		nodeName:                               getNodeName(),
		nodeIp:                                 getNodeIp(),
		serverHistograms:                       make(map[metricKey]*cache.Histogram),
		dbCallHistograms:                       make(map[metricKey]*cache.Histogram),
		keyValue:                               newKeyValue(100), // 最大100的KeyValue 可重用Map
		metricKeyToDimensions:                  metricKeyToDimensionsCache,
		dbCallMetricKeyToDimensions:            dbMetricKeyToDimensionsCache,
		ticker:                                 ticker,
		done:                                   make(chan struct{}),
		serviceToOperations:                    make(map[string]map[string]struct{}),
		maxNumberOfServicesToTrack:             pConfig.MaxServicesToTrack,
		maxNumberOfOperationsToTrackPerService: pConfig.MaxOperationsToTrackPerService,
	}, nil
}

// Start implements the component.Component interface.
func (p *connectorImp) Start(ctx context.Context, _ component.Host) error {
	if !p.config.DbEnabled && !p.config.ServerEnabled {
		p.logger.Warn("[Warning] Disable redmetrics connector")
		return nil
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

// ConsumeTraces implements the consumer.Traces interface.
// It aggregates the trace data to generate metrics.
func (p *connectorImp) ConsumeTraces(_ context.Context, traces ptrace.Traces) error {
	p.lock.Lock()
	p.aggregateMetrics(traces)
	p.lock.Unlock()
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

	if err := collectLatencyMetrics(ilm, p.metricKeyToDimensions, p.serverHistograms, "kindling_span_trace_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}
	if err := collectLatencyMetrics(ilm, p.dbCallMetricKeyToDimensions, p.dbCallHistograms, "kindling_db_duration_nanoseconds"); err != nil {
		return pmetric.Metrics{}, err
	}

	p.metricKeyToDimensions.RemoveEvictedItems()
	p.dbCallMetricKeyToDimensions.RemoveEvictedItems()

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
	return m, nil
}

// collectLatencyMetrics collects the raw latency metrics, writing the data
// into the given instrumentation library metrics.
func collectLatencyMetrics(ilm pmetric.ScopeMetrics,
	metricKeyToDimensions *cache.Cache[metricKey, pcommon.Map],
	histograms map[metricKey]*cache.Histogram,
	prefix string) error {
	if len(histograms) == 0 {
		return nil
	}

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

		countTotal := uint64(0)
		hist.VisitNonZeroBuckets(func(vmrange string, count uint64) {
			dpLatency := dps.AppendEmpty()
			dpLatency.SetTimestamp(timestamp)
			dpLatency.SetIntValue(int64(count))

			dpLatency.Attributes().EnsureCapacity(dimensions.Len() + 1)
			dimensions.CopyTo(dpLatency.Attributes())
			dpLatency.Attributes().PutStr("vmrange", vmrange)
			countTotal += count
		})

		if countTotal > 0 {
			dpLatencyCount := countDps.AppendEmpty()
			dpLatencyCount.SetTimestamp(timestamp)
			dpLatencyCount.SetIntValue(int64(countTotal))
			dimensions.CopyTo(dpLatencyCount.Attributes())

			dpLatencySum := sumDps.AppendEmpty()
			dpLatencySum.SetTimestamp(timestamp)
			dpLatencySum.SetIntValue(int64(hist.GetSum()))
			dimensions.CopyTo(dpLatencySum.Attributes())
		}
	}
	return nil
}

// aggregateMetrics aggregates the raw metrics from the input trace data.
// Each metric is identified by a key that is built from the service name
// and span metadata such as operation, kind, status_code and any additional
// dimensions the user has configured.
func (p *connectorImp) aggregateMetrics(traces ptrace.Traces) {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		rspans := traces.ResourceSpans().At(i)
		resourceAttr := rspans.Resource().Attributes()
		pidIntValue := fillproc.GetPid(resourceAttr)
		if pidIntValue <= 0 {
			// 必须存在PID
			continue
		}

		serviceAttr, ok := resourceAttr.Get(conventions.AttributeServiceName)
		if !ok {
			// 必须存在服务名
			continue
		}
		pid := strconv.FormatInt(pidIntValue, 10)
		containerId := fillproc.GetContainerId(resourceAttr)
		if len(containerId) > 12 {
			containerId = containerId[:12]
		}
		serviceName := serviceAttr.Str()
		ilsSlice := rspans.ScopeSpans()
		for j := 0; j < ilsSlice.Len(); j++ {
			ils := ilsSlice.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				p.aggregateMetricsForSpan(pid, containerId, serviceName, span)
			}
		}
	}
}

func (p *connectorImp) aggregateMetricsForSpan(pid string, containerId string, serviceName string, span ptrace.Span) {
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
			// Always reset the buffer before re-using.
			p.keyValue.reset()
			key := metricKey(p.buildKey(pid, containerId, serviceName, span))
			if _, has := p.metricKeyToDimensions.Get(key); !has {
				p.metricKeyToDimensions.Add(key, p.keyValue.GetMap())
			}
			updateHistogram(p.serverHistograms, key, latencyInNanoseconds)
		}
	}

	if p.config.DbEnabled && span.Kind() != ptrace.SpanKindServer {
		spanAttr := span.Attributes()
		dbSystemAttr, systemExist := spanAttr.Get(conventions.AttributeDBSystem)
		dbOperateAttr, operateExist := spanAttr.Get(conventions.AttributeDBOperation)
		dbTableAttr, tableExist := spanAttr.Get(conventions.AttributeDBSQLTable)
		dbUrlAttr, urlExist := spanAttr.Get(conventions.AttributeDBConnectionString)
		if !tableExist || !operateExist {
			if dbStatement, sqlExist := spanAttr.Get(conventions.AttributeDBStatement); sqlExist {
				operation, table := sqlprune.SQLParseOperationAndTableNEW(dbStatement.Str())
				if operation != "" {
					if !operateExist {
						operateExist = true
						dbOperateAttr = pcommon.NewValueStr(operation)
					}
					if !tableExist {
						tableExist = true
						dbTableAttr = pcommon.NewValueStr(table)
					}
				}
			}
		}

		if systemExist && operateExist && tableExist && urlExist {
			p.keyValue.reset()
			dbName := getAttrValueWithDefault(spanAttr, conventions.AttributeDBName, "")
			dbError := span.Status().Code() == ptrace.StatusCodeError
			dbCallKey := metricKey(p.buildDbKey(pid, containerId, serviceName, dbSystemAttr.Str(), dbName, dbOperateAttr.Str(), dbTableAttr.Str(), dbUrlAttr.Str(), dbError))
			if _, has := p.dbCallMetricKeyToDimensions.Get(dbCallKey); !has {
				p.dbCallMetricKeyToDimensions.Add(dbCallKey, p.keyValue.GetMap())
			}
			updateHistogram(p.dbCallHistograms, dbCallKey, latencyInNanoseconds)
		}
	}
}

// buildKey Server Red指标
func (p *connectorImp) buildKey(pid string, containerId string, serviceName string, span ptrace.Span) string {
	spanName := span.Name()
	if len(p.serviceToOperations) > p.maxNumberOfServicesToTrack {
		p.logger.Warn("Too many services to track, using overflow service name", zap.Int("maxNumberOfServicesToTrack", p.maxNumberOfServicesToTrack))
		serviceName = overflowServiceName
	}
	if len(p.serviceToOperations[serviceName]) > p.maxNumberOfOperationsToTrackPerService {
		p.logger.Warn("Too many operations to track, using overflow operation name", zap.Int("maxNumberOfOperationsToTrackPerService", p.maxNumberOfOperationsToTrackPerService))
		spanName = overflowOperation
	}

	p.keyValue.
		Add(keyServiceName, serviceName).
		Add(keyContentKey, spanName).
		Add(keyNodeName, p.nodeName).
		Add(keyNodeIp, p.nodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyTopSpan, strconv.FormatBool(span.ParentSpanID().IsEmpty())).
		Add(keyIsError, strconv.FormatBool(span.Status().Code() == ptrace.StatusCodeError))

	if _, ok := p.serviceToOperations[serviceName]; !ok {
		p.serviceToOperations[serviceName] = make(map[string]struct{})
	}
	p.serviceToOperations[serviceName][spanName] = struct{}{}

	return p.keyValue.GetValue()
}

// buildDbKey DB Red指标
func (p *connectorImp) buildDbKey(pid string, containerId string, serviceName string, dbSystem string, dbName string, dbOperate string, dbTable string, dbUrl string, isError bool) string {
	p.keyValue.
		Add(keyServiceName, serviceName).
		Add(keyNodeName, p.nodeName).
		Add(keyNodeIp, p.nodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, fmt.Sprintf("%s %s", dbOperate, dbTable)).
		Add(keyDbSystem, dbSystem).
		Add(keyDbName, dbName).
		Add(keyDbUrl, dbUrl).
		Add(keyIsError, strconv.FormatBool(isError))
	return p.keyValue.GetValue()
}

func getAttrValueWithDefault(attr pcommon.Map, key string, defaultValue string) string {
	// The more specific span attribute should take precedence.
	attrValue, exists := attr.Get(key)
	if exists {
		return attrValue.AsString()
	}
	return defaultValue
}

// updateHistogram adds the histogram sample to the histogram defined by the metric key.
func updateHistogram(histograms map[metricKey]*cache.Histogram, key metricKey, latency float64) {
	histo, ok := histograms[key]
	if !ok {
		histo = cache.NewHistogram()
		histograms[key] = histo
	}
	histo.Update(latency)
}

type ReusedKeyValue struct {
	maxAttribute int
	keys         []string
	vals         []string
	size         int
	dest         *bytes.Buffer
}

func newKeyValue(maxAttribute int) *ReusedKeyValue {
	return &ReusedKeyValue{
		maxAttribute: maxAttribute,
		keys:         make([]string, maxAttribute),
		vals:         make([]string, maxAttribute),
		dest:         bytes.NewBuffer(make([]byte, 0, 1024)),
	}
}

func (kv *ReusedKeyValue) reset() {
	kv.size = 0
}

func (kv *ReusedKeyValue) Add(key string, value string) *ReusedKeyValue {
	if kv.size < kv.maxAttribute {
		kv.keys[kv.size] = key
		kv.vals[kv.size] = value

		kv.size++
	}

	return kv
}

func (kv *ReusedKeyValue) GetValue() string {
	kv.dest.Reset()
	for i := 0; i < kv.size; i++ {
		if i > 0 {
			kv.dest.WriteString(metricKeySeparator)
		}
		kv.dest.WriteString(kv.vals[i])
	}
	return kv.dest.String()
}

func (kv *ReusedKeyValue) GetMap() pcommon.Map {
	dims := pcommon.NewMap()
	for i := 0; i < kv.size; i++ {
		dims.PutStr(kv.keys[i], kv.vals[i])
	}
	return dims
}

func getNodeName() string {
	// 从环境变量获取NodeName
	if nodeNameFromEnv, exist := os.LookupEnv(envNodeName); exist {
		return nodeNameFromEnv
	}
	// 使用主机名作为NodeName
	if hostName, err := os.Hostname(); err == nil {
		return hostName
	}
	return "Unknown"
}

func getNodeIp() string {
	// 从环境变量获取NodeIP
	if nodeIpFromEnv, exist := os.LookupEnv(envNodeIP); exist {
		return nodeIpFromEnv
	}
	return "Unknown"
}
