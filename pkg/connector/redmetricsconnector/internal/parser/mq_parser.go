package parser

import (
	"strings"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.6.1"
	"go.uber.org/zap"
)

const (
	AttributeMessageDest     = "messaging.destination"
	AttributeMessageDestName = "messaging.destination.name"
)

type MqParser struct {
}

func NewMqParser() *MqParser {
	return &MqParser{}
}

func (parser *MqParser) Parse(logger *zap.Logger, pid string, containerId string, serviceName string, entryUrl string, span *ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string {
	if span.Kind() != ptrace.SpanKindClient && span.Kind() != ptrace.SpanKindProducer && span.Kind() != ptrace.SpanKindConsumer {
		return ""
	}

	var (
		name     string
		mqSystem string
	)
	mqSystemAttr, systemExist := spanAttr.Get(conventions.AttributeMessagingSystem)
	if systemExist {
		mqSystem = mqSystemAttr.Str()
	} else {
		if span.Kind() == ptrace.SpanKindClient {
			return ""
		}
		mqSystem = Unknown
	}
	if span.Kind() == ptrace.SpanKindClient {
		name = span.Name()
	} else {
		name = getMessageDest(spanAttr, Unknown)
	}

	return buildMqKey(keyValue, pid, containerId, serviceName, entryUrl,
		name,                    // Topic
		GetClientPeer(spanAttr), // ip:port
		mqSystem,                // mqSystem, eg. rabbitmq、kafka
		span.Status().Code() == ptrace.StatusCodeError, // IsError
		strings.ToLower(span.Kind().String()),
	)
}

func getMessageDest(attr pcommon.Map, defaultValue string) string {
	// 1.x messaging.destination
	if messageDest, found := attr.Get(AttributeMessageDest); found {
		return messageDest.Str()
	}
	// 2.x messaging.destination.name
	if messageDestName, found := attr.Get(AttributeMessageDestName); found {
		return messageDestName.Str()
	}
	return defaultValue
}
