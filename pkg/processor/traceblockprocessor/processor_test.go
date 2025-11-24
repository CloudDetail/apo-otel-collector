package traceblockprocessor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
)

func TestTraceBlockProcessorDropsLongTraces(t *testing.T) {
	sink := newTracesSink()
	testCfg := &Config{
		IdleTTL:        10 * time.Millisecond,
		BlockThreshold: 20 * time.Millisecond, // Must be greater than IdleTTL
		BlockedIdleTTL: 20 * time.Millisecond,
	}
	processor, err := createTracesProcessor(context.Background(), processor.Settings{
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
	}, testCfg, sink)
	require.NoError(t, err)

	traceID := newTraceID(1)
	proc := processor.(*traceBlockProcessor)

	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 1)))
	require.Equal(t, int64(1), proc.receivedSpans)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 2)))
	require.Equal(t, int64(2), proc.receivedSpans)
	require.Equal(t, int64(0), proc.droppedSpans)

	time.Sleep(22 * time.Millisecond)
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 3)))
	require.Equal(t, int64(3), proc.receivedSpans)
	require.Equal(t, int64(1), proc.droppedSpans)

	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 4)))
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 5)))
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 6)))
	require.Equal(t, int64(6), proc.receivedSpans)
	require.Equal(t, int64(4), proc.droppedSpans)

	proc.reportStats()

	traceID = newTraceID(2)
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 1)))
	require.Equal(t, int64(1), proc.receivedSpans)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 2)))
	require.Equal(t, int64(2), proc.receivedSpans)
	require.Equal(t, int64(0), proc.droppedSpans)

	time.Sleep(22 * time.Millisecond)
	proc.cleanExpired()
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(traceID, 3)))
	require.Equal(t, int64(3), proc.receivedSpans)
	require.Equal(t, int64(0), proc.droppedSpans)
}

func TestTraceBlockProcessorCleanStrategy(t *testing.T) {
	sink := newTracesSink()
	testCfg := &Config{
		IdleTTL:        10 * time.Millisecond,
		BlockThreshold: 20 * time.Millisecond, // Must be greater than IdleTTL
		BlockedIdleTTL: 20 * time.Millisecond,
	}

	factory := NewFactory()
	processor, err := factory.CreateTracesProcessor(context.Background(), processor.Settings{
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
	}, testCfg, sink)
	require.NoError(t, err)

	activeID := newTraceID(2)
	blockedID := newTraceID(3)

	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(activeID, 1)))
	require.NoError(t, processor.ConsumeTraces(context.Background(), buildTraces(blockedID, 2)))

	now := time.Now()
	activeKey := activeID.String()
	blockedKey := blockedID.String()

	proc := processor.(*traceBlockProcessor)
	proc.mu.Lock()
	activeState := proc.traces[activeKey]
	blockedState := proc.traces[blockedKey]
	blockedState.isBlocked = true
	activeState.lastSeen = now.Add(-testCfg.IdleTTL - time.Millisecond)
	blockedState.lastSeen = now.Add(-(testCfg.IdleTTL + time.Millisecond))
	proc.mu.Unlock()

	proc.cleanExpired()

	proc.mu.Lock()
	_, activeExists := proc.traces[activeKey]
	blockedState = proc.traces[blockedKey]
	proc.mu.Unlock()

	require.False(t, activeExists, "active trace should be cleaned after idle ttl")
	require.NotNil(t, blockedState, "blocked trace should stay until extended ttl")

	proc.mu.Lock()
	blockedState.lastSeen = now.Add(-(2*testCfg.IdleTTL + 2*time.Millisecond))
	proc.mu.Unlock()

	proc.cleanExpired()

	proc.mu.Lock()
	_, blockedExists := proc.traces[blockedKey]
	proc.mu.Unlock()

	require.False(t, blockedExists, "blocked trace should be cleaned after extended ttl")
}

type tracesSink struct {
	mu        sync.Mutex
	spanCount int
}

func newTracesSink() *tracesSink {
	return &tracesSink{}
}

func (t *tracesSink) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{}
}

func (t *tracesSink) ConsumeTraces(_ context.Context, traces ptrace.Traces) error {
	count := countSpans(traces)
	t.mu.Lock()
	t.spanCount += count
	t.mu.Unlock()
	return nil
}

func (t *tracesSink) SpanCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spanCount
}

func countSpans(traces ptrace.Traces) int {
	var total int
	rs := traces.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		scopeSpans := rs.At(i).ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			total += scopeSpans.At(j).Spans().Len()
		}
	}
	return total
}

func buildTraces(traceID pcommon.TraceID, spanSeq byte) ptrace.Traces {
	traces := ptrace.NewTraces()
	span := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test-span")
	span.SetTraceID(traceID)

	var spanID pcommon.SpanID
	spanID[7] = spanSeq
	span.SetSpanID(spanID)

	now := time.Now()
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Millisecond)))
	return traces
}

func newTraceID(lastByte byte) pcommon.TraceID {
	var id pcommon.TraceID
	id[15] = lastByte
	return id
}
