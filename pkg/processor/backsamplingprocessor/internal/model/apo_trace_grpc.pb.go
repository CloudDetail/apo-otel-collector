// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v3.20.3
// source: internal/model/apo_trace.proto

package model

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	TraceService_StoreDataGroups_FullMethodName = "/kindling.TraceService/StoreDataGroups"
)

// TraceServiceClient is the client API for TraceService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type TraceServiceClient interface {
	StoreDataGroups(ctx context.Context, in *DataGroups, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type traceServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTraceServiceClient(cc grpc.ClientConnInterface) TraceServiceClient {
	return &traceServiceClient{cc}
}

func (c *traceServiceClient) StoreDataGroups(ctx context.Context, in *DataGroups, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, TraceService_StoreDataGroups_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TraceServiceServer is the server API for TraceService service.
// All implementations must embed UnimplementedTraceServiceServer
// for forward compatibility
type TraceServiceServer interface {
	StoreDataGroups(context.Context, *DataGroups) (*emptypb.Empty, error)
	mustEmbedUnimplementedTraceServiceServer()
}

// UnimplementedTraceServiceServer must be embedded to have forward compatible implementations.
type UnimplementedTraceServiceServer struct {
}

func (UnimplementedTraceServiceServer) StoreDataGroups(context.Context, *DataGroups) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StoreDataGroups not implemented")
}
func (UnimplementedTraceServiceServer) mustEmbedUnimplementedTraceServiceServer() {}

// UnsafeTraceServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to TraceServiceServer will
// result in compilation errors.
type UnsafeTraceServiceServer interface {
	mustEmbedUnimplementedTraceServiceServer()
}

func RegisterTraceServiceServer(s grpc.ServiceRegistrar, srv TraceServiceServer) {
	s.RegisterService(&TraceService_ServiceDesc, srv)
}

func _TraceService_StoreDataGroups_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DataGroups)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TraceServiceServer).StoreDataGroups(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: TraceService_StoreDataGroups_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TraceServiceServer).StoreDataGroups(ctx, req.(*DataGroups))
	}
	return interceptor(ctx, in, info, handler)
}

// TraceService_ServiceDesc is the grpc.ServiceDesc for TraceService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var TraceService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kindling.TraceService",
	HandlerType: (*TraceServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "StoreDataGroups",
			Handler:    _TraceService_StoreDataGroups_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "internal/model/apo_trace.proto",
}
