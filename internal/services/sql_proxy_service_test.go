package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// fakeProxy emulates the parts of tas-mcp the console depends on: the tenant
// comes from TAS-Auth only, the stored connection is keyed by (tenant,
// server), and a server that requires a connection refuses a tenant with
// none (SEC-38). It records every call so tests can assert what aether-be sent.
type fakeProxy struct {
	t *testing.T

	tokenTenant map[string]string // bearer -> tenant
	connRows    map[string]string // tenant -> the rows its stored database returns (JSON)

	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Server  string
	Bearer  string
	Tenant  string
	Method  string
	Args    map[string]interface{}
	Headers http.Header
}

func (f *fakeProxy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/federation/servers/"), "/")
		if len(parts) != 2 || parts[1] != "invoke" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			ID     string                 `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		bearer := r.Header.Get("TAS-Auth")
		tenant := f.tokenTenant[bearer]
		args, _ := req.Params["arguments"].(map[string]interface{})
		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{Server: parts[0], Bearer: bearer, Tenant: tenant, Method: req.Method, Args: args, Headers: r.Header.Clone()})
		f.mu.Unlock()

		if req.Method != "tools/call" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": map[string]any{"tools": []any{}}})
			return
		}
		rows, ok := f.connRows[tenant]
		if tenant == "" || !ok {
			// SEC-38: postgres-mcp refuses a call without a stored connection.
			http.Error(w, "forbidden: this server requires a stored connection", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": rows}},
		}})
	})
}

func (f *fakeProxy) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const (
	orgA, orgB         = "org-a", "org-b"
	tenantA, tenantB   = "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	aetherTA, aetherTB = "tenant_a", "tenant_b"
	tokenA             = "tas_qg_live_AAAAsecretAAAA"
	tokenB             = "tas_qg_live_BBBBsecretBBBB"
)

type proxyFixture struct {
	fake  *fakeProxy
	svc   *ProxySQLService
	logs  *observer.ObservedLogs
	close func()
}

func newProxyFixture(t *testing.T, tokens OrgTokenSource) *proxyFixture {
	t.Helper()
	fake := &fakeProxy{t: t,
		tokenTenant: map[string]string{tokenA: tenantA, tokenB: tenantB},
		connRows: map[string]string{
			tenantA: `[{"who":"tenant-a-db"}]`,
			tenantB: `[{"who":"tenant-b-db"}]`,
		},
	}
	srv := httptest.NewServer(fake.handler())
	core, logs := observer.New(zap.DebugLevel)
	log := zap.New(core)
	if tokens == nil {
		tokens = StaticOrgTokenSource{
			orgA: NewOrgToken(orgA, tenantA, tokenA),
			orgB: NewOrgToken(orgB, tenantB, tokenB),
		}
	}
	svc := NewProxySQLService(NewMCPProxyClientWithHTTP(srv.URL, srv.Client(), log), tokens, "postgres-mcp", log)
	return &proxyFixture{fake: fake, svc: svc, logs: logs, close: srv.Close}
}

func orgSpace(org, aetherTenant string) *models.SpaceContext {
	return &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: org, OrganizationID: org, TenantID: aetherTenant, UserID: "u1"}
}

func sharedDB(aetherTenant string) *models.Database {
	return &models.Database{ID: "db-" + aetherTenant, TenantID: aetherTenant, Type: models.DatabaseTypePostgres,
		Host: "typed-by-user.example", MCPServerID: "postgres-mcp", MCPConnectionID: "conn-" + aetherTenant, MaxRows: 1000}
}

func statusOf(err error) int {
	if apiErr, ok := err.(*errors.APIError); ok {
		return apiErr.StatusCode
	}
	return 0
}

// Each org's query runs on its own tenant's stored connection, chosen by the
// proxy from the token, and aether-be sends no connection of its own.
func TestProxySQL_EachOrgGetsItsOwnTenantConnection(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()

	for _, tc := range []struct {
		org, aetherTenant, wantWho, wantBearer, wantTenant string
	}{
		{orgA, aetherTA, "tenant-a-db", tokenA, tenantA},
		{orgB, aetherTB, "tenant-b-db", tokenB, tenantB},
	} {
		ctx := WithSpaceContext(context.Background(), orgSpace(tc.org, tc.aetherTenant))
		res, err := f.svc.ExecuteQuery(ctx, sharedDB(tc.aetherTenant), "SELECT current_database()", nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.org, err)
		}
		if got := res.Rows[0]["who"]; got != tc.wantWho {
			t.Fatalf("%s: query ran on %v, want %s", tc.org, got, tc.wantWho)
		}
		last := f.fake.calls[len(f.fake.calls)-1]
		if last.Bearer != tc.wantBearer || last.Tenant != tc.wantTenant || last.Server != "postgres-mcp" {
			t.Fatalf("%s: sent bearer/tenant/server %q/%q/%q", tc.org, last.Bearer, last.Tenant, last.Server)
		}
		if _, ok := last.Args["_connection"]; ok {
			t.Fatalf("%s: aether-be sent a _connection; the proxy alone injects it", tc.org)
		}
		if last.Args["sql"] != "SELECT current_database()" || len(last.Args) != 1 {
			t.Fatalf("%s: unexpected arguments %v", tc.org, last.Args)
		}
	}
}

// Cross-tenant refusals: none of these reaches the proxy.
func TestProxySQL_CrossTenantRefusals(t *testing.T) {
	f := newProxyFixture(t, StaticOrgTokenSource{orgB: NewOrgToken(orgB, tenantB, tokenB)})
	defer f.close()

	cases := []struct {
		name   string
		sc     *models.SpaceContext
		db     *models.Database
		status int
	}{
		{"another tenant's row in my space", orgSpace(orgA, aetherTA), sharedDB(aetherTB), http.StatusForbidden},
		{"my org has no token, only B does", orgSpace(orgA, aetherTA), sharedDB(aetherTA), http.StatusForbidden},
		{"personal space never borrows an org token", &models.SpaceContext{SpaceType: models.SpaceTypePersonal, SpaceID: "u1", TenantID: aetherTA}, sharedDB(aetherTA), http.StatusForbidden},
		{"org-type space with no owning Organization", &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: "s1", TenantID: aetherTA}, sharedDB(aetherTA), http.StatusForbidden},
		{"no space context at all", nil, sharedDB(aetherTA), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.sc != nil {
				ctx = WithSpaceContext(ctx, tc.sc)
			}
			_, err := f.svc.ExecuteQuery(ctx, tc.db, "SELECT 1", nil)
			if statusOf(err) != tc.status {
				t.Fatalf("got %v (status %d), want %d", err, statusOf(err), tc.status)
			}
		})
	}
	if n := f.fake.callCount(); n != 0 {
		t.Fatalf("proxy called %d times for refused requests", n)
	}
}

// Fail closed: a connection never copied to the shared store, or one the
// proxy cannot serve, is refused, and nothing falls back to a default source.
func TestProxySQL_FailsClosed(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA))

	notShared := sharedDB(aetherTA)
	notShared.MCPConnectionID = ""
	if _, err := f.svc.ExecuteQuery(ctx, notShared, "SELECT 1", nil); statusOf(err) != http.StatusConflict {
		t.Fatalf("unmigrated connection: got %v, want 409", err)
	}

	otherServer := sharedDB(aetherTA)
	otherServer.MCPServerID = "dbhub"
	if _, err := f.svc.ExecuteQuery(ctx, otherServer, "SELECT 1", nil); statusOf(err) != http.StatusConflict {
		t.Fatalf("connection stored for another server: got %v, want 409", err)
	}

	mysql := sharedDB(aetherTA)
	mysql.Type = models.DatabaseTypeMySQL
	if _, err := f.svc.ExecuteQuery(ctx, mysql, "SELECT 1", nil); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("mysql: got %v, want 400", err)
	}
	if _, err := f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT $1", []any{1}); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("bind params: got %v, want 400", err)
	}
	if n := f.fake.callCount(); n != 0 {
		t.Fatalf("proxy called %d times for refused requests", n)
	}

	// The tenant's stored connection was deleted in AIQG: the proxy refuses
	// (SEC-38) and the console reports it.
	delete(f.fake.connRows, tenantA)
	if _, err := f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil); statusOf(err) != http.StatusForbidden {
		t.Fatalf("no stored connection: got %v, want 403", err)
	}
}

// With the proxy on, DatabaseService never routes SQL to dbhub.
func TestDatabaseService_SQLBackendNeverDBHubWithProxy(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	s := &DatabaseService{dbhub: &DBHubService{enabled: true, baseURL: "http://dbhub.invalid"}}
	if _, ok := s.sql().(*DBHubService); !ok {
		t.Fatal("flag off: SQL should go to dbhub as before")
	}
	s.SetSQLProxy(f.svc)
	if _, ok := s.sql().(*ProxySQLService); !ok {
		t.Fatal("flag on: SQL must go to the proxy")
	}
}

// Schema, tables and columns go through the same guarded path.
func TestProxySQL_SchemaPathsUseQueryTool(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	f.fake.connRows[tenantA] = `[{"schema":"public","name":"orders","row_count":"42"}]`
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA))

	tables, err := f.svc.GetTables(ctx, sharedDB(aetherTA))
	if err != nil || len(tables) != 1 || tables[0].Name != "orders" || tables[0].RowCount != 42 {
		t.Fatalf("tables = %+v, %v", tables, err)
	}
	if _, err := f.svc.GetTableColumns(ctx, sharedDB(aetherTA), "public.orders'; DROP TABLE x; --"); statusOf(err) != http.StatusBadRequest {
		t.Fatalf("injected table name: got %v, want 400", err)
	}
	f.fake.connRows[tenantA] = `[{"name":"id","type":"uuid","nullable":false,"default":null,"primary_key":true}]`
	cols, err := f.svc.GetTableColumns(ctx, sharedDB(aetherTA), "public.orders")
	if err != nil || len(cols) != 1 || !cols[0].PrimaryKey {
		t.Fatalf("columns = %+v, %v", cols, err)
	}
	for _, c := range f.fake.calls {
		if c.Method != "tools/call" || c.Args["sql"] == nil {
			t.Fatalf("schema call did not use the query tool: %+v", c)
		}
	}
}

// The org token is never written to a log line, even on a refusal.
func TestProxySQL_TokenNeverLogged(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA))
	_, _ = f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil)
	delete(f.fake.connRows, tenantA)
	_, _ = f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil)

	for _, e := range f.logs.All() {
		line := e.Message + fmt.Sprint(e.ContextMap())
		if strings.Contains(line, tokenA) || strings.Contains(line, "secret") {
			t.Fatalf("log line leaks the token: %s", line)
		}
	}
	tok := NewOrgToken(orgA, tenantA, tokenA)
	for _, s := range []string{fmt.Sprint(tok), fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, tok)} {
		if strings.Contains(s, tokenA) {
			t.Fatalf("OrgToken formats its secret: %s", s)
		}
	}
}

func writeTokens(t *testing.T, path string, orgs map[string][2]string) {
	t.Helper()
	m := map[string]map[string]string{}
	for org, v := range orgs {
		m[org] = map[string]string{"aiqg_tenant_id": v[0], "token": v[1]}
	}
	b, _ := json.Marshal(map[string]any{"orgs": m})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The file source reloads a rotated Secret and fails closed on a bad one.
func TestFileOrgTokenSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	src := NewFileOrgTokenSource(path)
	sc := orgSpace(orgA, aetherTA)

	if _, err := src.TokenFor(sc); statusOf(err) != http.StatusServiceUnavailable {
		t.Fatalf("missing file: %v", err)
	}

	writeTokens(t, path, map[string][2]string{orgA: {tenantA, tokenA}})
	tok, err := src.TokenFor(sc)
	if err != nil || tok.Bearer() != tokenA || tok.AIQGTenantID != tenantA {
		t.Fatalf("first load: %v %v", tok, err)
	}
	if _, err := src.TokenFor(orgSpace(orgB, aetherTB)); statusOf(err) != http.StatusForbidden {
		t.Fatalf("unmapped org: %v", err)
	}

	// Rotation: a new value is picked up without a restart.
	rotated := "tas_qg_live_rotated_value_000"
	writeTokens(t, path, map[string][2]string{orgA: {tenantA, rotated}})
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	if tok, err := src.TokenFor(sc); err != nil || tok.Bearer() != rotated {
		t.Fatalf("after rotation: %v %v", tok, err)
	}

	// A bad entry refuses every org, and the error never quotes a token.
	writeTokens(t, path, map[string][2]string{orgA: {tenantA, "not-a-gateway-token-secretvalue"}})
	future = future.Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	_, err = src.TokenFor(sc)
	if statusOf(err) != http.StatusServiceUnavailable || strings.Contains(err.Error(), "secretvalue") {
		t.Fatalf("bad entry: %v", err)
	}
}
