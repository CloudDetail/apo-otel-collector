package fillproc

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

const (
	KEY_PID         = "apo.pid"
	KEY_CONTAINERID = "apo.containerId"
)

func GetPid(resourceAttr pcommon.Map) int64 {
	if apoPidAttr, ok := resourceAttr.Get(KEY_PID); ok {
		return apoPidAttr.Int()
	}
	if otelPidAttr, ok := resourceAttr.Get(conventions.AttributeProcessPID); ok {
		return otelPidAttr.Int()
	}
	return 0
}

func GetContainerId(resourceAttr pcommon.Map) string {
	if apoContainerIdAttr, ok := resourceAttr.Get(KEY_CONTAINERID); ok {
		return apoContainerIdAttr.Str()
	}
	if otelContainerIdAttr, ok := resourceAttr.Get(conventions.AttributeContainerID); ok {
		return otelContainerIdAttr.Str()
	}
	return ""
}
