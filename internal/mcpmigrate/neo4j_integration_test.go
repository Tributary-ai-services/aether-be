package mcpmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sort"
	"testing"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// The migration's Cypher against a real Neo4j 5.15 (NEO4J_URI): candidates
// are the org's PostgreSQL connections in the legacy org space and in a
// HAS_SPACE space, in the org's tenant only; Bind sets the three properties
// on an unbound node, is idempotent, and never rebinds a node to another
// connection.
func TestNeo4jGraph_Integration(t *testing.T) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" || testing.Short() {
		t.Skip("NEO4J_URI not set (or -short)")
	}
	log, err := logger.New(logger.Config{Level: "error", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	pw := os.Getenv("NEO4J_PASSWORD")
	if pw == "" {
		pw = "password"
	}
	db, err := database.NewNeo4jClient(config.DatabaseConfig{URI: uri, Username: "neo4j", Password: pw, Database: "neo4j", MaxConns: 5}, log)
	if err != nil {
		t.Skipf("Neo4j unavailable: %v", err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = db.Close(ctx) })

	b := make([]byte, 4)
	_, _ = rand.Read(b)
	sfx := hex.EncodeToString(b)
	org, otherOrg := "it-org-"+sfx, "it-org2-"+sfx
	tenant, otherTenant := "it-tenant-"+sfx, "it-tenant2-"+sfx
	space := "it-space-" + sfx

	seed := `
		CREATE (o:Organization {id: $org, tenant_id: $tenant})
		CREATE (o2:Organization {id: $org2, tenant_id: $tenant2})
		CREATE (sp:Space {id: $space, tenant_id: $tenant})
		CREATE (o)-[:HAS_SPACE]->(sp)
		CREATE (:Database {id: $p+'legacy', tenant_id: $tenant, space_id: $org, type: 'postgres', name: 'legacy', host: 'h1', port: 5432, database: 'd1'})
		CREATE (:Database {id: $p+'spaced', tenant_id: $tenant, space_id: $space, type: 'postgres', name: 'spaced', host: 'h2', port: 6543, database: 'd2'})
		CREATE (:Database {id: $p+'neo', tenant_id: $tenant, space_id: $org, type: 'neo4j', name: 'neo', host: 'h3', port: 7687, database: 'n'})
		CREATE (:Database {id: $p+'other', tenant_id: $tenant2, space_id: $org2, type: 'postgres', name: 'other', host: 'h4', port: 5432, database: 'd4'})
		CREATE (:Database {id: $p+'wrongtenant', tenant_id: $tenant2, space_id: $org, type: 'postgres', name: 'x', host: 'h5', port: 5432, database: 'd5'})
	`
	params := map[string]any{"org": org, "org2": otherOrg, "tenant": tenant, "tenant2": otherTenant, "space": space, "p": sfx + "-"}
	if _, err := db.ExecuteQueryWithLogging(ctx, seed, params); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecuteQueryWithLogging(ctx, `MATCH (n) WHERE n.id STARTS WITH $p OR n.id IN [$org, $org2, $space] DETACH DELETE n`, params)
	})

	g := Neo4jGraph{DB: db}
	if got, err := g.OrgTenant(ctx, org); err != nil || got != tenant {
		t.Fatalf("OrgTenant = %q, %v", got, err)
	}
	if got, _ := g.OrgTenant(ctx, "it-missing-"+sfx); got != "" {
		t.Fatalf("missing org tenant = %q", got)
	}
	cands, err := g.Candidates(ctx, org, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cands {
		ids = append(ids, c.ID)
		if c.TenantID != tenant {
			t.Fatalf("candidate from another tenant: %+v", c)
		}
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != sfx+"-legacy" || ids[1] != sfx+"-spaced" {
		t.Fatalf("candidates = %v, want the legacy and HAS_SPACE postgres nodes only", ids)
	}
	for _, c := range cands {
		if c.ID == sfx+"-spaced" && (c.Port != 6543 || c.Host != "h2" || c.Database != "d2") {
			t.Fatalf("fields not read: %+v", c)
		}
	}

	id := sfx + "-legacy"
	if ok, err := g.Bind(ctx, id, tenant, "postgres-mcp", "conn-1"); err != nil || !ok {
		t.Fatalf("bind = %v, %v", ok, err)
	}
	if ok, _ := g.Bind(ctx, id, tenant, "postgres-mcp", "conn-1"); !ok {
		t.Fatal("rebinding to the same connection must be idempotent")
	}
	if ok, _ := g.Bind(ctx, id, tenant, "postgres-mcp", "conn-2"); ok {
		t.Fatal("a node bound to one connection must not be rebound to another")
	}
	if ok, _ := g.Bind(ctx, id, otherTenant, "postgres-mcp", "conn-1"); ok {
		t.Fatal("Bind must be constrained to the tenant")
	}
	res, err := db.ExecuteQueryWithLogging(ctx, `MATCH (d:Database {id: $id}) WHERE d.tenant_id = $tenant
		RETURN d.mcp_server_id AS s, d.mcp_connection_id AS c, d.mcp_migrated_at IS NOT NULL AS at, d.host AS host`,
		map[string]any{"id": id, "tenant": tenant})
	if err != nil || len(res.Records) != 1 {
		t.Fatal(err)
	}
	r := res.Records[0]
	s, _ := r.Get("s")
	c, _ := r.Get("c")
	at, _ := r.Get("at")
	host, _ := r.Get("host")
	if s != "postgres-mcp" || c != "conn-1" || at != true || host != "h1" {
		t.Fatalf("node after bind: s=%v c=%v at=%v host=%v", s, c, at, host)
	}
	// Bound candidates now report their binding.
	cands, _ = g.Candidates(ctx, org, tenant)
	for _, cand := range cands {
		if cand.ID == id && cand.MCPConnectionID != "conn-1" {
			t.Fatalf("candidate binding not read: %+v", cand)
		}
	}
}
