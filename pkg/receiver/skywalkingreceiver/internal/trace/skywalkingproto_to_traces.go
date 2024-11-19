// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/skywalking"

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/CloudDetail/apo-otel-collector/pkg/fillproc"
	"github.com/CloudDetail/apo-otel-collector/pkg/sqlprune"
	"github.com/google/uuid"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	conventions "go.opentelemetry.io/collector/semconv/v1.18.0"
	common "skywalking.apache.org/repo/goapi/collect/common/v3"
	agentV3 "skywalking.apache.org/repo/goapi/collect/language/agent/v3"
)

const (
	AttributeRefType                   = "refType"
	AttributeParentService             = "parent.service"
	AttributeParentInstance            = "parent.service.instance"
	AttributeParentEndpoint            = "parent.endpoint"
	AttributeSkywalkingSpanID          = "sw8.span_id"
	AttributeSkywalkingTraceID         = "sw8.trace_id"
	AttributeSkywalkingSegmentID       = "sw8.segment_id"
	AttributeSkywalkingParentSpanID    = "sw8.parent_span_id"
	AttributeSkywalkingParentSegmentID = "sw8.parent_segment_id"
	AttributeNetworkAddressUsedAtPeer  = "network.AddressUsedAtPeer"
)

var otSpanResourcesMapping = map[string]string{
	"url":         conventions.AttributeHTTPURL,
	"status_code": conventions.AttributeHTTPStatusCode,
}

var otSpanTagsMapping = map[string]string{
	"url":          conventions.AttributeHTTPURL,
	"db.type":      conventions.AttributeDBSystem,
	"db.instance":  conventions.AttributeDBName,
	"db.statement": conventions.AttributeDBStatement,
}

// ProtoToTraces converts multiple skywalking proto batches to internal traces
func ProtoToTraces(segment *agentV3.SegmentObject, pid int, containerId string) ptrace.Traces {
	traceData := ptrace.NewTraces()

	swSpans := segment.Spans
	if swSpans == nil && len(swSpans) == 0 {
		return traceData
	}

	resourceSpan := traceData.ResourceSpans().AppendEmpty()
	rs := resourceSpan.Resource()

	for _, span := range swSpans {
		swTagsToInternalResource(span, rs)
	}

	if pid > 0 {
		rs.Attributes().PutInt(fillproc.KEY_PID, int64(pid))
	}
	if len(containerId) > 0 {
		rs.Attributes().PutStr(fillproc.KEY_CONTAINERID, containerId)
	}
	rs.Attributes().PutStr(conventions.AttributeServiceName, segment.GetService())
	rs.Attributes().PutStr(conventions.AttributeServiceInstanceID, segment.GetServiceInstance())
	rs.Attributes().PutStr(AttributeSkywalkingTraceID, segment.GetTraceId())

	il := resourceSpan.ScopeSpans().AppendEmpty()
	swSpansToSpanSlice(segment.GetTraceId(), segment.GetTraceSegmentId(), swSpans, il.Spans())

	return traceData
}

func swTagsToInternalResource(span *agentV3.SpanObject, dest pcommon.Resource) {
	if span == nil {
		return
	}

	attrs := dest.Attributes()
	attrs.Clear()

	tags := span.Tags
	if tags == nil {
		return
	}

	for _, tag := range tags {
		otKey, ok := otSpanResourcesMapping[tag.Key]
		if ok {
			attrs.PutStr(otKey, tag.Value)
		}
	}
}

func swSpansToSpanSlice(traceID string, segmentID string, spans []*agentV3.SpanObject, dest ptrace.SpanSlice) {
	if len(spans) == 0 {
		return
	}

	dest.EnsureCapacity(len(spans))
	for _, span := range spans {
		if span == nil {
			continue
		}
		swSpanToSpan(traceID, segmentID, span, dest.AppendEmpty())
	}
}

