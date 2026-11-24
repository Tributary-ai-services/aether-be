package mcpmigrate

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
	orgA    = "org-a"
	orgB    = "org-b"
	aetherA = "tenant_a"
	aetherB = "tenant_b"
	// Live HAS_SPACE spaces each have their own tenant, never the org's
	// (AE-39): space_1772236887 is in tenant_1772236887.
	spaceA  = "space_a1"
	spaceTA = "tenant_space_a1"
	secretA = "pw-of-a-S3cret"
)

// fakeGraph holds Database nodes for two orgs. Candidates honours the org
// filter, as the Cypher does, and reports each node's space tenant as stored.
type fakeGraph struct {
	orgTenant map[string]string
	nodes     map[string]*Candidate // id -> node
	nodeOrg   map[string]string     // id -> org
	binds     int
}

func (g *fakeGraph) OrgTenant(_ context.Context, org string) (string, error) {
	return g.orgTenant[org], nil
}

func (g *fakeGraph) Candidates(_ context.Context, org, tenant string) ([]Candidate, error) {
	var out []Candidate
	for id, n := range g.nodes {
		if g.nodeOrg[id] == org && g.orgTenant[org] == tenant {
			out = append(out, *n)
		}
	}
	return out, nil
}

func (g *fakeGraph) Bind(_ context.Context, id, tenant, server, conn string) (bool, error) {
	n, ok := g.nodes[id]
	if !ok || n.TenantID != tenant {
		return false, nil
	}
	if n.MCPConnectionID != "" && n.MCPConnectionID != conn {
		return false, nil
	}
	n.MCPServerID, n.MCPConnectionID = server, conn
	g.binds++
	return true, nil
}

type fakeCreds map[string][2]string // secret name -> user, pass

func (f fakeCreds) Credentials(_ context.Context, c Candidate) (string, string, error) {
	v, ok := f[c.SecretName]
	if !ok {
		return "", "", fmt.Errorf("read secret %s", c.SecretName)
	}
	return v[0], v[1], nil
}

// fakeAIQG writes into the tenant of its token, like aiqg-dashboard-be, and
// keeps one live connection per (tenant, server).
type fakeAIQG struct {
	tenant  string
	links   []Link
	store   map[string][]Connection // tenant -> connections
	secrets map[string]string       // connection id -> DSN
	creates int
}

func (a *fakeAIQG) Links(context.Context) ([]Link, error) { return a.links, nil }
func (a *fakeAIQG) Connections(context.Context) ([]Connection, error) {
	return append([]Connection(nil), a.store[a.tenant]...), nil
}
func (a *fakeAIQG) Create(_ context.Context, in NewConnection) (*Connection, error) {
	for _, c := range a.store[a.tenant] {
		if c.ServerID == in.ServerID {
			return nil, fmt.Errorf("HTTP 409 connection_exists")
		}
	}
	a.creates++
	c := Connection{ID: fmt.Sprintf("conn-%d", a.creates), ServerID: in.ServerID, Label: in.Label, Kind: in.Kind, Params: in.Params}
	a.store[a.tenant] = append(a.store[a.tenant], c)
	a.secrets[c.ID] = in.Secret
	return &c, nil
}

func fixture(tokenTenant string) (*fakeGraph, fakeCreds, *fakeAIQG) {
	g := &fakeGraph{
		orgTenant: map[string]string{orgA: aetherA, orgB: aetherB},
		nodes: map[string]*Candidate{
			"db-a1": {ID: "db-a1", Name: "Orders", TenantID: spaceTA, SpaceID: spaceA, SpaceTenantID: spaceTA, Host: "orders-db.tas-mcp-servers.svc", Port: 5432, Database: "orders", SSLMode: "disable", SecretName: "db-credentials-a1"},
			"db-b1": {ID: "db-b1", Name: "B's", TenantID: aetherB, SpaceID: orgB, SpaceTenantID: aetherB, Host: "b-db.tas-mcp-servers.svc", Port: 5432, Database: "b", SecretName: "db-credentials-b1"},
		},
		nodeOrg: map[string]string{"db-a1": orgA, "db-b1": orgB},
	}
	creds := fakeCreds{"db-credentials-a1": {"orders_ro", secretA}, "db-credentials-b1": {"b_ro", "pw-of-b"}}
	api := &fakeAIQG{tenant: tokenTenant,
		links: []Link{
			{TenantID: tenantA, Kind: "org", AetherOrgID: orgA, AetherTenantID: aetherA},
			{TenantID: tenantB, Kind: "org", AetherOrgID: orgB, AetherTenantID: aetherB},
		},
		store: map[string][]Connection{}, secrets: map[string]string{}}
	return g, creds, api
}

func only(t *testing.T, r *Report, id string) Step {
	t.Helper()
	for _, s := range r.Steps {
		if s.DatabaseID == id {
			return s
		}
	}
	t.Fatalf("no step for %s in %+v", id, r.Steps)
	return Step{}
}

