package sampler

import (
	"log"
	"sync"

	"github.com/CloudDetail/apo-module/model/v1"
)

type TailbaseSampler struct {
	name            string
	unSendIds       *unSendIds
	sampledTraceIds sync.Map
	traceHoldTime   int
	logEnable       bool
}

func newTailbaseSampler(name string, traceHoldTime int, retryNum int, logEnable bool) *TailbaseSampler {
	return &TailbaseSampler{
		name:          name,
		unSendIds:     newUnSendIds(retryNum),
		traceHoldTime: traceHoldTime,
		logEnable:     logEnable,
	}
}

func (sampler *TailbaseSampler) isTailBaseSampled(trace *model.TraceLabels) bool {
	if _, found := sampler.sampledTraceIds.Load(trace.TraceId); found {
		if sampler.logEnable {
			log.Printf("[%s] %s is stored by %s tailBase.", trace.ServiceName, trace.TraceId, sampler.name)
		}
		return true
	}
	return false
}

func (sampler *TailbaseSampler) notifyTailbase(trace *model.TraceLabels) {
	trace.IsProfiled = true
	if sampler.logEnable {
		log.Printf("[%s] %s is %s sampled", trace.ServiceName, trace.TraceId, sampler.name)
	}
	if _, found := sampler.sampledTraceIds.Load(trace.TraceId); !found {
		sampler.sampledTraceIds.Store(trace.TraceId, &traceIdHoldTime{count: 0})
		// Record sampled traceIds and send them to receiver per second.
		sampler.unSendIds.cacheIds(trace.TraceId)
	}
}

func (sampler *TailbaseSampler) cleanSampledIds() []string {
	result := make([]string, 0)

	// Delete expired traceIds.
	sampler.sampledTraceIds.Range(func(k, v interface{}) bool {
		value := v.(*traceIdHoldTime)
		if value.isExpired(sampler.traceHoldTime) {
			sampler.sampledTraceIds.Delete(k)
		} else {
			if value.checkSampled() {
				result = append(result, k.(string))
			}
		}
		return true
	})
	return result
}

func (sampler *TailbaseSampler) updateSampledTraceIds(traceIds []string, count int) {
	if count > 0 {
		sampler.unSendIds.markSent(count)
	}
	for _, traceId := range traceIds {
		sampler.sampledTraceIds.Store(traceId, &traceIdHoldTime{count: 0})
	}
}

func (sampler *TailbaseSampler) updateUnSentCount(count int) {
	if count > 0 {
		sampler.unSendIds.markUnSent(count)
	}
}

type traceIdHoldTime struct {
	count int
}

func (t *traceIdHoldTime) isExpired(holdTime int) bool {
	return t.count >= holdTime
}

func (t *traceIdHoldTime) checkSampled() bool {
	t.count++
	return t.count <= 2
}

type unSendIds struct {
	lock      sync.RWMutex
	toSendIds []string
	retry     *RetryList
}

func newUnSendIds(retryNum int) *unSendIds {
	return &unSendIds{
		toSendIds: make([]string, 0),
		retry:     newRetryList(retryNum),
	}
}

func (unSend *unSendIds) cacheIds(id string) {
	unSend.lock.Lock()
	defer unSend.lock.Unlock()

	unSend.toSendIds = append(unSend.toSendIds, id)
}

func (unSend *unSendIds) markUnSent(num int) {
	if num <= 0 {
		return
	}

	unSend.lock.Lock()
	defer unSend.lock.Unlock()

	toRemoveSize := num
	if unSend.retry != nil {
		toRemoveSize = unSend.retry.getToRemoveSize(num)
	}
	if toRemoveSize > 0 {
		unSend.toSendIds = unSend.toSendIds[toRemoveSize:]
	}
}

func (unSend *unSendIds) markSent(num int) {
	unSend.lock.Lock()
	defer unSend.lock.Unlock()

	unSend.toSendIds = unSend.toSendIds[num:]

	if unSend.retry != nil {
		// Reset count
		unSend.retry.reset()
	}
}

func (unSend *unSendIds) GetToSendIds() []string {
	size := len(unSend.toSendIds)
	return unSend.toSendIds[0:size]
}

type RetryList struct {
	counts    []int // Record count per seconds
	fromIndex int
	lastNum   int // Record how many datas are not sent last time.
	size      int // Record how many datas are sent
}

func newRetryList(retryNum int) *RetryList {
	if retryNum > 0 {
		return &RetryList{
			counts:    make([]int, retryNum),
			fromIndex: 0,
			lastNum:   0,
			size:      0,
		}
	} else {
		return nil
	}
}

func (retry *RetryList) getToRemoveSize(num int) int {
	if retry.size < len(retry.counts) {
		index := retry.fromIndex + retry.size
		if index >= len(retry.counts) {
			index -= len(retry.counts)
		}
		retry.counts[index] = num - retry.lastNum
		retry.size++
		retry.lastNum = num

		return 0
	} else {
		toRemoveSize := retry.counts[retry.fromIndex]
		retry.counts[retry.fromIndex] = num - retry.lastNum
		// Clean the oldest record, add new recrod to make cache store N seconds.
		retry.counts = append(retry.counts[1:], num-retry.lastNum)
		retry.lastNum = num - toRemoveSize
		if retry.fromIndex == len(retry.counts)-1 {
			retry.fromIndex = 0
		} else {
			retry.fromIndex++
		}
		return toRemoveSize
	}
}

func (retry *RetryList) reset() {
	retry.size = 0
	retry.lastNum = 0
}