func swSpanToSpan(traceID string, segmentID string, span *agentV3.SpanObject, dest ptrace.Span) {
	dest.SetTraceID(swTraceIDToTraceID(traceID))
	// skywalking defines segmentId + spanId as unique identifier
	// so use segmentId to convert to an unique otel-span
	dest.SetSpanID(segmentIDToSpanID(segmentID, uint32(span.GetSpanId())))

	// parent spanid = -1, means(root span) no parent span in current skywalking segment, so it is necessary to search for the parent segment.
	if span.ParentSpanId != -1 {
		dest.SetParentSpanID(segmentIDToSpanID(segmentID, uint32(span.GetParentSpanId())))
	} else if len(span.Refs) == 1 {
		// TODO: SegmentReference references usually have only one element, but in batch consumer case, such as in MQ or async batch process, it could be multiple.
		// We only handle one element for now.
		dest.SetParentSpanID(segmentIDToSpanID(span.Refs[0].GetParentTraceSegmentId(), uint32(span.Refs[0].GetParentSpanId())))
	}

	dest.SetName(span.OperationName)
	dest.SetStartTimestamp(microsecondsToTimestamp(span.GetStartTime()))
	dest.SetEndTimestamp(microsecondsToTimestamp(span.GetEndTime()))

	attrs := dest.Attributes()
	attrs.EnsureCapacity(len(span.Tags))
	swToTags(span, attrs)
	// drop the attributes slice if all of them were replaced during translation
	if attrs.Len() == 0 {
		attrs.Clear()
	}

	attrs.PutStr(AttributeSkywalkingSegmentID, segmentID)
	setSwSpanIDToAttributes(span, attrs)
	setInternalSpanStatus(span, dest.Status())

	switch {
	case span.SpanLayer == agentV3.SpanLayer_MQ:
		if span.SpanType == agentV3.SpanType_Entry {
			dest.SetKind(ptrace.SpanKindConsumer)
		} else if span.SpanType == agentV3.SpanType_Exit {
			dest.SetKind(ptrace.SpanKindProducer)
		}
	case span.GetSpanType() == agentV3.SpanType_Exit:
		dest.SetKind(ptrace.SpanKindClient)
	case span.GetSpanType() == agentV3.SpanType_Entry:
		dest.SetKind(ptrace.SpanKindServer)
	case span.GetSpanType() == agentV3.SpanType_Local:
		dest.SetKind(ptrace.SpanKindInternal)
	default:
		dest.SetKind(ptrace.SpanKindUnspecified)
	}

	swLogsToSpanEvents(span.GetLogs(), dest.Events())
	// skywalking: In the across thread and across processes, these references target the parent segments.
	swReferencesToSpanLinks(span.Refs, dest.Links())
}

func swReferencesToSpanLinks(refs []*agentV3.SegmentReference, dest ptrace.SpanLinkSlice) {
	if len(refs) == 0 {
		return
	}

	dest.EnsureCapacity(len(refs))

	for _, ref := range refs {
		link := dest.AppendEmpty()
		link.SetTraceID(swTraceIDToTraceID(ref.TraceId))
		link.SetSpanID(segmentIDToSpanID(ref.ParentTraceSegmentId, uint32(ref.ParentSpanId)))
		link.TraceState().FromRaw("")
		kvParis := []*common.KeyStringValuePair{
			{
				Key:   AttributeParentService,
				Value: ref.ParentService,
			},
			{
				Key:   AttributeParentInstance,
				Value: ref.ParentServiceInstance,
			},
			{
				Key:   AttributeParentEndpoint,
				Value: ref.ParentEndpoint,
			},
			{
				Key:   AttributeNetworkAddressUsedAtPeer,
				Value: ref.NetworkAddressUsedAtPeer,
			},
			{
				Key:   AttributeRefType,
				Value: ref.RefType.String(),
			},
			{
				Key:   AttributeSkywalkingTraceID,
				Value: ref.TraceId,
			},
			{
				Key:   AttributeSkywalkingParentSegmentID,
				Value: ref.ParentTraceSegmentId,
			},
			{
				Key:   AttributeSkywalkingParentSpanID,
				Value: strconv.Itoa(int(ref.ParentSpanId)),
			},
		}
		swKvPairsToInternalAttributes(kvParis, link.Attributes())
	}
}

func setInternalSpanStatus(span *agentV3.SpanObject, dest ptrace.Status) {
	if span.GetIsError() {
		dest.SetCode(ptrace.StatusCodeError)
		dest.SetMessage("ERROR")
	} else {
		dest.SetCode(ptrace.StatusCodeOk)
		dest.SetMessage("SUCCESS")
	}
}

