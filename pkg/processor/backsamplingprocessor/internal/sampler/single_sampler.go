package sampler

import "github.com/CloudDetail/apo-module/model/v1"

type SingleSampler struct {
	normalSampler      NoramlSampler
	openNormalSampling bool
	openSlowSampling   bool
	openErrorSampling  bool
	ignoreThreshold    uint64
	silent             *Silent
}

func NewSingleSampler(slowSampling bool, errorSampling bool, ignoreThreshold uint64) *SingleSampler {
	return &SingleSampler{
		openSlowSampling:  slowSampling,
		openErrorSampling: errorSampling,
		ignoreThreshold:   ignoreThreshold,
	}
}

func (c *SingleSampler) WithNormalSampler(normalSampleWaitTime int64) *SingleSampler {
	c.normalSampler = NewTopNormalTraceSampler(normalSampleWaitTime)
	c.openNormalSampling = normalSampleWaitTime > 0
	return c
}

func (c *SingleSampler) WithSilent(silentCount int, silentPeriod int64, silentMode string) *SingleSampler {
	c.silent = NewSilent(silentCount, silentPeriod, silentMode)
	return c
}

func (c *SingleSampler) Sample(trace *model.TraceLabels) bool {
	if !c.openNormalSampling && !c.openSlowSampling && !c.openErrorSampling {
		return false
	}
	if !trace.IsError && !trace.TopSpan && trace.Duration < c.ignoreThreshold {
		return false
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
		if c.openNormalSampling && trace.IsSampled {
			isSampled = c.normalSampler.Sample(trace)
		}
	}

	if isSampled {
		trace.IsProfiled = true
		return true
	}
	return false
}

func (c *SingleSampler) CheckWindow(now int64) {
	c.silent.CheckAndResetSilent(now)
	c.normalSampler.Check(now)
}
