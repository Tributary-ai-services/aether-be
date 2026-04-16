// Package streaming provides a Kafka-backed event hub that fans CloudEvents
// out to WebSocket clients for the Live Streams UI panel.
package streaming

import (
	"encoding/json"
	"sync"
	"time"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Conn wraps a WebSocket connection with tenant-scoped filtering.
type Conn struct {
	ID       string
	TenantID string
	TypeFilter []string // if non-empty, only deliver events matching these type prefixes
	WS       *websocket.Conn
	send     chan []byte
	closed   chan struct{}
	once     sync.Once
}

func NewConn(id, tenantID string, ws *websocket.Conn, typeFilter []string) *Conn {
	return &Conn{
		ID:         id,
		TenantID:   tenantID,
		TypeFilter: typeFilter,
		WS:         ws,
		send:       make(chan []byte, 256),
		closed:     make(chan struct{}),
	}
}

func (c *Conn) Close() {
	c.once.Do(func() { close(c.closed) })
}

// WritePump drains the send channel to the WebSocket. Blocks until the
// connection closes. Caller should run this in a goroutine.
func (c *Conn) WritePump() {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	defer c.WS.Close()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.WS.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			c.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WS.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-pingTicker.C:
			c.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WS.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

// ReadPump reads client messages (for keepalive). Blocks until the connection
// closes. Caller should run this in a goroutine.
func (c *Conn) ReadPump() {
	defer c.Close()
	c.WS.SetReadLimit(512)
	c.WS.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.WS.SetPongHandler(func(string) error {
		c.WS.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := c.WS.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Conn) accepts(e tasevents.Event) bool {
	if c.TenantID != "" && e.TenantID != "" && c.TenantID != e.TenantID {
		return false
	}
	if len(c.TypeFilter) == 0 {
		return true
	}
	for _, prefix := range c.TypeFilter {
		if len(e.Type) >= len(prefix) && e.Type[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// Hub manages WebSocket connections and broadcasts CloudEvents.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn
	log   *zap.Logger
}

func NewHub(log *zap.Logger) *Hub {
	return &Hub{
		conns: make(map[string]*Conn),
		log:   log,
	}
}

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	h.conns[c.ID] = c
	h.mu.Unlock()
	h.log.Info("streaming connection registered",
		zap.String("conn_id", c.ID),
		zap.String("tenant_id", c.TenantID),
		zap.Int("total", h.ConnectionCount()),
	)
}

func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	if c, ok := h.conns[id]; ok {
		c.Close()
		close(c.send)
		delete(h.conns, id)
	}
	h.mu.Unlock()
}

// Broadcast sends an event to all matching connections.
func (h *Hub) Broadcast(e tasevents.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		h.log.Error("failed to marshal event for broadcast", zap.Error(err))
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, c := range h.conns {
		if !c.accepts(e) {
			continue
		}
		select {
		case c.send <- data:
		default:
			h.log.Warn("dropping event for slow connection",
				zap.String("conn_id", c.ID),
				zap.String("event_type", e.Type),
			)
		}
	}
}

func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}
