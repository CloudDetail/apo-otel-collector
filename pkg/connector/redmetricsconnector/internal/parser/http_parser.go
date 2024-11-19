package parser

import (
	"fmt"
	"strings"

	"github.com/CloudDetail/apo-otel-collector/pkg/connector/redmetricsconnector/internal/cache"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

const (
	AttributeHttpMethod        = "http.method"         // 1.x
	AttributeHttpRequestMethod = "http.request.method" // 2.x

	AttributeHttpUrl = "http.url" // 1.x
	AttributeUrlFull = "url.full" // 2.x
)

type HttpParser struct {
	urlParser string
}

func NewHttpParser(urlParser string) *HttpParser {
	return &HttpParser{
		urlParser: urlParser,
	}
}

func (parser *HttpParser) Parse(logger *zap.Logger, pid string, containerId string, serviceName string, span ptrace.Span, spanAttr pcommon.Map, keyValue *cache.ReusedKeyValue) string {
	if span.Kind() != ptrace.SpanKindClient {
		return ""
	}
	httpMethod := getHttpMethod(spanAttr)
	if httpMethod == "" {
		return ""
	}

	return BuildExternalKey(keyValue, pid, containerId, serviceName,
		parser.parse(httpMethod, getHttpUrl(spanAttr)), // Get /xxx
		GetClientPeer(spanAttr, "http", Unknown),       // http://ip:port
		span.Status().Code() == ptrace.StatusCodeError, // IsError
	)
}

func (parser *HttpParser) parse(httpMethod string, url string) string {
	if parser.urlParser == "topUrl" {
		return fmt.Sprintf("%s %s", httpMethod, getTopUrl(url))
	}
	return httpMethod
}

func getTopUrl(url string) string {
	if url == "" {
		return Unknown
	}
	urls := strings.Split(url, "/")
	if len(urls) == 1 {
		return url
	}
	if len(urls) >= 3 && strings.HasSuffix(urls[0], ":") && urls[1] == "" {
		// schema://host:port/path
		if len(urls) == 3 {
			return "/"
		} else {
			return fmt.Sprintf("/%s", urls[3])
		}
	}
	return fmt.Sprintf("/%s", urls[1])
}

func getHttpMethod(attr pcommon.Map) string {
	// 1.x http.method
	if httpMethod, found := attr.Get(AttributeHttpMethod); found {
		return httpMethod.Str()
	}
	// 2.x http.request.method
	if httpRequestMethod, found := attr.Get(AttributeHttpRequestMethod); found {
		return httpRequestMethod.Str()
	}
	return ""
}

func getHttpUrl(attr pcommon.Map) string {
	// 1.x http.url
	if httpUrl, found := attr.Get(AttributeHttpUrl); found {
		return httpUrl.Str()
	}
	// 2.x url.full
	if urlFull, found := attr.Get(AttributeUrlFull); found {
		return urlFull.Str()
	}
	return ""
}
