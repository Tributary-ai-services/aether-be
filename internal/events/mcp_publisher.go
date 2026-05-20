// Package events publishes CloudEvents 1.0 activity events from aether-be
// to Kafka for the TAS Live Streams pipeline.
package events

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"
	"github.com/Tributary-ai-services/aether-shared/go-events/payloads"
	"github.com/Tributary-ai-services/aether-shared/go-events/topics"
)

const ceSource = "urn:tas:service:aether-be:mcp-proxy"

type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// MCPPublisher emits com.tas.activity.mcp.tool_invoked CloudEvents to
// tas.activity.mcp around aether-be's MCP proxy tool dispatch. A nil
// receiver is safe — every method becomes a no-op so callers don't need
// nil checks.
type MCPPublisher struct {
	writer  kafkaWriter
	logger  *zap.Logger
	timeout time.Duration
}

// MCPConfig configures the MCPPublisher.
type MCPConfig struct {
	Brokers      []string
	Topic        string
	Logger       *zap.Logger
	WriteTimeout time.Duration
}

// NewMCPPublisher returns an MCPPublisher backed by a kafka-go Writer.
// Returns nil if brokers is empty so callers can pass through without
// branches at every call site.
func NewMCPPublisher(cfg MCPConfig) *MCPPublisher {
	if len(cfg.Brokers) == 0 {
		return nil
	}
	topic := cfg.Topic
	if topic == "" {
		topic = topics.ActivityMCP
	}
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	timeout := cfg.WriteTimeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		BatchTimeout: 10 * time.Millisecond,
		WriteTimeout: timeout,
		RequiredAcks: kafka.RequireOne,
	}
	return &MCPPublisher{writer: writer, logger: logger, timeout: timeout}
}

// NewMCPPublisherWithWriter is for tests.
func NewMCPPublisherWithWriter(w kafkaWriter, logger *zap.Logger) *MCPPublisher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MCPPublisher{writer: w, logger: logger, timeout: 2 * time.Second}
}

// PublishToolInvoked emits com.tas.activity.mcp.tool_invoked. subject is
// the server ID so frontend filters can pivot per-server.
func (p *MCPPublisher) PublishToolInvoked(ctx context.Context, tenantID, userID, requestID string, payload payloads.MCPToolInvoked) {
	if p == nil || p.writer == nil {
		return
	}
	sev := tasevents.SeverityInfo
	if !payload.Success {
		sev = tasevents.SeverityMedium
	}
	ce := tasevents.New(payloads.TypeMCPToolInvoked, ceSource,
		tasevents.WithTenant(tenantID, ""),
		tasevents.WithUser(userID),
		tasevents.WithRequest(requestID),
		tasevents.WithSubject(payload.ServerID),
		tasevents.WithSeverity(sev),
		tasevents.WithData(payload),
	)

	value, headers, err := kafkabind.Encode(ce)
	if err != nil {
		p.logger.Warn("mcp events: encode failed", zap.Error(err))
		return
	}

	writeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	msg := kafka.Message{
		Key:     kafkabind.MessageKey(ce),
		Value:   value,
		Headers: headers,
		Time:    ce.Time,
	}
	if err := p.writer.WriteMessages(writeCtx, msg); err != nil {
		p.logger.Warn("mcp events: publish failed", zap.Error(err))
	}
}

// Close flushes pending messages.
func (p *MCPPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
