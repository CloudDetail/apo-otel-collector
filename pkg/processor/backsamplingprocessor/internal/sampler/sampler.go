package sampler

import (
	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/cache"
)

type SampleResult int

const (
	SampleResultNoOp         SampleResult = 0
	SampleResultCache        SampleResult = 1 // Cache Trace
	SampleResultTrace        SampleResult = 2 // Slow TailBase <Trace>
	SampleResultTraceProfile SampleResult = 3 // Slow <Trace + Profile + Log>
	SampleResultTraceLog     SampleResult = 4 // Error <Trace + Log>
	SampleResultNormal       SampleResult = 5 // Normal <Trace + Profile + Log>
)

type TailbaseSamplers struct {
	traceCache    *cache.WriteableCache[*model.TraceLabels]
	slowSampler   *TailbaseSampler
	errorSampler  *TailbaseSampler
	normalSampler *TailbaseSampler
}

func newTailbaseSamplers(holdTime int, retryNum int, logEnable bool) *TailbaseSamplers {
	return &TailbaseSamplers{
		traceCache:    cache.NewWriteableCache[*model.TraceLabels](),
		slowSampler:   newTailbaseSampler("slow", holdTime, retryNum, logEnable),
		errorSampler:  newTailbaseSampler("error", holdTime, retryNum, logEnable),
		normalSampler: newTailbaseSampler("normal", holdTime, retryNum, logEnable),
	}
}

func (sampler *TailbaseSamplers) Sample(trace *model.TraceLabels, hasSlow bool, hasError bool, isSampled bool) SampleResult {
	if isSampled {
		if hasSlow && hasError {
			sampler.slowSampler.notifyTailbase(trace)
			sampler.errorSampler.notifyTailbase(trace)
			trace.ReportType = 4 // Slow && Error
			// Trace + Log + Cpu
			return SampleResultTraceProfile
		} else if hasSlow {
			sampler.slowSampler.notifyTailbase(trace)
			trace.ReportType = 1 // Slow
			return SampleResultTraceProfile
		} else if hasError {
			sampler.errorSampler.notifyTailbase(trace)
			trace.ReportType = 3 // Error
			return SampleResultTraceLog
		} else {
			sampler.normalSampler.notifyTailbase(trace)
			trace.ReportType = 2
			return SampleResultNormal
		}
	}

	if sampler.errorSampler.isTailBaseSampled(trace) {
		if hasError {
			return SampleResultTraceLog
		}
		return SampleResultTrace
	}

	if sampler.slowSampler.isTailBaseSampled(trace) || sampler.normalSampler.isTailBaseSampled(trace) {
		return SampleResultTrace
	}

	return SampleResultCache
}

func (sampler *TailbaseSamplers) GetSampledTraceIds() ([]string, []string, []string) {
	slowIds := sampler.slowSampler.unSendIds.GetToSendIds()
	errorIds := sampler.errorSampler.unSendIds.GetToSendIds()
	normalIds := sampler.normalSampler.unSendIds.GetToSendIds()

	return slowIds, errorIds, normalIds
}
