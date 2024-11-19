package parser

import (
	"strings"

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

func (parser *RpcParser) Parse(logger *zap.Logger, pid string, containerId string, serviceName string, span ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string {
	if span.Kind() == ptrace.SpanKindClient {
		return ""
	}
	rpcSystemAttr, systemExist := spanAttr.Get(conventions.AttributeRPCSystem)
	if !systemExist {
		return ""
	}
	protocol := rpcSystemAttr.Str()
	// apache_dubbo => dubbo
	if index := strings.LastIndex(protocol, "_"); index != -1 {
		protocol = protocol[index+1:]
	}

	return BuildExternalKey(keyValue, pid, containerId, serviceName,
		span.Name(),
		GetClientPeer(spanAttr, protocol, Unknown),     // dubbo://ip:port
		span.Status().Code() == ptrace.StatusCodeError, // IsError
	)
}
