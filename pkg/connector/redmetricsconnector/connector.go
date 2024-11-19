package redmetricsconnector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	keyAddress  = "address"
	keyRole     = "role"

	AttributeSkywalkingSpanID = "sw8.span_id"

	AttributeHttpMethod        = "http.method"         // 1.x
	AttributeHttpRequestMethod = "http.request.method" // 2.x

	AttributeNetPeerName   = "net.peer.name"  // 1.x
	AttributeNetPeerPort   = "net.peer.port"  // 1.x
	AttributeServerAddress = "server.address" // 2.x
	AttributeServerPort    = "server.port"    // 2.x

	AttributeNetSockPeerAddr    = "net.sock.peer.addr"   // 1.x
	AttributeNetSockPeerPort    = "net.sock.peer.port"   // 1.x
	AttributeNetworkPeerAddress = "network.peer.address" // 2.x
	AttributeNetworkPeerPort    = "network.peer.port"    // 2.x
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

	// The starting time of the data points.
	startTimestamp pcommon.Timestamp

	// Histogram.
	serverHistograms       map[metricKey]cache.Histogram // 服务 指标
	dbCallHistograms       map[metricKey]cache.Histogram // db 指标
	externalCallHistograms map[metricKey]cache.Histogram // external 指标
	mqCallHistograms       map[metricKey]cache.Histogram // mq 指标
	bounds                 []float64

	keyValue *ReusedKeyValue

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

	return &connectorImp{
		logger:                                 logger,
		config:                                 *pConfig,
		nodeName:                               getNodeName(),
		nodeIp:                                 getNodeIp(),
		startTimestamp:                         pcommon.NewTimestampFromTime(time.Now()),
		serverHistograms:                       make(map[metricKey]cache.Histogram),
		dbCallHistograms:                       make(map[metricKey]cache.Histogram),
		externalCallHistograms:                 make(map[metricKey]cache.Histogram),
		mqCallHistograms:                       make(map[metricKey]cache.Histogram),
		bounds:                                 bounds,
		keyValue:                               newKeyValue(100), // 最大100的KeyValue 可重用Map
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
	}, nil
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
			p.updateHistogram(p.serverHistograms, key, latencyInNanoseconds)
		}
	}

	spanAttr := span.Attributes()
	if span.Kind() == ptrace.SpanKindClient {
		if p.config.DbEnabled {
			if dbSystemAttr, systemExist := spanAttr.Get(conventions.AttributeDBSystem); systemExist {
				dbSystem := dbSystemAttr.Str()
				name := ""
				if dbSystem == "redis" || dbSystem == "memcached" || dbSystem == "aerospike" {
					name = span.Name()
				} else {
					dbOperateAttr, operateExist := spanAttr.Get(conventions.AttributeDBOperation)
					dbTableAttr, tableExist := spanAttr.Get(conventions.AttributeDBSQLTable)
					if !tableExist || !operateExist {
						if dbStatement, sqlExist := spanAttr.Get(conventions.AttributeDBStatement); sqlExist {
							operation, table := sqlprune.SQLParseOperationAndTableNEW(dbStatement.Str())
							if operation != "" {
								dbOperateAttr, operateExist = pcommon.NewValueStr(operation), true
								dbTableAttr, tableExist = pcommon.NewValueStr(table), true
							} else {
								p.logger.Info("Drop SQL by parse failed",
									zap.String("span", span.Name()),
									zap.String("type", dbSystem),
									zap.String("sql", dbStatement.Str()),
								)
							}
						}
					}
					if tableExist && operateExist {
						name = fmt.Sprintf("%s %s", dbOperateAttr.Str(), dbTableAttr.Str())
					} else {
						name = "unknown"
					}
				}

				p.keyValue.reset()
				dbAddress := getClientPeer(spanAttr, dbSystem, "unknown")
				dbName := getAttrValueWithDefault(spanAttr, conventions.AttributeDBName, "")
				dbError := span.Status().Code() == ptrace.StatusCodeError
				dbCallKey := metricKey(p.buildDbKey(pid, containerId, serviceName, dbSystem, dbName, name, dbAddress, dbError))
				if _, has := p.dbCallMetricKeyToDimensions.Get(dbCallKey); !has {
					p.dbCallMetricKeyToDimensions.Add(dbCallKey, p.keyValue.GetMap())
				}
				p.updateHistogram(p.dbCallHistograms, dbCallKey, latencyInNanoseconds)
			}
		}

		if p.config.ExternalEnabled {
			if httpMethod := getHttpMethod(spanAttr); httpMethod != "" {
				p.keyValue.reset()
				httpAddress := getClientPeer(spanAttr, "http", "unknown")
				httpError := span.Status().Code() == ptrace.StatusCodeError
				httpCallKey := metricKey(p.buildExternalKey(pid, containerId, serviceName, httpMethod, httpAddress, httpError))
				if _, has := p.externalCallMetricKeyToDimensions.Get(httpCallKey); !has {
					p.externalCallMetricKeyToDimensions.Add(httpCallKey, p.keyValue.GetMap())
				}
				p.updateHistogram(p.externalCallHistograms, httpCallKey, latencyInNanoseconds)
			} else if rpcSystemAttr, systemExist := spanAttr.Get(conventions.AttributeRPCSystem); systemExist {
				p.keyValue.reset()
				protocol := rpcSystemAttr.Str()
				// apache_dubbo => dubbo
				if index := strings.LastIndex(protocol, "_"); index != -1 {
					protocol = protocol[index+1:]
				}
				rpcAddress := getClientPeer(spanAttr, protocol, "unknown")
				rpcError := span.Status().Code() == ptrace.StatusCodeError
				rpcCallKey := metricKey(p.buildExternalKey(pid, containerId, serviceName, span.Name(), rpcAddress, rpcError))
				if _, has := p.externalCallMetricKeyToDimensions.Get(rpcCallKey); !has {
					p.externalCallMetricKeyToDimensions.Add(rpcCallKey, p.keyValue.GetMap())
				}
				p.updateHistogram(p.externalCallHistograms, rpcCallKey, latencyInNanoseconds)
			} else {
				_, dbSystemExist := spanAttr.Get(conventions.AttributeDBSystem)
				_, mqSystemExist := spanAttr.Get(conventions.AttributeMessagingSystem)
				if !dbSystemExist && !mqSystemExist {
					p.keyValue.reset()
					unknownAddress := getClientPeer(spanAttr, "unknown", "unknown")
					unknownError := span.Status().Code() == ptrace.StatusCodeError
					unknonwCallKey := metricKey(p.buildExternalKey(pid, containerId, serviceName, span.Name(), unknownAddress, unknownError))
					if _, has := p.externalCallMetricKeyToDimensions.Get(unknonwCallKey); !has {
						p.externalCallMetricKeyToDimensions.Add(unknonwCallKey, p.keyValue.GetMap())
					}
					p.updateHistogram(p.externalCallHistograms, unknonwCallKey, latencyInNanoseconds)
				}
			}
		}
	}

	if p.config.MqEnabled && (span.Kind() == ptrace.SpanKindClient || span.Kind() == ptrace.SpanKindProducer || span.Kind() == ptrace.SpanKindConsumer) {
		if mqSystemAttr, systemExist := spanAttr.Get(conventions.AttributeMessagingSystem); systemExist {
			p.keyValue.reset()
			mqAddress := getClientPeer(spanAttr, mqSystemAttr.Str(), mqSystemAttr.Str())
			mqError := span.Status().Code() == ptrace.StatusCodeError
			mqRole := strings.ToLower(span.Kind().String())
			mqCallKey := metricKey(p.buildMqKey(pid, containerId, serviceName, span.Name(), mqAddress, mqError, mqRole))
			if _, has := p.mqCallMetricKeyToDimensions.Get(mqCallKey); !has {
				p.mqCallMetricKeyToDimensions.Add(mqCallKey, p.keyValue.GetMap())
			}
			p.updateHistogram(p.mqCallHistograms, mqCallKey, latencyInNanoseconds)
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

// buildExternalKey Http/Rpc Red指标
func (p *connectorImp) buildExternalKey(pid string, containerId string, serviceName string, name string, peer string, isError bool) string {
	p.keyValue.
		Add(keyServiceName, serviceName).
		Add(keyNodeName, p.nodeName).
		Add(keyNodeIp, p.nodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
		Add(keyAddress, peer).
		Add(keyIsError, strconv.FormatBool(isError))
	return p.keyValue.GetValue()
}

// buildMqKey ActiveMq / RabbitMq / RocketMq / Kafka Red指标
func (p *connectorImp) buildMqKey(pid string, containerId string, serviceName string, name string, address string, isError bool, role string) string {
	p.keyValue.
		Add(keyServiceName, serviceName).
		Add(keyNodeName, p.nodeName).
		Add(keyNodeIp, p.nodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
		Add(keyAddress, address).
		Add(keyRole, role).
		Add(keyIsError, strconv.FormatBool(isError))
	return p.keyValue.GetValue()
}

// buildDbKey DB Red指标
func (p *connectorImp) buildDbKey(pid string, containerId string, serviceName string, dbSystem string, dbName string, name string, dbUrl string, isError bool) string {
	p.keyValue.
		Add(keyServiceName, serviceName).
		Add(keyNodeName, p.nodeName).
		Add(keyNodeIp, p.nodeIp).
		Add(keyPid, pid).
		Add(keyContainerId, containerId).
		Add(keyName, name).
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

func getHttpMethod(attr pcommon.Map) string {
	// 1.x http.method
	if httpMethod, found := attr.Get(AttributeHttpMethod); found {
		return httpMethod.Str()
	}
	// 2.x http.request.method
	if httpRequestMethod, found := attr.Get(AttributeHttpRequestMethod); found {
		return httpRequestMethod.Str()
	}
	return ""
}

func getClientPeer(attr pcommon.Map, protocol string, defaultValue string) string {
	// 1.x - redis、grpc、rabbitmq
	if netSockPeerAddr, addrFound := attr.Get(AttributeNetSockPeerAddr); addrFound {
		if netSockPeerPort, portFound := attr.Get(AttributeNetSockPeerPort); portFound {
			return fmt.Sprintf("%s://%s:%s", protocol, netSockPeerAddr.Str(), netSockPeerPort.AsString())
		} else {
			return fmt.Sprintf("%s://%s", protocol, netSockPeerAddr.Str())
		}
	}
	// 2.x - redis、grpc、rabbitmq
	if networkPeerAddress, addrFound := attr.Get(AttributeNetworkPeerAddress); addrFound {
		if networkPeerPort, portFound := attr.Get(AttributeNetworkPeerPort); portFound {
			return fmt.Sprintf("%s://%s:%s", protocol, networkPeerAddress.Str(), networkPeerPort.AsString())
		} else {
			return fmt.Sprintf("%s://%s", protocol, networkPeerAddress.Str())
		}
	}

	// 1.x - httpclient、db、dubbo
	if peerName, peerFound := attr.Get(AttributeNetPeerName); peerFound {
		if peerPort, peerPortFound := attr.Get(AttributeNetPeerPort); peerPortFound {
			return fmt.Sprintf("%s://%s:%s", protocol, peerName.Str(), peerPort.AsString())
		} else {
			return fmt.Sprintf("%s://%s", protocol, peerName.Str())
		}
	}
	// 2.x - httpclient、db、dubbo
	if serverAddress, serverFound := attr.Get(AttributeServerAddress); serverFound {
		if serverPort, serverPortFound := attr.Get(AttributeServerPort); serverPortFound {
			return fmt.Sprintf("%s://%s:%s", protocol, serverAddress.Str(), serverPort.AsString())
		} else {
			return fmt.Sprintf("%s://%s", protocol, serverAddress.Str())
		}
	}

	return defaultValue
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
