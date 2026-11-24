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

// The migration's Cypher against a real Neo4j 5.15 (NEO4J_URI), seeded in
// the live shape (AE-39): the org has one tenant and every HAS_SPACE space
// has its own (space_1772236887 -> tenant_1772236887). Candidates are the
// org's PostgreSQL connections in the legacy org space (org tenant) and in a
// HAS_SPACE space (that space's tenant), and never a node in another org's
// space, a node whose tenant is not its space's, or a node in a space that
// another org also claims. Bind sets the three properties on an unbound node,
// is idempotent, and never rebinds a node to another connection.
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
	space, spaceTenant := "it-space-"+sfx, "it-spacetenant-"+sfx
	otherSpace, otherSpaceTenant := "it-space2-"+sfx, "it-spacetenant2-"+sfx
	shared, sharedTenant := "it-space3-"+sfx, "it-spacetenant3-"+sfx

	seed := `
		CREATE (o:Organization {id: $org, tenant_id: $tenant})
		CREATE (o2:Organization {id: $org2, tenant_id: $tenant2})
		CREATE (sp:Space {id: $space, tenant_id: $spacetenant})
		CREATE (sp2:Space {id: $space2, tenant_id: $spacetenant2})
		CREATE (sp3:Space {id: $space3, tenant_id: $spacetenant3})
		CREATE (o)-[:HAS_SPACE]->(sp)
		CREATE (o2)-[:HAS_SPACE]->(sp2)
		CREATE (o)-[:HAS_SPACE]->(sp3)
		CREATE (o2)-[:HAS_SPACE]->(sp3)
		CREATE (:Database {id: $p+'legacy', tenant_id: $tenant, space_id: $org, type: 'postgres', name: 'legacy', host: 'h1', port: 5432, database: 'd1'})
		CREATE (:Database {id: $p+'spaced', tenant_id: $spacetenant, space_id: $space, type: 'postgres', name: 'spaced', host: 'h2', port: 6543, database: 'd2'})
		CREATE (:Database {id: $p+'neo', tenant_id: $spacetenant, space_id: $space, type: 'neo4j', name: 'neo', host: 'h3', port: 7687, database: 'n'})
		CREATE (:Database {id: $p+'other', tenant_id: $tenant2, space_id: $org2, type: 'postgres', name: 'other', host: 'h4', port: 5432, database: 'd4'})
		CREATE (:Database {id: $p+'wrongtenant', tenant_id: $tenant2, space_id: $org, type: 'postgres', name: 'x', host: 'h5', port: 5432, database: 'd5'})
		CREATE (:Database {id: $p+'otherorgspace', tenant_id: $spacetenant2, space_id: $space2, type: 'postgres', name: 'o2s', host: 'h6', port: 5432, database: 'd6'})
		CREATE (:Database {id: $p+'orgtenantinspace', tenant_id: $tenant, space_id: $space, type: 'postgres', name: 'ots', host: 'h7', port: 5432, database: 'd7'})
		CREATE (:Database {id: $p+'foreigntenantinspace', tenant_id: $spacetenant2, space_id: $space, type: 'postgres', name: 'fts', host: 'h8', port: 5432, database: 'd8'})
		CREATE (:Database {id: $p+'claimedtwice', tenant_id: $spacetenant3, space_id: $space3, type: 'postgres', name: 'c2', host: 'h9', port: 5432, database: 'd9'})
	`
	params := map[string]any{"org": org, "org2": otherOrg, "tenant": tenant, "tenant2": otherTenant,
		"space": space, "spacetenant": spaceTenant, "space2": otherSpace, "spacetenant2": otherSpaceTenant,
		"space3": shared, "spacetenant3": sharedTenant, "p": sfx + "-"}
	if _, err := db.ExecuteQueryWithLogging(ctx, seed, params); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecuteQueryWithLogging(ctx, `MATCH (n) WHERE n.id STARTS WITH $p OR n.id IN [$org, $org2, $space, $space2, $space3] DETACH DELETE n`, params)
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
		if c.TenantID != c.SpaceTenantID {
			t.Fatalf("candidate outside its space's tenant: %+v", c)
		}
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != sfx+"-legacy" || ids[1] != sfx+"-spaced" {
		t.Fatalf("candidates = %v, want the legacy and HAS_SPACE postgres nodes only", ids)
	}
	for _, c := range cands {
		switch c.ID {
		case sfx + "-spaced":
			if c.Port != 6543 || c.Host != "h2" || c.Database != "d2" || c.TenantID != spaceTenant || c.SpaceID != space {
				t.Fatalf("fields not read: %+v", c)
			}
		case sfx + "-legacy":
			if c.TenantID != tenant || c.SpaceTenantID != tenant || c.SpaceID != org {
				t.Fatalf("legacy fields: %+v", c)
			}
		}
	}
	// The other org sees only its own legacy node: never ours, and never the
	// space both orgs claim.
	if cands2, err := g.Candidates(ctx, otherOrg, otherTenant); err != nil {
		t.Fatal(err)
	} else {
		var ids2 []string
		for _, c := range cands2 {
			ids2 = append(ids2, c.ID)
		}
		sort.Strings(ids2)
		if len(ids2) != 2 || ids2[0] != sfx+"-other" || ids2[1] != sfx+"-otherorgspace" {
			t.Fatalf("other org candidates = %v", ids2)
		}
	}
	// A tenant that is not the org's yields nothing.
	if c, _ := g.Candidates(ctx, org, otherTenant); len(c) != 0 {
		t.Fatalf("candidates under the wrong org tenant: %+v", c)
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
