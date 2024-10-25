// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace // import "github.com/CloudDetail/apo-otel-collector/pkg/receiver/otlpreceiver/internal/trace"

import (
	"context"
	"net/http"

	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"github.com/CloudDetail/apo-otel-collector/pkg/receiver/otlpreceiver/internal/errors"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
)

const dataFormatProtobuf = "protobuf"

// Receiver is the type used to handle spans from OpenTelemetry exporters.
type Receiver struct {
	ptraceotlp.UnimplementedGRPCServer
	nextConsumer      consumer.Traces
	obsreport         *receiverhelper.ObsReport
	fillProcExtension fillproc.FillProc
}

// New creates a new Receiver reference.
func New(nextConsumer consumer.Traces, obsreport *receiverhelper.ObsReport, fillProcExtension fillproc.FillProc) *Receiver {
	return &Receiver{
		nextConsumer:      nextConsumer,
		obsreport:         obsreport,
		fillProcExtension: fillProcExtension,
	}
}

// Export implements the service Export traces func.
func (r *Receiver) Export(ctx context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	td := req.Traces()
	// We need to ensure that it propagates the receiver name as a tag
	numSpans := td.SpanCount()
	if numSpans == 0 {
		return ptraceotlp.NewExportResponse(), nil
	}

	ctx = r.obsreport.StartTracesOp(ctx)

	for i := 0; i < td.ResourceSpans().Len(); i++ {
		resourceAttr := td.ResourceSpans().At(i).Resource().Attributes()

		sdkName, _ := resourceAttr.Get(conventions.AttributeTelemetrySDKName)
		serviceName, _ := resourceAttr.Get(conventions.AttributeServiceName)
		serviceInstanceId, _ := resourceAttr.Get(conventions.AttributeServiceInstanceID)
		pid, containerId := r.fillProcExtension.GetMatchPidAndContainerId(ctx, serviceName.Str(), serviceInstanceId.Str(), sdkName.Str())
		if pid > 0 {
			resourceAttr.PutInt(fillproc.KEY_PID, int64(pid))
		}
		if containerId != "" {
			resourceAttr.PutStr(fillproc.KEY_CONTAINERID, containerId)
		}
	}
	err := r.nextConsumer.ConsumeTraces(ctx, td)
	r.obsreport.EndTracesOp(ctx, dataFormatProtobuf, numSpans, err)

	// Use appropriate status codes for permanent/non-permanent errors
	// If we return the error straightaway, then the grpc implementation will set status code to Unknown
	// Refer: https://github.com/grpc/grpc-go/blob/v1.59.0/server.go#L1345
	// So, convert the error to appropriate grpc status and return the error
	// NonPermanent errors will be converted to codes.Unavailable (equivalent to HTTP 503)
	// Permanent errors will be converted to codes.InvalidArgument (equivalent to HTTP 400)
	if err != nil {
		return ptraceotlp.NewExportResponse(), errors.GetStatusFromError(err)
	}

	return ptraceotlp.NewExportResponse(), nil
}

// Export implements the service Export traces func.
func (r *Receiver) ExportHttp(httpReq *http.Request, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	td := req.Traces()
	// We need to ensure that it propagates the receiver name as a tag
	numSpans := td.SpanCount()
	if numSpans == 0 {
		return ptraceotlp.NewExportResponse(), nil
	}

	ctx := httpReq.Context()

	ctx = r.obsreport.StartTracesOp(ctx)

	for i := 0; i < td.ResourceSpans().Len(); i++ {
		resourceAttr := td.ResourceSpans().At(i).Resource().Attributes()

		sdkName, _ := resourceAttr.Get(conventions.AttributeTelemetrySDKName)
		serviceName, _ := resourceAttr.Get(conventions.AttributeServiceName)
		serviceInstanceId, _ := resourceAttr.Get(conventions.AttributeServiceInstanceID)
		pid, containerId := r.fillProcExtension.GetMatchPidAndContainerIdForHttp(httpReq.RemoteAddr, httpReq.Host, serviceName.Str(), serviceInstanceId.Str(), sdkName.Str())
		if pid > 0 {
			resourceAttr.PutInt(fillproc.KEY_PID, int64(pid))
		}
		if containerId != "" {
			resourceAttr.PutStr(fillproc.KEY_CONTAINERID, containerId)
		}
	}
	err := r.nextConsumer.ConsumeTraces(ctx, td)
	r.obsreport.EndTracesOp(ctx, dataFormatProtobuf, numSpans, err)

	// Use appropriate status codes for permanent/non-permanent errors
	// If we return the error straightaway, then the grpc implementation will set status code to Unknown
	// Refer: https://github.com/grpc/grpc-go/blob/v1.59.0/server.go#L1345
	// So, convert the error to appropriate grpc status and return the error
	// NonPermanent errors will be converted to codes.Unavailable (equivalent to HTTP 503)
	// Permanent errors will be converted to codes.InvalidArgument (equivalent to HTTP 400)
	if err != nil {
		return ptraceotlp.NewExportResponse(), errors.GetStatusFromError(err)
	}

	return ptraceotlp.NewExportResponse(), nil
}
