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

// fakeProxy emulates the parts of tas-mcp the console depends on (AIQG-88):
// it accepts only its delegate credential, asks "AIQG" (orgTenant +
// members) which tenant the named org is linked to and whether the user is a
// member, refuses otherwise, keys the stored connection by that tenant, and a
// server that requires a connection refuses a tenant with none (SEC-38). It
// records every call so tests can assert what aether-be sent.
type fakeProxy struct {
	t *testing.T

	credential string
	orgTenant  map[string]string          // Aether org -> AIQG tenant (the link)
	members    map[string]map[string]bool // tenant -> Keycloak sub -> member
	connRows   map[string]string          // tenant -> the rows its stored database returns (JSON)

	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Server  string
	Org     string
	User    string
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
		org, user := r.Header.Get("TAS-MCP-On-Behalf-Of-Org"), r.Header.Get("TAS-MCP-On-Behalf-Of-User")
		tenant := f.orgTenant[org]
		args, _ := req.Params["arguments"].(map[string]interface{})
		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{Server: parts[0], Org: org, User: user, Tenant: tenant, Method: req.Method, Args: args, Headers: r.Header.Clone()})
		f.mu.Unlock()

		switch {
		case r.Header.Get("TAS-MCP-Delegate-Token") != f.credential:
			http.Error(w, "invalid TAS-MCP-Delegate-Token", http.StatusForbidden)
			return
		case r.Header.Get("TAS-Auth") != "":
			http.Error(w, "send the delegate credential alone", http.StatusBadRequest)
			return
		case tenant == "":
			http.Error(w, "delegated call refused: org_not_linked", http.StatusForbidden)
			return
		case !f.members[tenant][user]:
			http.Error(w, "delegated call refused: not_a_member", http.StatusForbidden)
			return
		}
		if req.Method != "tools/call" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": map[string]any{"tools": []any{}}})
			return
		}
		rows, ok := f.connRows[tenant]
		if !ok {
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

func (f *fakeProxy) last() fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

const (
	orgA, orgB         = "org-a", "org-b"
	tenantA, tenantB   = "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	aetherTA, aetherTB = "tenant_a", "tenant_b"
	userA, userB       = "kc-sub-alice", "kc-sub-bob"
	delegateCred       = "dlg-test-credential-0123456789abcdef-SECRETVALUE"
)

type proxyFixture struct {
	fake  *fakeProxy
	svc   *ProxySQLService
	logs  *observer.ObservedLogs
	close func()
}

func newProxyFixture(t *testing.T, cred DelegateCredential) *proxyFixture {
	t.Helper()
	fake := &fakeProxy{t: t,
		credential: delegateCred,
		orgTenant:  map[string]string{orgA: tenantA, orgB: tenantB},
		members:    map[string]map[string]bool{tenantA: {userA: true}, tenantB: {userB: true}},
		connRows: map[string]string{
			tenantA: `[{"who":"tenant-a-db"}]`,
			tenantB: `[{"who":"tenant-b-db"}]`,
		},
	}
	srv := httptest.NewServer(fake.handler())
	core, logs := observer.New(zap.DebugLevel)
	log := zap.New(core)
	if cred == nil {
		cred = StaticDelegateCredential(delegateCred)
	}
	svc := NewProxySQLService(NewMCPProxyClientWithHTTP(srv.URL, srv.Client(), cred, log), "postgres-mcp", log)
	return &proxyFixture{fake: fake, svc: svc, logs: logs, close: srv.Close}
}

func orgSpace(org, aetherTenant, user string) *models.SpaceContext {
	return &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: org, OrganizationID: org, TenantID: aetherTenant, UserID: user}
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

// Each org's query names that org and the caller; the proxy runs it on that
// org's tenant's stored connection, and aether-be sends no connection itself.
func TestProxySQL_EachOrgGetsItsOwnTenantConnection(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()

	for _, tc := range []struct {
		org, aetherTenant, user, wantWho, wantTenant string
	}{
		{orgA, aetherTA, userA, "tenant-a-db", tenantA},
		{orgB, aetherTB, userB, "tenant-b-db", tenantB},
	} {
		ctx := WithSpaceContext(context.Background(), orgSpace(tc.org, tc.aetherTenant, tc.user))
		res, err := f.svc.ExecuteQuery(ctx, sharedDB(tc.aetherTenant), "SELECT current_database()", nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.org, err)
		}
		if got := res.Rows[0]["who"]; got != tc.wantWho {
			t.Fatalf("%s: query ran on %v, want %s", tc.org, got, tc.wantWho)
		}
		last := f.fake.last()
		if last.Org != tc.org || last.User != tc.user || last.Tenant != tc.wantTenant || last.Server != "postgres-mcp" {
			t.Fatalf("%s: sent org/user/tenant/server %q/%q/%q/%q", tc.org, last.Org, last.User, last.Tenant, last.Server)
		}
		if last.Headers.Get("TAS-Auth") != "" || last.Headers.Get("TAS-MCP-On-Behalf-Of-Tenant") != "" {
			t.Fatalf("%s: aether-be sent a TAS-Auth token or chose a tenant itself", tc.org)
		}
		if _, ok := last.Args["_connection"]; ok {
			t.Fatalf("%s: aether-be sent a _connection; the proxy alone injects it", tc.org)
		}
		if last.Args["sql"] != "SELECT current_database()" || len(last.Args) != 1 {
			t.Fatalf("%s: unexpected arguments %v", tc.org, last.Args)
		}
	}
}

