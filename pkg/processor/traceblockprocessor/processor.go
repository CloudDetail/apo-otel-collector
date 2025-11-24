package traceblockprocessor

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

type traceState struct {
	firstSeen time.Time
	lastSeen  time.Time
	isBlocked bool
}

type traceBlockProcessor struct {
	logger       *zap.Logger
	nextConsumer consumer.Traces

	mu        sync.Mutex
	traces    map[string]*traceState
	stopOnce  sync.Once
	startOnce sync.Once

	minuteTicker *time.Ticker
	stopCh       chan struct{}
	doneCh       chan struct{}

	idleTTL        time.Duration
	blockedIdleTTL time.Duration
	blockThreshold time.Duration

	// Statistics
	receivedSpans int64
	droppedSpans  int64
}

func newTraceBlockProcessor(ctx context.Context, settings component.TelemetrySettings, cfg Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	settings.Logger.Info("Building traceblockprocessor with config", zap.Any("config", cfg))
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &traceBlockProcessor{
		logger:         settings.Logger,
		nextConsumer:   nextConsumer,
		traces:         make(map[string]*traceState),
		idleTTL:        cfg.IdleTTL,
		blockedIdleTTL: cfg.BlockedIdleTTL,
		blockThreshold: cfg.BlockThreshold,
	}, nil
}

func (p *traceBlockProcessor) Start(_ context.Context, _ component.Host) error {
	p.startOnce.Do(func() {
		p.stopCh = make(chan struct{})
		p.doneCh = make(chan struct{})
		p.minuteTicker = time.NewTicker(time.Minute)
		go p.runMinuteTask()
	})
	return nil
}

func (p *traceBlockProcessor) Shutdown(_ context.Context) error {
	p.stopOnce.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
		}
	})
	if p.doneCh != nil {
		<-p.doneCh
	}
	return nil
}

func (p *traceBlockProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

func (p *traceBlockProcessor) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	// Count total spans received
	totalSpans := int64(0)
	resourceSpans := traces.ResourceSpans()
	resourceSpans.RemoveIf(func(r ptrace.ResourceSpans) bool {
		scopeSpans := r.ScopeSpans()
		scopeSpans.RemoveIf(func(ss ptrace.ScopeSpans) bool {
			spans := ss.Spans()
			spans.RemoveIf(func(span ptrace.Span) bool {
				totalSpans++
				traceID := span.TraceID()
				if traceID.IsEmpty() {
					return false
				}
				return p.shouldDrop(traceID)
			})
			return spans.Len() == 0
		})
		return scopeSpans.Len() == 0
	})

	// Count remaining spans after filtering
	remainingSpans := int64(0)
	for i := 0; i < resourceSpans.Len(); i++ {
		rs := resourceSpans.At(i)
		scopeSpans := rs.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			ss := scopeSpans.At(j)
			remainingSpans += int64(ss.Spans().Len())
		}
	}

	// Count dropped spans (original total - remaining)
	droppedInBatch := totalSpans - remainingSpans
	p.mu.Lock()
	p.receivedSpans += totalSpans
	p.droppedSpans += droppedInBatch
	p.mu.Unlock()

	if resourceSpans.Len() == 0 {
		return nil
	}
	return p.nextConsumer.ConsumeTraces(ctx, traces)
}

func (p *traceBlockProcessor) runMinuteTask() {
	defer func() {
		if p.minuteTicker != nil {
			p.minuteTicker.Stop()
		}
		if p.doneCh != nil {
			close(p.doneCh)
		}
	}()
	for {
		select {
		case <-p.minuteTicker.C:
			p.cleanExpired()
			p.reportStats()
		case <-p.stopCh:
			return
		}
	}
}

func (p *traceBlockProcessor) cleanExpired() {
	now := time.Now()
	p.mu.Lock()
	for traceID, state := range p.traces {
		ttl := p.idleTTL
		if state.isBlocked {
			ttl = p.blockedIdleTTL
		}
		if now.Sub(state.lastSeen) >= ttl {
			delete(p.traces, traceID)
		}
	}

	p.mu.Unlock()
}

func (p *traceBlockProcessor) reportStats() {
	p.mu.Lock()
	received := p.receivedSpans
	dropped := p.droppedSpans

	p.receivedSpans = 0
	p.droppedSpans = 0
	p.mu.Unlock()

	if p.logger != nil && received > 0 {
		dropRate := float64(0)
		if dropped > 0 {
			dropRate = float64(dropped) / float64(received) * 100
		}
		p.logger.Info("Trace block processor statistics",
			zap.Int64("received_spans", received),
			zap.Int64("dropped_spans", dropped),
			zap.Float64("drop_rate_percent", dropRate),
		)
	}
}

func (p *traceBlockProcessor) shouldDrop(traceID pcommon.TraceID) bool {
	now := time.Now()
	key := traceID.String()
	if key == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	state, ok := p.traces[key]
	if !ok {
		p.traces[key] = &traceState{
			firstSeen: now,
			lastSeen:  now,
		}
		return false
	}

	state.lastSeen = now
	if !state.isBlocked && now.Sub(state.firstSeen) >= p.blockThreshold {
		state.isBlocked = true
	}
	return state.isBlocked
}
