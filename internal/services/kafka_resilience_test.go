package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

func unreachableKafkaConfig() config.KafkaConfig {
	return config.KafkaConfig{
		Enabled: true,
		// Port 1 on the loopback interface: nothing listens, and the connect
		// fails immediately rather than hanging on a routing timeout.
		Brokers:     []string{"127.0.0.1:1"},
		TopicPrefix: "test",
	}
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewDefault()
	if err != nil {
		t.Fatalf("logger.NewDefault: %v", err)
	}
	return log
}

// TestNewKafkaServiceSurvivesUnreachableBroker guards the OPS-39 fix: a broker
// that is down at boot must not leave kafkaService nil for the life of the
// process, which permanently disabled publishing and made the health endpoint
// drop its "kafka" key entirely.
func TestNewKafkaServiceSurvivesUnreachableBroker(t *testing.T) {
	svc, err := NewKafkaService(unreachableKafkaConfig(), testLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaService returned error for an unreachable broker: %v", err)
	}
	if svc == nil {
		t.Fatal("NewKafkaService returned a nil service for an unreachable broker")
	}
	t.Cleanup(func() { _ = svc.Close() })

	if svc.Connected() {
		t.Error("Connected() reported true while the broker is unreachable")
	}

	// HealthCheck must report the real state, not the constructor's optimism.
	if err := svc.HealthCheck(context.Background()); err == nil {
		t.Error("HealthCheck returned nil for an unreachable broker")
	}
}

// TestPublishShortCircuitsWhenDisconnected checks that publishing while the
// broker is down fails fast rather than stalling the caller for the writer's
// 10s WriteTimeout — these calls sit in request handlers.
func TestPublishShortCircuitsWhenDisconnected(t *testing.T) {
	svc, err := NewKafkaService(unreachableKafkaConfig(), testLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	start := time.Now()
	err = svc.PublishEvent(context.Background(), Event{
		Type:    EventUserCreated,
		Subject: "user-1",
	})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrKafkaUnavailable) {
		t.Errorf("PublishEvent error = %v, want ErrKafkaUnavailable", err)
	}
	if elapsed > time.Second {
		t.Errorf("PublishEvent took %v while disconnected; expected an immediate return", elapsed)
	}

	start = time.Now()
	err = svc.PublishMessage(context.Background(), Message{Topic: "t", Value: "v"})
	elapsed = time.Since(start)

	if !errors.Is(err, ErrKafkaUnavailable) {
		t.Errorf("PublishMessage error = %v, want ErrKafkaUnavailable", err)
	}
	if elapsed > time.Second {
		t.Errorf("PublishMessage took %v while disconnected; expected an immediate return", elapsed)
	}
}

// TestCloseStopsConnectionMonitor ensures the background prober does not
// outlive the service (a goroutine leak per service instance).
func TestCloseStopsConnectionMonitor(t *testing.T) {
	svc, err := NewKafkaService(unreachableKafkaConfig(), testLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaService: %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-svc.monitorDone:
	case <-time.After(5 * time.Second):
		t.Fatal("connection monitor still running after Close")
	}
}
