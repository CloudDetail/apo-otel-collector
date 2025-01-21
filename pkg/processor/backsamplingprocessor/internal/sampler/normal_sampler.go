package sampler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
)

type NoramlSampler interface {
	Sample(trace *model.TraceLabels) bool
	UpdateTime(trace *model.TraceLabels)
	Check(now int64)
}

func newNormalSampler(topSampler bool, nextTime int64, logEnable bool) NoramlSampler {
	if topSampler {
		return NewTopNormalTraceSampler(nextTime)
	} else {
		return newAnyNormalTraceSampler(nextTime, logEnable)
	}
}

type TopNormalTraceSampler struct {
	displayWindow    int64
	nextWindow       int64
	collectedPidUrls sync.Map
}

func NewTopNormalTraceSampler(displayWindow int64) *TopNormalTraceSampler {
	return &TopNormalTraceSampler{
		displayWindow: displayWindow,
		nextWindow:    0,
	}
}

func (sampler *TopNormalTraceSampler) Sample(trace *model.TraceLabels) bool {
	if sampler.displayWindow == 0 {
		return false
	}

	pidUrl := getPidUrl(trace)
	if _, exist := sampler.collectedPidUrls.Load(pidUrl); exist {
		return false
	}

	sampler.collectedPidUrls.Store(pidUrl, true)
	return true
}

func (sampler *TopNormalTraceSampler) UpdateTime(trace *model.TraceLabels) {}

func (sampler *TopNormalTraceSampler) Check(now int64) {
	if sampler.displayWindow == 0 {
		return
	}

	if sampler.nextWindow == 0 {
		sampler.nextWindow = sampler.displayWindow + now
	} else if sampler.nextWindow <= now {
		sampler.nextWindow = sampler.displayWindow + now
		sampler.collectedPidUrls.Range(func(k, v interface{}) bool {
			sampler.collectedPidUrls.Delete(k)
			return true
		})
	}
}

type AnyNormalTraceSampler struct {
	logEnable        bool
	waitTime         int64
	collectedPidUrls sync.Map
}

func newAnyNormalTraceSampler(waitTime int64, logEnable bool) *AnyNormalTraceSampler {
	return &AnyNormalTraceSampler{
		logEnable: logEnable,
		waitTime:  waitTime,
	}
}

func (sampler *AnyNormalTraceSampler) Sample(trace *model.TraceLabels) bool {
	if sampler.waitTime == 0 {
		return false
	}
	if _, exist := sampler.collectedPidUrls.Load(getPidUrl(trace)); exist {
		return false
	}
	return true
}

func (sampler *AnyNormalTraceSampler) UpdateTime(trace *model.TraceLabels) {
	if sampler.waitTime == 0 {
		return
	}
	sampler.collectedPidUrls.Store(getPidUrl(trace), time.Now().Unix()+sampler.waitTime)
}

func (sampler *AnyNormalTraceSampler) Check(now int64) {
	if sampler.waitTime == 0 {
		return
	}

	count := 0
	sampler.collectedPidUrls.Range(func(k, v interface{}) bool {
		checkTime := v.(int64)
		if checkTime < now {
			sampler.collectedPidUrls.Delete(k)
			count += 1
		}
		return true
	})
	if sampler.logEnable && count > 0 {
		log.Printf("Clean %d Expired Urls", count)
	}
}

func getPidUrl(trace *model.TraceLabels) string {
	return fmt.Sprintf("%d-%s", trace.Pid, trace.Url)
}