func TestDryRunChangesNothing(t *testing.T) {
	g, creds, api := fixture(tenantA)
	rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA})
	if err != nil {
		t.Fatal(err)
	}
	if s := only(t, rep, "db-a1"); s.Action != ActCreate || s.Applied {
		t.Fatalf("dry run step = %+v", s)
	}
	if api.creates != 0 || g.binds != 0 {
		t.Fatalf("dry run wrote: creates=%d binds=%d", api.creates, g.binds)
	}
}

func TestApplyCopiesOnlyTheTokensTenantAndIsIdempotent(t *testing.T) {
	g, creds, api := fixture(tenantA)
	rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	s := only(t, rep, "db-a1")
	if s.Action != ActCreate || !s.Applied || s.ConnectionID == "" {
		t.Fatalf("apply step = %+v", s)
	}
	if len(rep.Steps) != 1 {
		t.Fatalf("org B's connection was considered for tenant A: %+v", rep.Steps)
	}
	if len(api.store[tenantB]) != 0 {
		t.Fatal("wrote into tenant B")
	}
	// The DSN carries A's login and A's host, escaped.
	u, err := url.Parse(api.secrets[s.ConnectionID])
	if err != nil || u.Hostname() != "orders-db.tas-mcp-servers.svc" || u.Path != "/orders" || u.Query().Get("sslmode") != "disable" {
		t.Fatalf("DSN = %v (%v)", u, err)
	}
	if pw, _ := u.User.Password(); pw != secretA {
		t.Fatal("DSN does not carry the stored password")
	}
	if !strings.Contains(api.store[tenantA][0].Label, Marker("db-a1")) {
		t.Fatalf("label lacks marker: %q", api.store[tenantA][0].Label)
	}
	// The source is untouched apart from the binding.
	if n := g.nodes["db-a1"]; n.Host != "orders-db.tas-mcp-servers.svc" || n.SecretName != "db-credentials-a1" || n.MCPConnectionID != s.ConnectionID {
		t.Fatalf("node after apply: %+v", n)
	}

	// Rerun: nothing new.
	rep2, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if s2 := only(t, rep2, "db-a1"); s2.Action != ActDone {
		t.Fatalf("rerun step = %+v", s2)
	}
	if api.creates != 1 {
		t.Fatalf("rerun created again: %d", api.creates)
	}
}

// A crash between the AIQG write and the node write: the rerun recognizes the
// connection by its marker and only records the id.
func TestResumeAfterPartialFailure(t *testing.T) {
	g, creds, api := fixture(tenantA)
	api.store[tenantA] = []Connection{{ID: "conn-x", ServerID: "postgres-mcp", Label: Label(*g.nodes["db-a1"])}}
	rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := only(t, rep, "db-a1"); s.Action != ActBind || !s.Applied || s.ConnectionID != "conn-x" {
		t.Fatalf("resume step = %+v", s)
	}
	if api.creates != 0 {
		t.Fatal("resume created a duplicate")
	}
}

