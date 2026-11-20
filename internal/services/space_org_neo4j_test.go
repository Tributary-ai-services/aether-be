package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// Plan #22 AB-13: an organization space's context carries the Organization
// that owns it through HAS_SPACE, and a space with no such edge carries none.
//
// These run against a real Neo4j and skip unless NEO4J_URI is set (CI's test
// job sets it to its neo4j service). They create uniquely named nodes and
// DETACH DELETE them afterwards; never point NEO4J_URI at a shared database.

func neo4jForTest(t *testing.T) (*database.Neo4jClient, *logger.Logger) {
	t.Helper()
	uri := os.Getenv("NEO4J_URI")
	if uri == "" || testing.Short() {
		t.Skip("NEO4J_URI not set (or -short); skipping Neo4j-backed test")
	}
	log, err := logger.New(logger.Config{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	cfg := config.DatabaseConfig{
		URI:      uri,
		Username: envOr("NEO4J_USERNAME", "neo4j"),
		Password: envOr("NEO4J_PASSWORD", "password"),
		Database: envOr("NEO4J_DATABASE", "neo4j"),
		MaxConns: 5,
	}
	client, err := database.NewNeo4jClient(cfg, log)
	if err != nil {
		t.Skipf("Neo4j unavailable at %s: %v", uri, err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client, log
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// orgSpaceFixture builds: a user who OWNS two organization-type spaces, one
// linked to an Organization by HAS_SPACE and one legacy space with no edge.
type orgSpaceFixture struct {
	keycloakID, orgID, linkedSpace, unlinkedSpace string
}

func seedOrgSpaces(t *testing.T, client *database.Neo4jClient) orgSpaceFixture {
	t.Helper()
	suffix := uuid.NewString()
	f := orgSpaceFixture{
		keycloakID:    "ab13-kc-" + suffix,
		orgID:         "ab13-org-" + suffix,
		linkedSpace:   "ab13-space-linked-" + suffix,
		unlinkedSpace: "ab13-space-unlinked-" + suffix,
	}
	ctx := context.Background()
	_, err := client.ExecuteQueryWithLogging(ctx, `
		CREATE (u:User {id: $kc, keycloak_id: $kc})
		CREATE (o:Organization {id: $org, name: 'AB-13 test org'})
		CREATE (s1:Space {id: $linked, name: 'linked', space_type: 'organization', tenant_id: 'tenant_' + $linked})
		CREATE (s2:Space {id: $unlinked, name: 'unlinked', space_type: 'organization', tenant_id: 'tenant_' + $unlinked})
		CREATE (u)-[:OWNS]->(s1), (u)-[:OWNS]->(s2), (o)-[:HAS_SPACE]->(s1)
	`, map[string]interface{}{"kc": f.keycloakID, "org": f.orgID, "linked": f.linkedSpace, "unlinked": f.unlinkedSpace})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = client.ExecuteQueryWithLogging(context.Background(), `
			MATCH (n) WHERE n.id IN [$kc, $org, $linked, $unlinked] DETACH DELETE n
		`, map[string]interface{}{"kc": f.keycloakID, "org": f.orgID, "linked": f.linkedSpace, "unlinked": f.unlinkedSpace})
	})
	return f
}

func TestGetSpaceOrganizationID(t *testing.T) {
	client, log := neo4jForTest(t)
	f := seedOrgSpaces(t, client)
	spaces := NewSpaceService(client, log)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := spaces.GetSpaceOrganizationID(ctx, f.linkedSpace)
	if err != nil || got != f.orgID {
		t.Fatalf("linked space: got %q, %v; want %q", got, err, f.orgID)
	}
	got, err = spaces.GetSpaceOrganizationID(ctx, f.unlinkedSpace)
	if err != nil || got != "" {
		t.Fatalf("unlinked space: got %q, %v; want empty", got, err)
	}
	got, err = spaces.GetSpaceOrganizationID(ctx, "ab13-no-such-space")
	if err != nil || got != "" {
		t.Fatalf("missing space: got %q, %v; want empty", got, err)
	}
}

func TestResolveOrganizationSpaceFromSpaceNode_CarriesOrganizationID(t *testing.T) {
	client, log := neo4jForTest(t)
	f := seedOrgSpaces(t, client)
	svc := &SpaceContextService{spaceService: NewSpaceService(client, log), logger: log}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sc, err := svc.resolveOrganizationSpaceFromSpaceNode(ctx, f.keycloakID, f.linkedSpace)
	if err != nil {
		t.Fatalf("linked: %v", err)
	}
	if sc.OrganizationID != f.orgID {
		t.Fatalf("linked: OrganizationID = %q, want %q", sc.OrganizationID, f.orgID)
	}

	sc, err = svc.resolveOrganizationSpaceFromSpaceNode(ctx, f.keycloakID, f.unlinkedSpace)
	if err != nil {
		t.Fatalf("unlinked: %v", err)
	}
	if sc.OrganizationID != "" {
		t.Fatalf("unlinked: OrganizationID = %q, want empty (never guessed)", sc.OrganizationID)
	}
}
