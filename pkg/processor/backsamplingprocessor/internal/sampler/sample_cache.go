package sampler

import (
	"sync"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
	grpc_model "github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/model"
)

type SampleCache struct {
	normalTopSampling  bool
	normalSampler      NoramlSampler
	tailbaseSamplers   *TailbaseSamplers
	openNormalSampling bool
	openSlowSampling   bool
	openErrorSampling  bool
	ignoreThreshold    uint64
	slowThresholdCache *ContainerSlowThreshold
	silent             *Silent
}

func NewSampleCache(slowSampling bool, errorSampling bool, ignoreThreshold uint64) *SampleCache {
	return &SampleCache{
		openSlowSampling:  slowSampling,
		openErrorSampling: errorSampling,
		ignoreThreshold:   ignoreThreshold,
	}
}

func (c *SampleCache) WithNormalSampler(normalTopSample bool, normalSampleWaitTime int64, logEnable bool) *SampleCache {
	c.normalTopSampling = normalTopSample
	c.normalSampler = newNormalSampler(normalTopSample, normalSampleWaitTime, logEnable)
	c.openNormalSampling = normalSampleWaitTime > 0
	return c
}

func (c *SampleCache) WithTailBaseSampler(retryNum int, sampleWaitTime int, logEnable bool) *SampleCache {
	c.tailbaseSamplers = newTailbaseSamplers(sampleWaitTime, retryNum, logEnable)
	c.slowThresholdCache = newContainerSlowThreshold()
	return c
}

func (c *SampleCache) WithSilent(silentCount int, silentPeriod int64, silentMode string) *SampleCache {
	c.silent = NewSilent(silentCount, silentPeriod, silentMode)
	return c
}

func (c *SampleCache) GetSampleResult(trace *model.TraceLabels) SampleResult {
	c.CheckSlow(trace)

	if !c.openNormalSampling && !c.openSlowSampling && !c.openErrorSampling {
		return SampleResultNoOp
	}

	if !trace.IsError && !trace.TopSpan && trace.Duration < c.ignoreThreshold {

		return SampleResultNoOp
	}

	hasSlow := false
	hasError := false
	isSampled := false
	if trace.IsSlow || trace.IsError {
		hasSlow = c.openSlowSampling && trace.IsSlow
		hasError = c.openErrorSampling && trace.IsError
		if (hasSlow || hasError) && trace.IsSampled && !c.silent.IsSilent(getPidUrl(trace)) {
			isSampled = true
		}
	} else {
		if c.openNormalSampling && trace.IsSampled && (!c.normalTopSampling || trace.TopSpan) {
			isSampled = c.normalSampler.Sample(trace)
			if isSampled {
				c.normalSampler.UpdateTime(trace)
			}
		}
	}

	result := c.tailbaseSamplers.Sample(trace, hasSlow, hasError, isSampled)
	if c.openNormalSampling && !c.normalTopSampling {
		if result == SampleResultTrace || result == SampleResultTraceProfile || result == SampleResultTraceLog {
			c.normalSampler.UpdateTime(trace)
		}
	}
	return result
}

func (c *SampleCache) CacheTrace(trace *model.TraceLabels) {
	c.tailbaseSamplers.traceCache.Write(trace.TraceId, trace)
}

func (c *SampleCache) UpdateSampledTraceIds(result *grpc_model.ProfileResult, slowCount int, errorCount int, normalCount int) {
	c.tailbaseSamplers.slowSampler.updateSampledTraceIds(result.SlowTraceIds, slowCount)
	c.tailbaseSamplers.errorSampler.updateSampledTraceIds(result.ErrorTraceIds, errorCount)
	c.tailbaseSamplers.normalSampler.updateSampledTraceIds(result.NormalTraceIds, normalCount)
}

func (c *SampleCache) UpdateUnSentCount(slowCount int, errorCount int, normalCount int) {
	c.tailbaseSamplers.slowSampler.updateUnSentCount(slowCount)
	c.tailbaseSamplers.errorSampler.updateUnSentCount(errorCount)
	c.tailbaseSamplers.normalSampler.updateUnSentCount(normalCount)
}

func (c *SampleCache) CheckSlow(trace *model.TraceLabels) {
	threshold := c.slowThresholdCache.getSlowThreshold(trace.Url)
	trace.ThresholdRange = threshold.thresholdRange
	trace.ThresholdType = threshold.thresholdType
	trace.ThresholdValue = threshold.thresholdValue
	trace.ThresholdMultiple = threshold.thresholdMultiple

	trace.IsSlow = trace.Duration >= uint64(threshold.thresholdValue)
	if trace.Duration < c.ignoreThreshold {
		trace.IsSlow = false
	}
}

func (c *SampleCache) UpdateSlowThreshold(datas []*grpc_model.SlowThresholdData) {
	if len(datas) == 0 {
		return
	}
	now := time.Now().Unix()
	for _, data := range datas {
		c.slowThresholdCache.updateSlowThreshold(data, now)
	}
}

func (c *SampleCache) UpdateNoramlTime(trace *model.TraceLabels) {
	c.normalSampler.UpdateTime(trace)
}

func (c *SampleCache) CheckWindow(now int64) {
	c.silent.CheckAndResetSilent(now)
	c.normalSampler.Check(now)
}

func (c *SampleCache) GetSampledTraceIds() ([]string, []string, []string) {
	return c.tailbaseSamplers.GetSampledTraceIds()
}

func (c *SampleCache) GetAndCleanTailbaseTraces() *SampledTraceIds {
	return &SampledTraceIds{
		TraceCache: c.tailbaseSamplers.traceCache.GetAndReset(),
		ErrorIds:   c.tailbaseSamplers.errorSampler.cleanSampledIds(),
		SlowIds:    c.tailbaseSamplers.slowSampler.cleanSampledIds(),
		NormalIds:  c.tailbaseSamplers.normalSampler.cleanSampledIds(),
	}
}

type SampledTraceIds struct {
	TraceCache map[string][]*model.TraceLabels
	ErrorIds   []string
	SlowIds    []string
	NormalIds  []string
}

type SlowThreshold struct {
	thresholdType     model.ThresholdType
	thresholdRange    model.ThresholdRange
	thresholdValue    float64
	thresholdMultiple float64
}

type ContainerSlowThreshold struct {
	updateTime      int64
	thresholdValues sync.Map
}

func newContainerSlowThreshold() *ContainerSlowThreshold {
	return &ContainerSlowThreshold{
		updateTime: time.Now().Unix(),
	}
}

func (threshold *ContainerSlowThreshold) updateSlowThreshold(data *grpc_model.SlowThresholdData, time int64) {
	if data.Value > 0.0 {
		threshold.thresholdValues.Store(data.Url, &SlowThreshold{
			thresholdType:     model.ThresholdType(data.Type),
			thresholdRange:    model.ThresholdRange(data.Range),
			thresholdValue:    data.Value,
			thresholdMultiple: data.Multiple,
		})
		threshold.updateTime = time
	}
}

func (threshold *ContainerSlowThreshold) getSlowThreshold(url string) *SlowThreshold {
	if dataInterface, ok := threshold.thresholdValues.Load(url); ok {
		return dataInterface.(*SlowThreshold)
	} else {
		return &SlowThreshold{
			thresholdType:     model.P90ThresholdType,
			thresholdRange:    model.ThresholdRange("default"),
			thresholdValue:    500.0 * 1e6,
			thresholdMultiple: 1.0,
		}
	}
}
