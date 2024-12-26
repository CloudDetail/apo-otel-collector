package parser

import (
	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

type RpcParser struct {
}

func NewRpcParser() *RpcParser {
	return &RpcParser{}
}

func (parser *RpcParser) Parse(logger *zap.Logger, pid string, containerId string, serviceName string, entryUrl string, span *ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string {
	if span.Kind() != ptrace.SpanKindClient {
		return ""
	}
	rpcSystemAttr, systemExist := spanAttr.Get(conventions.AttributeRPCSystem)
	if !systemExist {
		return ""
	}

	return BuildExternalKey(keyValue, pid, containerId, serviceName, entryUrl,
		span.Name(),
		GetClientPeer(spanAttr), // ip:port
		rpcSystemAttr.Str(),
		span.Status().Code() == ptrace.StatusCodeError, // IsError
	)
}
