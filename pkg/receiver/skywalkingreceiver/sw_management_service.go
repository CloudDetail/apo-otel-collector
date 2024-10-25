package skywalkingreceiver

import (
	"context"

	"github.com/CloudDetail/apo-otel-collector/pkg/receiver/skywalkingreceiver/internal/trace"
	v3 "skywalking.apache.org/repo/goapi/collect/common/v3"
	management "skywalking.apache.org/repo/goapi/collect/management/v3"
)

type SwManagementServiceServer struct {
	management.UnimplementedManagementServiceServer
	TraceReceiver *trace.Receiver
}

func (s *SwManagementServiceServer) ReportInstanceProperties(ctx context.Context, properties *management.InstanceProperties) (*v3.Commands, error) {
	if s.TraceReceiver != nil && s.TraceReceiver.FillProcExtension != nil {
		s.TraceReceiver.FillProcExtension.GetMatchPidAndContainerId(ctx, properties.Service, properties.ServiceInstance, "")
	}
	return nil, nil
}

func (s *SwManagementServiceServer) KeepAlive(ctx context.Context, ping *management.InstancePingPkg) (*v3.Commands, error) {
	if s.TraceReceiver != nil && s.TraceReceiver.FillProcExtension != nil {
		s.TraceReceiver.FillProcExtension.GetMatchPidAndContainerId(ctx, ping.Service, ping.ServiceInstance, "")
	}
	return nil, nil
}
