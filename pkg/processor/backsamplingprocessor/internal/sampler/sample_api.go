package sampler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
	grpc_model "github.com/CloudDetail/apo-otel-collector/pkg/processor/backsamplingprocessor/internal/model"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	TableSpanTrace = "span_trace_group"
)

type SampleApi struct {
	slowThresholdClient grpc_model.SlowThresholdServiceClient
	sampledClient       grpc_model.ProfileServiceClient
	storeTraceclient    grpc_model.TraceServiceClient
	getSampleRateClient grpc_model.SampleServiceClient
	logEnable           bool

	lock         sync.Mutex
	cachedTraces []*model.TraceGroup
}

func NewSampleApi(host string, port int, adaptiveEnable bool, logEnable bool) (*SampleApi, error) {
	var kacp = keepalive.ClientParameters{
		Time:                5 * time.Second, // send pings every 5 seconds if there is no activity
		Timeout:             time.Second,     // wait 1 second for ping ack before considering the connection dead
		PermitWithoutStream: true,            // send pings even without active streams
	}
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(kacp))
	if err != nil {
		return nil, err
	}

	sampleApi := &SampleApi{
		slowThresholdClient: grpc_model.NewSlowThresholdServiceClient(conn),
		sampledClient:       grpc_model.NewProfileServiceClient(conn),
		storeTraceclient:    grpc_model.NewTraceServiceClient(conn),
		logEnable:           logEnable,
	}
	if adaptiveEnable {
		sampleApi.getSampleRateClient = grpc_model.NewSampleServiceClient(conn)
	}
	return sampleApi, nil
}

func (api *SampleApi) QuerySlowThreshold(nodeIp string) (*grpc_model.SlowThresholdResponse, error) {
	return api.slowThresholdClient.QuerySlowThreshold(context.Background(), &grpc_model.SlowThresholdRequest{
		Ip: nodeIp,
	})
}

func (api *SampleApi) QueryProfiles(query *grpc_model.ProfileQuery) (*grpc_model.ProfileResult, error) {
	return api.sampledClient.QueryProfiles(context.Background(), query)
}

func (api *SampleApi) SendTraces() {
	size := len(api.cachedTraces)
	if size == 0 {
		return
	}

	datas := make([]string, 0)
	for i := 0; i < size; i++ {
		dataVal, _ := json.Marshal(api.cachedTraces[i])
		data := string(dataVal)
		if api.logEnable {
			log.Printf("[Send Trace] %s", data)
		}
		datas = append(datas, data)
	}

	api.lock.Lock()
	api.cachedTraces = api.cachedTraces[size:]
	api.lock.Unlock()

	api.storeTraceclient.StoreDataGroups(context.Background(), &grpc_model.DataGroups{
		Name:  TableSpanTrace,
		Datas: datas,
	})
}

func (api *SampleApi) StoreTrace(trace *model.TraceLabels) {
	api.lock.Lock()
	defer api.lock.Unlock()

	api.cachedTraces = append(api.cachedTraces, &model.TraceGroup{
		Name:      TableSpanTrace,
		Version:   "v1.1",
		Source:    "otel",
		Timestamp: trace.StartTime,
		Labels:    trace,
	})
}

func (api *SampleApi) GetSampleValue(query *grpc_model.SampleMetric) (*grpc_model.SampleResult, error) {
	if api.getSampleRateClient == nil {
		return nil, nil
	}
	return api.getSampleRateClient.GetSampleValue(context.Background(), query)
}