func setSwSpanIDToAttributes(span *agentV3.SpanObject, dest pcommon.Map) {
	dest.PutInt(AttributeSkywalkingSpanID, int64(span.GetSpanId()))
	if span.ParentSpanId != -1 {
		dest.PutInt(AttributeSkywalkingParentSpanID, int64(span.GetParentSpanId()))
	}
}

func swLogsToSpanEvents(logs []*agentV3.Log, dest ptrace.SpanEventSlice) {
	if len(logs) == 0 {
		return
	}
	dest.EnsureCapacity(len(logs))

	for i, log := range logs {
		var event ptrace.SpanEvent
		if dest.Len() > i {
			event = dest.At(i)
		} else {
			event = dest.AppendEmpty()
		}

		event.SetName("logs")
		event.SetTimestamp(microsecondsToTimestamp(log.GetTime()))
		if len(log.GetData()) == 0 {
			continue
		}

		attrs := event.Attributes()
		attrs.Clear()
		attrs.EnsureCapacity(len(log.GetData()))
		swKvPairsToInternalAttributes(log.GetData(), attrs)
	}
}

func swToTags(span *agentV3.SpanObject, dest pcommon.Map) {
	if span.SpanType == agentV3.SpanType_Exit {
		dest.PutStr(conventions.AttributeNetPeerName, span.Peer)
		if span.SpanLayer == agentV3.SpanLayer_RPCFramework {
			dest.PutStr(conventions.AttributeRPCSystem, strings.ToLower(GetComponent(span.ComponentId)))
		}
	}
	if span.Tags == nil {
		return
	}

	if span.SpanLayer == agentV3.SpanLayer_MQ {
		if span.SpanType == agentV3.SpanType_Exit || span.SpanType == agentV3.SpanType_Entry {
			dest.PutStr(conventions.AttributeMessagingSystem, strings.ToLower(GetComponent(span.ComponentId)))
		}
	}

	for _, tag := range span.Tags {
		if setCacheAttribute(dest, tag.Key, tag.Value, span.SpanType) &&
			setDbAttribute(dest, tag.Key, tag.Value) &&
			setMqAttribute(dest, tag.Key, tag.Value) {

			if otKey, ok := otSpanTagsMapping[tag.Key]; ok {
				dest.PutStr(otKey, tag.Value)
			} else {
				dest.PutStr(tag.Key, tag.Value)
			}
		}
	}
}

func setCacheAttribute(dest pcommon.Map, key string, value string, spanType agentV3.SpanType) bool {
	if !strings.HasPrefix(key, "cache.") {
		return true
	}
	if spanType == agentV3.SpanType_Local {
		// EhCache...
		dest.PutStr(key, value)
	} else if key == "cache.type" {
		// Redis...
		// XMemcached -> memcached
		dest.PutStr(conventions.AttributeDBSystem, strings.ToLower(GetServerName(value)))
	} else if key == "cache.cmd" {
		// Redis、Memcached
		dest.PutStr(conventions.AttributeDBStatement, value)
	} else if key == "cache.op" {
		// Aerospike
		dest.PutStr(conventions.AttributeDBOperation, value)
	} else {
		dest.PutStr(key, value)
	}
	return false
}

func setDbAttribute(dest pcommon.Map, key string, value string) bool {
	if !strings.HasPrefix(key, "db.") {
		return true
	}
	if key == "db.type" {
		// Mysql...
		dest.PutStr(conventions.AttributeDBSystem, strings.ToLower(value))
	} else if key == "db.instance" {
		dest.PutStr(conventions.AttributeDBName, value)
	} else if key == "db.statement" {
		dest.PutStr(conventions.AttributeDBStatement, value)
		// 解析SQL语句
		if value != "" {
			if operation, table := sqlprune.SQLParseOperationAndTableNEW(value); operation != "" {
				dest.PutStr(conventions.AttributeDBOperation, operation)
				dest.PutStr(conventions.AttributeDBSQLTable, table)
			}
		}
	} else {
		dest.PutStr(key, value)
	}
	return false
}

