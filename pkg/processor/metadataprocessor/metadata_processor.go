package metadataprocessor

import (
	"context"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"

	"github.com/CloudDetail/metadata/model/cache"
	metasource "github.com/CloudDetail/metadata/source"
)

var _ processor.Metrics = &metadataProcessor{}

type metadataProcessor struct {
	cfg          *Config
	logger       *zap.Logger
	nextConsumer consumer.Metrics

	metaSource metasource.MetaSource
}

func newMetadataProcessor(set processor.Settings, nextConsumer consumer.Metrics, cfg *Config) (*metadataProcessor, error) {
	return &metadataProcessor{
		cfg:          cfg,
		logger:       set.Logger,
		nextConsumer: nextConsumer,
	}, nil
}

func (m *metadataProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

func (m *metadataProcessor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rs := rms.At(i)
		ilms := rs.ScopeMetrics()
		for j := 0; j < ilms.Len(); j++ {
			ils := ilms.At(j)
			metrics := ils.Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)
				if strings.HasPrefix(metric.Name(), m.cfg.MetricPrefix) {
					m.FillWithK8sMetadata(ctx, metric)
				}
			}
		}
	}
	return m.nextConsumer.ConsumeMetrics(ctx, md)
}

func (mp *metadataProcessor) FillWithK8sMetadata(ctx context.Context, metric pmetric.Metric) {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		dps := metric.Gauge().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			mp.fillPodInfo(dps.At(i).Attributes())
		}
	case pmetric.MetricTypeSum:
		dps := metric.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			mp.fillPodInfo(dps.At(i).Attributes())
		}
	case pmetric.MetricTypeHistogram:
		dps := metric.Histogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			mp.fillPodInfo(dps.At(i).Attributes())
		}
	case pmetric.MetricTypeExponentialHistogram:
		dps := metric.ExponentialHistogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			mp.fillPodInfo(dps.At(i).Attributes())
		}
	case pmetric.MetricTypeSummary:
		dps := metric.Summary().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			mp.fillPodInfo(dps.At(i).Attributes())
		}
	case pmetric.MetricTypeEmpty:
	}
}

func (*metadataProcessor) fillPodInfo(attrs pcommon.Map) {
	containerId, find := attrs.Get("container_id")
	if !find || containerId.Str() == "" {
		return
	}
	pod, find := cache.Querier.GetPodByContainerId("", containerId.Str())
	if !find {
		return
	}
	attrs.PutStr("pod", pod.Name)
	attrs.PutStr("namespace", pod.NS())
	attrs.PutStr("node", pod.NodeName())
	owners := pod.GetOwnerReferences(true)
	for index, owner := range owners {
		if index == 0 {
			attrs.PutStr("workload_kind", owner.Kind)
			attrs.PutStr("workload_name", owner.Name)
		} else {
			indexStr := strconv.Itoa(index)
			attrs.PutStr("workload_kind"+indexStr, owner.Kind)
			attrs.PutStr("workload_name"+indexStr, owner.Name)
		}
	}
}

func (m *metadataProcessor) Shutdown(ctx context.Context) error {
	if m.metaSource != nil {
		return m.metaSource.Stop()
	}
	return nil
}

// TODO 考虑以后扩展为extensions模式
// TODO 将metasource的log库输出接出到zap
func (m *metadataProcessor) Start(ctx context.Context, host component.Host) error {
	m.metaSource = metasource.CreateMetaSourceFromConfig(&m.cfg.MetaSourceConfig)
	return m.metaSource.Run()
}