// Refusals decided here never reach the proxy.
func TestProxySQL_LocalRefusals(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()

	cases := []struct {
		name   string
		sc     *models.SpaceContext
		db     *models.Database
		status int
	}{
		{"another tenant's row in my space", orgSpace(orgA, aetherTA, userA), sharedDB(aetherTB), http.StatusForbidden},
		{"personal space never borrows an org", &models.SpaceContext{SpaceType: models.SpaceTypePersonal, SpaceID: "u1", TenantID: aetherTA, UserID: userA}, sharedDB(aetherTA), http.StatusForbidden},
		{"org-type space with no owning Organization", &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: "s1", TenantID: aetherTA, UserID: userA}, sharedDB(aetherTA), http.StatusForbidden},
		{"no signed-in user", orgSpace(orgA, aetherTA, ""), sharedDB(aetherTA), http.StatusForbidden},
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

// Cross-tenant refusals the proxy decides (AIQG-88) come back as refusals,
// and no query runs: a user acting in an org they are not a member of, an
// unlinked org, and a wrong credential.
func TestProxySQL_ProxyRefusalsAreRefusals(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()

	// Bob is a member of B, not A.
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userB))
	if _, err := f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil); statusOf(err) != http.StatusForbidden {
		t.Fatalf("non-member: %v", err)
	}
	// An org AIQG has no link for.
	ctx = WithSpaceContext(context.Background(), orgSpace("org-unlinked", aetherTA, userA))
	if _, err := f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil); statusOf(err) != http.StatusForbidden {
		t.Fatalf("unlinked org: %v", err)
	}
	// A wrong credential is our misconfiguration: 503, never a fallback.
	bad := newProxyFixture(t, StaticDelegateCredential(delegateCred+"-wrong"))
	defer bad.close()
	bad.fake.credential = delegateCred
	ctx = WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userA))
	if _, err := bad.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil); statusOf(err) != http.StatusForbidden {
		t.Fatalf("wrong credential: %v", err)
	}
}

// Fail closed: a connection never copied to the shared store, or one the
// proxy cannot serve, is refused, and nothing falls back to a default source.
func TestProxySQL_FailsClosed(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userA))

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

	// No credential configured: refused before any call.
	none := newProxyFixture(t, StaticDelegateCredential(""))
	defer none.close()
	if _, err := none.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil); statusOf(err) != http.StatusServiceUnavailable || none.fake.callCount() != 0 {
		t.Fatalf("no credential: %v, calls %d", err, none.fake.callCount())
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
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userA))

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

// The delegate credential is never written to a log line, even on refusals.
func TestProxySQL_CredentialNeverLogged(t *testing.T) {
	f := newProxyFixture(t, nil)
	defer f.close()
	ctx := WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userA))
	_, _ = f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil)
	delete(f.fake.connRows, tenantA)
	_, _ = f.svc.ExecuteQuery(ctx, sharedDB(aetherTA), "SELECT 1", nil)
	_, _ = f.svc.ExecuteQuery(WithSpaceContext(context.Background(), orgSpace(orgA, aetherTA, userB)), sharedDB(aetherTA), "SELECT 1", nil)

	for _, e := range f.logs.All() {
		line := e.Message + fmt.Sprint(e.ContextMap())
		if strings.Contains(line, "SECRETVALUE") {
			t.Fatalf("log line leaks the credential: %s", line)
		}
	}
}

// The file credential reloads a rotated Secret and fails closed on a bad one.
func TestFileDelegateCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	cred := NewFileDelegateCredential(path)
	if _, err := cred.Token(); statusOf(err) != http.StatusServiceUnavailable {
		t.Fatalf("missing file: %v", err)
	}
	if err := os.WriteFile(path, []byte(delegateCred+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := cred.Token(); err != nil || tok != delegateCred {
		t.Fatalf("first read: %q %v", tok, err)
	}
	rotated := "dlg-rotated-credential-abcdef0123456789-NEWVALUE"
	_ = os.WriteFile(path, []byte(rotated), 0o600)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	if tok, err := cred.Token(); err != nil || tok != rotated {
		t.Fatalf("after rotation: %q %v", tok, err)
	}
	_ = os.WriteFile(path, []byte("short"), 0o600)
	future = future.Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	if _, err := cred.Token(); statusOf(err) != http.StatusServiceUnavailable || strings.Contains(err.Error(), "short") {
		t.Fatalf("bad file: %v", err)
	}
	// Still refused on the next read of the same bad file: nothing stale.
	if _, err := cred.Token(); err == nil {
		t.Fatal("a bad file must keep failing")
	}
	if NewDelegateCredential("", delegateCred).(StaticDelegateCredential) != StaticDelegateCredential(delegateCred) {
		t.Fatal("env fallback")
	}
}
