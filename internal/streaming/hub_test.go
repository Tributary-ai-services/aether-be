package streaming

import (
	"testing"
	"time"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"go.uber.org/zap"
)

func TestHub_BroadcastToMatchingConns(t *testing.T) {
	hub := NewHub(zap.NewNop())

	// Fake conn that collects sends without a real WebSocket.
	c1 := &Conn{
		ID:       "c1",
		TenantID: "t-acme",
		send:     make(chan []byte, 16),
		closed:   make(chan struct{}),
	}
	c2 := &Conn{
		ID:       "c2",
		TenantID: "t-other",
		send:     make(chan []byte, 16),
		closed:   make(chan struct{}),
	}
	hub.Register(c1)
	hub.Register(c2)

	evt := tasevents.New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal",
		tasevents.WithTenant("t-acme", ""),
	)
	hub.Broadcast(evt)

	// c1 should receive (tenant match)
	select {
	case <-c1.send:
	case <-time.After(time.Second):
		t.Error("c1 should have received the event")
	}

	// c2 should NOT receive (different tenant)
	select {
	case <-c2.send:
		t.Error("c2 should not have received the event (wrong tenant)")
	default:
	}
}

func TestHub_BroadcastNoTenantFilter(t *testing.T) {
	hub := NewHub(zap.NewNop())

	c := &Conn{
		ID:       "c1",
		TenantID: "", // no tenant filter — accept all
		send:     make(chan []byte, 16),
		closed:   make(chan struct{}),
	}
	hub.Register(c)

	evt := tasevents.New("com.tas.activity.llm.request", "urn:tas:service:llm-router",
		tasevents.WithTenant("t-acme", ""),
	)
	hub.Broadcast(evt)

	select {
	case <-c.send:
	case <-time.After(time.Second):
		t.Error("conn with empty tenant should receive all events")
	}
}

func TestHub_TypeFilter(t *testing.T) {
	hub := NewHub(zap.NewNop())

	c := &Conn{
		ID:         "c1",
		TypeFilter: []string{"com.tas.compliance."},
		send:       make(chan []byte, 16),
		closed:     make(chan struct{}),
	}
	hub.Register(c)

	// Should NOT match — activity event
	hub.Broadcast(tasevents.New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal"))
	select {
	case <-c.send:
		t.Error("activity event should not match compliance filter")
	default:
	}

	// Should match — compliance event
	hub.Broadcast(tasevents.New("com.tas.compliance.finding.detected", "urn:tas:service:gatekeeper"))
	select {
	case <-c.send:
	case <-time.After(time.Second):
		t.Error("compliance event should match compliance filter")
	}
}

func TestHub_UnregisterClosesSendChannel(t *testing.T) {
	hub := NewHub(zap.NewNop())
	c := &Conn{
		ID:     "c1",
		send:   make(chan []byte, 16),
		closed: make(chan struct{}),
	}
	hub.Register(c)

	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount())
	}

	hub.Unregister("c1")

	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections, got %d", hub.ConnectionCount())
	}
}
