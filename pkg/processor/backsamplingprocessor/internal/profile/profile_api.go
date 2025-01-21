package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
	"go.uber.org/zap"
)

type ProfileSignal struct {
	TraceId   string `json:"traceId"`
	SpanId    string `json:"spanId"`
	StartTime uint64 `json:"start"`
	EndTime   uint64 `json:"end"`
	Flag      int    `json:"flag"`
	Retry     int    `json:"retry"`
}

type ebpfProfileApi struct {
	logger  *zap.Logger
	address string
	lock    sync.Mutex
	signals []*ProfileSignal
}

func (api *ebpfProfileApi) AddProfileSignal(trace *model.TraceLabels, flag int) {
	api.lock.Lock()
	defer api.lock.Unlock()

	api.signals = append(api.signals, &ProfileSignal{
		TraceId:   trace.TraceId,
		SpanId:    trace.ApmSpanId,
		StartTime: trace.StartTime,
		EndTime:   trace.EndTime,
		Flag:      flag,
		Retry:     3,
	})
}

func (api *ebpfProfileApi) ConsumeProfileSignals() {
	size := len(api.signals)
	if size > 0 {
		var toSendSignals []*ProfileSignal
		api.lock.Lock()
		toSendSignals = api.signals[0:size]
		api.signals = api.signals[size:]
		api.lock.Unlock()

		body, _ := json.Marshal(toSendSignals)

		client := &http.Client{
			Timeout: time.Second,
		}
		_, err := client.Post(api.address, "application/json", bytes.NewBuffer(body))
		if err != nil {
			api.logger.Error("Request Signal", zap.Error(err))
		} else {
			api.logger.Info("Request Signal", zap.Int("count", size))
		}
	}
}

type ProfileApi interface {
	AddProfileSignal(*model.TraceLabels, int)
	ConsumeProfileSignals()
}

type noopProfileApi struct {
}

func (api *noopProfileApi) AddProfileSignal(trace *model.TraceLabels, flag int) {
	// DO Nothing.
}

func (api *noopProfileApi) ConsumeProfileSignals() {}

func NewProfileApi(logger *zap.Logger, ip string, port int) ProfileApi {
	if port <= 0 {
		return &noopProfileApi{}
	}
	return &ebpfProfileApi{
		logger:  logger,
		address: fmt.Sprintf("http://%s:%d/otelSignal", ip, port),
		signals: make([]*ProfileSignal, 0),
	}
}
