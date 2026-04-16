package streaming

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-shared/go-events/topics"
)

// ConsumerConfig configures the Kafka consumer.
type ConsumerConfig struct {
	Brokers []string
	GroupID string
	Topics  []string
}

// DefaultConsumerConfig returns config for the live-feed consumer group.
func DefaultConsumerConfig(brokers []string) ConsumerConfig {
	return ConsumerConfig{
		Brokers: brokers,
		GroupID: "aether-be-live-feed",
		Topics:  topics.ConsumerTopics(),
	}
}

// Consumer reads CloudEvents (and legacy envelopes during migration) from
// Kafka and broadcasts them to the Hub. It runs in its own goroutine; call
// Stop to shut it down.
type Consumer struct {
	hub    *Hub
	cfg    ConsumerConfig
	log    *zap.Logger
	cancel context.CancelFunc
	done   chan struct{}
}

func NewConsumer(hub *Hub, cfg ConsumerConfig, log *zap.Logger) *Consumer {
	return &Consumer{
		hub:  hub,
		cfg:  cfg,
		log:  log,
		done: make(chan struct{}),
	}
}

// Start begins consuming in a background goroutine.
func (c *Consumer) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop signals the consumer to stop and waits for it to finish.
func (c *Consumer) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
}

func (c *Consumer) run(ctx context.Context) {
	defer close(c.done)

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        c.cfg.Brokers,
		GroupID:        c.cfg.GroupID,
		GroupTopics:    c.cfg.Topics,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        500 * time.Millisecond,
		StartOffset:    kafka.LastOffset,
		CommitInterval: time.Second,
	})
	defer reader.Close()

	c.log.Info("streaming consumer started",
		zap.Strings("topics", c.cfg.Topics),
		zap.String("group_id", c.cfg.GroupID),
	)

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.log.Info("streaming consumer shutting down")
				return
			}
			c.log.Error("streaming consumer fetch error", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		evt, err := ParseMessage(msg)
		if err != nil {
			c.log.Warn("streaming consumer parse error",
				zap.String("topic", msg.Topic),
				zap.Error(err),
			)
			reader.CommitMessages(ctx, msg)
			continue
		}

		c.hub.Broadcast(evt)

		if err := reader.CommitMessages(ctx, msg); err != nil {
			c.log.Warn("streaming consumer commit error", zap.Error(err))
		}
	}
}
