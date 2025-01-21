package sampler

import (
	"sync"
	"time"
)

type SilentMode int

const (
	SilentWindow SilentMode = 0
	SilentWait   SilentMode = 1
)

type Silent struct {
	pidUrlDatas    sync.Map
	count          int
	period         int64
	mode           SilentMode
	nextWindowTime int64
}

func NewSilent(silentCount int, silentPeriod int64, silentMode string) *Silent {
	mode := SilentWindow
	if silentMode == "wait" {
		mode = SilentWait
	}

	return &Silent{
		count:  silentCount,
		period: silentPeriod,
		mode:   mode,
	}
}

func (silent *Silent) IsSilent(pidUrl string) bool {
	if silent.count == 0 {
		return true
	}
	if silent.period == 0 {
		return false
	}

	if silentDataInterface, exist := silent.pidUrlDatas.Load(pidUrl); exist {
		return silentDataInterface.(*SilentData).checkSilent()
	} else {
		silent.pidUrlDatas.Store(pidUrl, &SilentData{
			leftCount: silent.count,
		})
		return false
	}
}

func (silent *Silent) isNewWindow(duration int64, now int64) bool {
	if duration == 0 {
		return true
	}
	if silent.mode == SilentWait {
		return false
	}

	if silent.nextWindowTime == 0 {
		silent.nextWindowTime = now + duration
		return false
	}

	if silent.nextWindowTime <= now {
		silent.nextWindowTime = now + duration
		return true
	}
	return false
}

func (silent *Silent) CheckAndResetSilent(now int64) {
	isNewWindow := silent.isNewWindow(silent.period, now)

	silent.pidUrlDatas.Range(func(k, v interface{}) bool {
		silentData := v.(*SilentData)
		if silent.isExpire(silentData.silentTime, now, isNewWindow) {
			silent.pidUrlDatas.Delete(k)
		}
		return true
	})
}

func (silent *Silent) isExpire(time int64, now int64, isNewWindow bool) bool {
	if silent.period == 0 {
		return true
	}
	switch silent.mode {
	case SilentWait:
		return time > 0 && (time+silent.period) < now
	default:
		return isNewWindow
	}
}

type SilentData struct {
	leftCount  int
	silentTime int64
}

func (silent *SilentData) checkSilent() bool {
	if silent.leftCount == 0 {
		return true
	}
	if silent.leftCount == 1 {
		silent.leftCount = 0
		silent.silentTime = time.Now().Unix()
	} else {
		silent.leftCount--
		silent.silentTime = 0
	}
	return false
}
