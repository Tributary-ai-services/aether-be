package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

type recordedInvoke struct {
	path   string
	bearer string
	args   map[string]interface{}
}

func newRecordingProxy(t *testing.T) (*httptest.Server, *[]recordedInvoke, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	calls := []recordedInvoke{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string                 `json:"id"`
			Params map[string]interface{} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		args, _ := req.Params["arguments"].(map[string]interface{})
		mu.Lock()
		calls = append(calls, recordedInvoke{path: r.URL.Path, bearer: r.Header.Get("TAS-Auth"), args: args})
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		}})
	}))
	return srv, &calls, &mu
}

func proxiedHandler(srv *httptest.Server) *MCPHandler {
	h := &MCPHandler{mcpClients: map[string]*services.MCPClientService{}, logger: zap.NewNop(), healthCache: map[string]healthCacheEntry{}}
	h.SetProxy(services.NewMCPProxyClientWithHTTP(srv.URL, srv.Client(), zap.NewNop()),
		services.StaticOrgTokenSource{"org-a": services.NewOrgToken("org-a", "11111111-1111-1111-1111-111111111111", "tas_qg_live_handler_test_a")},
		config.DefaultProxyServerIDs)
	return h
}

func invoke(h *MCPHandler, sc *models.SpaceContext, body map[string]any) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, _ := json.Marshal(body)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/invoke", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	if sc != nil {
		c.Set(middleware.SpaceContextKey, sc)
	}
	h.InvokeTool(c)
	return w
}

// A proxied call carries the org token, names the proxy's server id, and
// never forwards a caller-supplied _connection.
func TestMCPInvokeViaProxy_SendsOrgTokenAndStripsConnection(t *testing.T) {
	srv, calls, mu := newRecordingProxy(t)
	defer srv.Close()
	h := proxiedHandler(srv)

	sc := &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: "org-a", OrganizationID: "org-a", TenantID: "tenant_a"}
	w := invoke(h, sc, map[string]any{"server_id": "mcp-postgres", "tool": "query",
		"params": map[string]any{"sql": "SELECT 1", "_connection": map[string]any{"host": "attacker.example", "password": "x"}}})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) != 1 {
		t.Fatalf("proxy calls = %d", len(*calls))
	}
	got := (*calls)[0]
	if got.path != "/api/v1/federation/servers/postgres-mcp/invoke" || got.bearer != "tas_qg_live_handler_test_a" {
		t.Fatalf("sent %s with bearer %q", got.path, got.bearer)
	}
	if _, ok := got.args["_connection"]; ok {
		t.Fatal("caller-supplied _connection was forwarded through the proxy")
	}
}

// No token for the space: refused, and the proxy is never called.
func TestMCPInvokeViaProxy_RefusesWithoutOrgToken(t *testing.T) {
	srv, calls, mu := newRecordingProxy(t)
	defer srv.Close()
	h := proxiedHandler(srv)

	for name, sc := range map[string]*models.SpaceContext{
		"other org":      {SpaceType: models.SpaceTypeOrganization, SpaceID: "org-b", OrganizationID: "org-b", TenantID: "tenant_b"},
		"personal space": {SpaceType: models.SpaceTypePersonal, SpaceID: "u1", TenantID: "tenant_u1"},
		"no space":       nil,
	} {
		w := invoke(h, sc, map[string]any{"server_id": "mcp-context7", "tool": "resolve-library-id", "params": map[string]any{}})
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", name, w.Code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) != 0 {
		t.Fatalf("proxy called %d times", len(*calls))
	}
}

// Path selection: only federated servers go through the proxy, and a saved
// connection stays direct on every server but postgres.
func TestMCPUsesProxy(t *testing.T) {
	srv, _, _ := newRecordingProxy(t)
	defer srv.Close()
	h := proxiedHandler(srv)

	cases := []struct {
		server, conn string
		want         bool
	}{
		{"mcp-postgres", "", true},
		{"mcp-postgres", "db-1", true},
		{"mcp-minio", "", true},
		{"mcp-minio", "db-2", false},
		{"mcp-neo4j", "db-3", false},
		{"mcp-brave-search", "", false},
		{"mcp-napkin", "", false},
	}
	for _, tc := range cases {
		if _, got := h.usesProxy(tc.server, tc.conn); got != tc.want {
			t.Errorf("usesProxy(%s, %q) = %v, want %v", tc.server, tc.conn, got, tc.want)
		}
	}

	off := &MCPHandler{}
	if _, got := off.usesProxy("mcp-postgres", ""); got {
		t.Error("flag off must never use the proxy")
	}
}