func setMqAttribute(dest pcommon.Map, key string, value string) bool {
	if !strings.HasPrefix(key, "mq.") {
		return true
	}

	if key == "mq.queue" {
		dest.PutStr(conventions.AttributeMessagingDestinationName, value)
	} else if key == "mq.topic" {
		// 已赋值则不重新赋予
		if _, exist := dest.Get(conventions.AttributeMessagingDestinationName); !exist {
			dest.PutStr(conventions.AttributeMessagingDestinationName, value)
		}
	} else if key == "mq.broker" {
		// MQ Consumer 未设置net.peer.name，此处通过通过mq.broker填充
		dest.PutStr(conventions.AttributeNetPeerName, value)
	} else {
		dest.PutStr(key, value)
	}
	return false
}

func swKvPairsToInternalAttributes(pairs []*common.KeyStringValuePair, dest pcommon.Map) {
	if pairs == nil {
		return
	}

	for _, pair := range pairs {
		dest.PutStr(pair.Key, pair.Value)
	}
}

// microsecondsToTimestamp converts epoch microseconds to pcommon.Timestamp
func microsecondsToTimestamp(ms int64) pcommon.Timestamp {
	return pcommon.NewTimestampFromTime(time.UnixMilli(ms))
}

func swTraceIDToTraceID(traceID string) pcommon.TraceID {
	// skywalking traceid format:
	// de5980b8-fce3-4a37-aab9-b4ac3af7eedd: from browser/js-sdk/envoy/nginx-lua sdk/py-agent
	// 56a5e1c519ae4c76a2b8b11d92cead7f.12.16563474296430001: from java-agent

	if len(traceID) <= 36 { // 36: uuid length (rfc4122)
		uid, err := uuid.Parse(traceID)
		if err != nil {
			return pcommon.NewTraceIDEmpty()
		}
		return pcommon.TraceID(uid)
	}
	return swStringToUUID(traceID, 0)
}

func segmentIDToSpanID(segmentID string, spanID uint32) pcommon.SpanID {
	// skywalking segmentid format:
	// 56a5e1c519ae4c76a2b8b11d92cead7f.12.16563474296430001: from TraceSegmentId
	// 56a5e1c519ae4c76a2b8b11d92cead7f: from ParentTraceSegmentId

	if len(segmentID) < 32 {
		return pcommon.NewSpanIDEmpty()
	}
	return uuidTo8Bytes(swStringToUUID(segmentID, spanID))
}

func swStringToUUID(s string, extra uint32) (dst [16]byte) {
	// there are 2 possible formats for 's':
	// s format = 56a5e1c519ae4c76a2b8b11d92cead7f.0000000000.000000000000000000
	//            ^ start(length=32)               ^ mid(u32) ^ last(u64)
	// uid = UUID(start) XOR ([4]byte(extra) . [4]byte(uint32(mid)) . [8]byte(uint64(last)))

	// s format = 56a5e1c519ae4c76a2b8b11d92cead7f
	//            ^ start(length=32)
	// uid = UUID(start) XOR [4]byte(extra)

	if len(s) < 32 {
		return
	}

	t := unsafeGetBytes(s)
	var uid [16]byte
	_, err := hex.Decode(uid[:], t[:32])
	if err != nil {
		return uid
	}

	for i := 0; i < 4; i++ {
		uid[i] ^= byte(extra)
		extra >>= 8
	}

	if len(s) == 32 {
		return uid
	}

	index1 := bytes.IndexByte(t, '.')
	index2 := bytes.LastIndexByte(t, '.')
	if index1 != 32 || index2 < 0 {
		return
	}

	mid, err := strconv.Atoi(s[index1+1 : index2])
	if err != nil {
		return
	}

	last, err := strconv.Atoi(s[index2+1:])
	if err != nil {
		return
	}

	for i := 4; i < 8; i++ {
		uid[i] ^= byte(mid)
		mid >>= 8
	}

	for i := 8; i < 16; i++ {
		uid[i] ^= byte(last)
		last >>= 8
	}

	return uid
}

func uuidTo8Bytes(uuid [16]byte) [8]byte {
	// high bit XOR low bit
	var dst [8]byte
	for i := 0; i < 8; i++ {
		dst[i] = uuid[i] ^ uuid[i+8]
	}
	return dst
}

func unsafeGetBytes(s string) []byte {
	return (*[0x7fff0000]byte)(unsafe.Pointer(unsafe.StringData(s)))[:len(s):len(s)]
}