func TestOneConnectionPerServer(t *testing.T) {
	g, creds, api := fixture(tenantA)
	g.nodes["db-a2"] = &Candidate{ID: "db-a2", Name: "Analytics", TenantID: aetherA, SpaceID: orgA, SpaceTenantID: aetherA, Host: "an.tas-mcp-servers.svc", Database: "an", SecretName: "db-credentials-a1"}
	g.nodeOrg["db-a2"] = orgA

	rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rep.Steps {
		if s.Action != ActAmbiguous {
			t.Fatalf("two candidates, none chosen: %+v", s)
		}
	}
	if api.creates != 0 || !rep.Failed() {
		t.Fatal("ambiguous run wrote, or reported success")
	}

	rep, err = Run(context.Background(), g, creds, api, Options{TenantID: tenantA, DatabaseID: "db-a2", Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if only(t, rep, "db-a2").Action != ActCreate || only(t, rep, "db-a1").Action != ActNotSelected {
		t.Fatalf("chosen run: %+v", rep.Steps)
	}

	// An unrelated connection already exists for the server: conflict.
	g2, creds2, api2 := fixture(tenantA)
	api2.store[tenantA] = []Connection{{ID: "conn-y", ServerID: "postgres-mcp", Label: "added by hand in AIQG"}}
	rep, err = Run(context.Background(), g2, creds2, api2, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := only(t, rep, "db-a1"); s.Action != ActConflict || api2.creates != 0 {
		t.Fatalf("conflict step = %+v", s)
	}
}

func TestRefusesUnlinkedOrMismatchedTenant(t *testing.T) {
	g, creds, api := fixture("33333333-3333-3333-3333-333333333333")
	if _, err := Run(context.Background(), g, creds, api, Options{TenantID: api.tenant, Apply: true}); err == nil {
		t.Fatal("an unlinked tenant must be refused")
	}

	g, creds, api = fixture(tenantA)
	api.links[0].AetherTenantID = aetherB // link disagrees with the org
	if _, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true}); err == nil {
		t.Fatal("a link naming another Aether tenant must be refused")
	}
	if api.creates != 0 {
		t.Fatal("wrote despite refusal")
	}
}

func TestSkipsWhatPostgresMCPCannotCarryAndMissingLogins(t *testing.T) {
	g, creds, api := fixture(tenantA)
	g.nodes["db-a1"].Host = "bad host/with?chars"
	rep, _ := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if s := only(t, rep, "db-a1"); s.Action != ActUnsupported {
		t.Fatalf("got %+v", s)
	}

	g, creds, api = fixture(tenantA)
	g.nodes["db-a1"].SecretName = "missing"
	rep, _ = Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if s := only(t, rep, "db-a1"); s.Action != ActNoCreds || api.creates != 0 {
		t.Fatalf("got %+v", s)
	}
}

// Nothing in a report, applied or not, carries a password or DSN.
func TestReportNeverCarriesSecrets(t *testing.T) {
	g, creds, api := fixture(tenantA)
	rep, _ := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if dump := fmt.Sprintf("%+v %#v", rep, rep); strings.Contains(dump, secretA) || strings.Contains(dump, "postgres://") {
		t.Fatalf("report leaks a secret: %s", dump)
	}
}

func TestTenantFromToken(t *testing.T) {
	mk := func(payload string) string {
		return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
	}
	if got, err := TenantFromToken(mk(`{"tenant_id":"` + tenantA + `"}`)); err != nil || got != tenantA {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := TenantFromToken(mk(`{"sub":"x"}`)); err == nil {
		t.Fatal("a token without tenant_id must be refused")
	}
	if _, err := TenantFromToken("tas_qg_live_x"); err == nil {
		t.Fatal("a gateway token is not an AIQG user token")
	}
}

func TestLabelKeepsMarker(t *testing.T) {
	l := Label(Candidate{ID: "db-1", Name: strings.Repeat("x", 300)})
	if len(l) > 128 || !strings.HasSuffix(l, "("+Marker("db-1")+")") {
		t.Fatalf("label %q (%d)", l, len(l))
	}
}

// INFO: a connection that logs in with a SEC-44 superuser Secret, or points
// at shared TAS infrastructure, is refused before its login is even read.
func TestRefusesSuperuserSecretsAndSharedHosts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Candidate)
		allowed []string
		want    string
	}{
		{"SEC-44 secret", func(c *Candidate) { c.SecretName = "db-credentials-0d462710" }, nil, ActRefused},
		{"dbhub-secrets", func(c *Candidate) { c.SecretName = "dbhub-secrets" }, nil, ActRefused},
		{"tas-shared host", func(c *Candidate) { c.Host = "postgres-shared.tas-shared.svc.cluster.local" }, nil, ActRefused},
		{"bare shared name", func(c *Candidate) { c.Host = "postgres-shared" }, nil, ActRefused},
		{"tas-shared host cleared", func(c *Candidate) { c.Host = "orders.tas-shared.svc" }, []string{"orders.tas-shared.svc"}, ActCreate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, creds, api := fixture(tenantA)
			tc.mutate(g.nodes["db-a1"])
			if g.nodes["db-a1"].SecretName != "db-credentials-a1" {
				creds[g.nodes["db-a1"].SecretName] = [2]string{"tasuser", "superuser-pw"}
			}
			rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true, SharedHostsAllowed: tc.allowed})
			if err != nil {
				t.Fatal(err)
			}
			s := only(t, rep, "db-a1")
			if s.Action != tc.want {
				t.Fatalf("action = %s (%s), want %s", s.Action, s.Reason, tc.want)
			}
			if tc.want == ActRefused && (api.creates != 0 || !rep.Failed()) {
				t.Fatalf("refused connection was copied (%d) or the run reported success", api.creates)
			}
		})
	}
}

// A node whose tenant is not the tenant of the space it was found through is
// never copied, even if the graph query let it through (defense in depth).
func TestRefusesNodeOutsideItsSpaceTenant(t *testing.T) {
	g, creds, api := fixture(tenantA)
	g.nodes["db-a1"].TenantID = aetherB
	rep, err := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := only(t, rep, "db-a1"); s.Action != ActError || api.creates != 0 || g.binds != 0 {
		t.Fatalf("got %+v (creates %d, binds %d)", s, api.creates, g.binds)
	}
}

// A login that is the shared superuser is refused whichever Secret holds it.
func TestRefusesSuperuserLoginInAnySecret(t *testing.T) {
	for _, user := range []string{"tasuser", "postgres", "TASUSER"} {
		g, creds, api := fixture(tenantA)
		creds["db-credentials-a1"] = [2]string{user, "pw"}
		rep, _ := Run(context.Background(), g, creds, api, Options{TenantID: tenantA, Apply: true})
		if s := only(t, rep, "db-a1"); s.Action != ActRefused || api.creates != 0 {
			t.Fatalf("%s: got %+v", user, s)
		}
	}
}
