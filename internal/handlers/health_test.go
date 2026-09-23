package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewDefault()
	if err != nil {
		t.Fatalf("logger.NewDefault: %v", err)
	}
	return log
}

// TestLivenessCheckIsUnconditional guards the OPS-39 fix: /health/live must
// answer 200 with no dependency consulted, because the kubelet's response to
// a failed liveness probe is to kill the container, which cannot repair a
// dependency.
func TestLivenessCheckIsUnconditional(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Every dependency nil, every optional dependency configured: the worst
	// case the process can be in and still be running.
	h := NewHealthHandler(nil, nil, nil, true, true, testLogger(t))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health/live", nil)

	h.LivenessCheck(c)

	if w.Code != http.StatusOK {
		t.Fatalf("LivenessCheck returned %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["status"] != "alive" {
		t.Errorf("status = %v, want alive", body["status"])
	}
}

// TestReadinessVerdict pins the readiness policy: only the gating dependency
// produces a 503. An unhealthy optional dependency reports "degraded" with a
// 200 so the replica stays in the Service's endpoints.
func TestReadinessVerdict(t *testing.T) {
	tests := []struct {
		name       string
		ready      bool
		degraded   bool
		wantStatus string
		wantCode   int
	}{
		{"all healthy", true, false, "ready", http.StatusOK},
		{"optional dependency down", true, true, "degraded", http.StatusOK},
		{"neo4j down", false, false, "not_ready", http.StatusServiceUnavailable},
		{"neo4j down and optional down", false, true, "not_ready", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code := readinessVerdict(tt.ready, tt.degraded)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("readinessVerdict(%v, %v) = (%q, %d), want (%q, %d)",
					tt.ready, tt.degraded, status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

// TestConfiguredButUninitializedDependenciesAreReported guards the second half
// of OPS-39: a dependency that failed to initialize used to vanish from the
// response, so the endpoint reported healthy because it had stopped looking.
func TestConfiguredButUninitializedDependenciesAreReported(t *testing.T) {
	h := NewHealthHandler(nil, nil, nil, true, true, testLogger(t))

	kafka := h.checkKafka(context.Background())
	if kafka.Status != "unhealthy" {
		t.Errorf("checkKafka status = %q, want unhealthy", kafka.Status)
	}
	if kafka.Error == "" {
		t.Error("checkKafka returned no error text for an uninitialized service")
	}

	storage := h.checkStorage(context.Background())
	if storage.Status != "unhealthy" {
		t.Errorf("checkStorage status = %q, want unhealthy", storage.Status)
	}
	if storage.Error == "" {
		t.Error("checkStorage returned no error text for an uninitialized service")
	}
}
