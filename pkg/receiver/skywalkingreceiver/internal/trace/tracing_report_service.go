// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/skywalkingreceiver/internal/trace"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"google.golang.org/protobuf/proto"
	common "skywalking.apache.org/repo/goapi/collect/common/v3"
	agent "skywalking.apache.org/repo/goapi/collect/language/agent/v3"
)

const (
	collectorHTTPTransport = "http"
	grpcTransport          = "grpc"
	failing                = "failing"
)

type Receiver struct {
	undertowCache     *UndertowCache
	FillProcExtension fillproc.FillProc
	nextConsumer      consumer.Traces
	grpcObsrecv       *receiverhelper.ObsReport
	httpObsrecv       *receiverhelper.ObsReport
	agent.UnimplementedTraceSegmentReportServiceServer
}

// NewReceiver creates a new Receiver reference.
func NewReceiver(nextConsumer consumer.Traces, set receiver.Settings) (*Receiver, error) {
	grpcObsrecv, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             set.ID,
		Transport:              grpcTransport,
		ReceiverCreateSettings: set,
	})
	if err != nil {
		return nil, err
	}
	httpObsrecv, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             set.ID,
		Transport:              collectorHTTPTransport,
		ReceiverCreateSettings: set,
	})
	if err != nil {
		return nil, err
	}
	return &Receiver{
		undertowCache: &UndertowCache{},
		nextConsumer:  nextConsumer,
		grpcObsrecv:   grpcObsrecv,
		httpObsrecv:   httpObsrecv,
	}, nil
}

// Collect implements the service Collect traces func.
func (r *Receiver) Collect(stream agent.TraceSegmentReportService_CollectServer) error {
	for {
		segmentObject, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return stream.SendAndClose(&common.Commands{})
			}
			return err
		}

		err = r.consumeTraces(stream.Context(), segmentObject)
		if err != nil {
			return stream.SendAndClose(&common.Commands{})
		}
	}
}

// CollectInSync implements the service CollectInSync traces func.
func (r *Receiver) CollectInSync(ctx context.Context, segments *agent.SegmentCollection) (*common.Commands, error) {
	for _, segment := range segments.Segments {
		marshaledSegment, err := proto.Marshal(segment)
		if err != nil {
			fmt.Printf("cannot marshal segemnt from sync, %v", err)
		}
		err = r.consumeTraces(ctx, segment)
		if err != nil {
			fmt.Printf("cannot consume traces, %v", err)
		}
		fmt.Printf("receivec data:%s", marshaledSegment)
	}
	return &common.Commands{}, nil
}

func (r *Receiver) consumeHttpTraces(req *http.Request, segment *agent.SegmentObject) error {
	if segment == nil {
		return nil
	}
	var (
		pid         int
		containerId string
	)
	if r.FillProcExtension != nil {
		pid, containerId = r.FillProcExtension.GetMatchPidAndContainerIdForHttp(req.RemoteAddr, req.Host)
	}
	ptd := ProtoToTraces(segment, pid, containerId)

	if result := r.undertowCache.CheckUndertow(segment, &ptd); result != nil {
		return r.nextConsumer.ConsumeTraces(req.Context(), *result)
	}
	return nil
}

func (r *Receiver) consumeTraces(ctx context.Context, segment *agent.SegmentObject) error {
	if segment == nil {
		return nil
	}

	var (
		pid         int
		containerId string
	)
	if r.FillProcExtension != nil {
		pid, containerId = r.FillProcExtension.GetMatchPidAndContainerId(ctx)
	}
	ptd := ProtoToTraces(segment, pid, containerId)

	if result := r.undertowCache.CheckUndertow(segment, &ptd); result != nil {
		return r.nextConsumer.ConsumeTraces(ctx, *result)
	}
	return nil
}

func (r *Receiver) HTTPHandler(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "application/json")
	b, err := io.ReadAll(req.Body)
	if err != nil {
		response := &Response{Status: failing, Msg: err.Error()}
		ResponseWithJSON(rsp, response, http.StatusBadRequest)
		return
	}
	var data []*agent.SegmentObject
	if err = json.Unmarshal(b, &data); err != nil {
		fmt.Printf("cannot Unmarshal skywalking segment collection, %v", err)
	}

	for _, segment := range data {
		err = r.consumeHttpTraces(req, segment)
		if err != nil {
			fmt.Printf("cannot consume traces, %v", err)
		}
	}
}

type Response struct {
	Status string `json:"status"`
	Msg    string `json:"msg"`
}

func ResponseWithJSON(rsp http.ResponseWriter, response *Response, code int) {
	rsp.WriteHeader(code)
	_ = json.NewEncoder(rsp).Encode(response)
}
