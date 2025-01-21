package notify

import (
	"strconv"

	"github.com/CloudDetail/apo-module/model/v1"
)

type Signal struct {
	PidStr         string `json:"pid"`
	StartTS        uint32 `json:"start_ts"`
	EndTS          uint32 `json:"end_ts"`
	ContainerId    string `json:"container_id"`
	ApmSpanId      string `json:"span_id"`
	ApmServiceName string `json:"service_name"`
	ApmTraceId     string `json:"trace_id"`
	EndPoint       string `json:"endpoint"`
	SignalType     uint32 `json:"signal_type"`
}

func buildSignal(trace *model.TraceLabels) *Signal {
	// 0 - Normal, 1 - Slow, 2 - Error, 3 - Error & Slow
	var signalType uint32 = 0
	if trace.IsSlow {
		signalType |= 1
	}
	if trace.IsError {
		signalType |= 2
	}

	return &Signal{
		PidStr:         strconv.FormatInt(int64(trace.Pid), 10),
		StartTS:        uint32(trace.StartTime / 1000000000),
		EndTS:          uint32(trace.EndTime / 1000000000),
		ContainerId:    trace.ContainerId,
		ApmSpanId:      trace.ApmSpanId,
		ApmServiceName: trace.ServiceName,
		ApmTraceId:     trace.TraceId,
		EndPoint:       trace.Url,
		SignalType:     signalType,
	}
}
