package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// Plan #22 AB-13: the membership answer agent-builder relies on carries the
// space's Organization. Runs against a real Neo4j; skips unless NEO4J_URI is
// set. Seeds uniquely named nodes and removes them afterwards.
func TestVerifySpaceMembership_IncludesOrganizationID(t *testing.T) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" || testing.Short() {
		t.Skip("NEO4J_URI not set (or -short); skipping Neo4j-backed test")
	}
	log, err := logger.New(logger.Config{Level: "error", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	client, err := database.NewNeo4jClient(config.DatabaseConfig{
		URI: uri, Username: get("NEO4J_USERNAME", "neo4j"), Password: get("NEO4J_PASSWORD", "password"),
		Database: get("NEO4J_DATABASE", "neo4j"), MaxConns: 5,
	}, log)
	if err != nil {
		t.Skipf("Neo4j unavailable: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	suffix := uuid.NewString()
	kc, org, linked, unlinked := "ab13h-kc-"+suffix, "ab13h-org-"+suffix, "ab13h-linked-"+suffix, "ab13h-unlinked-"+suffix
	params := map[string]interface{}{"kc": kc, "org": org, "linked": linked, "unlinked": unlinked}
	if _, err := client.ExecuteQueryWithLogging(context.Background(), `
		CREATE (u:User {id: $kc, keycloak_id: $kc})
		CREATE (o:Organization {id: $org, name: 'AB-13 handler org'})
		CREATE (s1:Space {id: $linked, name: 'linked', space_type: 'organization', tenant_id: 'tenant_' + $linked})
		CREATE (s2:Space {id: $unlinked, name: 'unlinked', space_type: 'organization', tenant_id: 'tenant_' + $unlinked})
		CREATE (u)-[:OWNS]->(s1), (u)-[:OWNS]->(s2), (o)-[:HAS_SPACE]->(s1)
	`, params); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer func() {
		_, _ = client.ExecuteQueryWithLogging(context.Background(),
			`MATCH (n) WHERE n.id IN [$kc, $org, $linked, $unlinked] DETACH DELETE n`, params)
	}()

	userSvc := services.NewUserService(client, nil, log)
	orgSvc := services.NewOrganizationService(client, nil, log)
	spaceSvc := services.NewSpaceService(client, log)
	ctxSvc := services.NewSpaceContextService(userSvc, orgSvc, spaceSvc, nil, log)
	h := NewSpaceHandler(ctxSvc, spaceSvc, userSvc, orgSvc, log)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/spaces/:id/membership", func(c *gin.Context) { c.Set("user_id", kc); c.Next() }, h.VerifySpaceMembership)

	for _, tc := range []struct{ space, wantOrg string }{{linked, org}, {unlinked, ""}} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/spaces/"+tc.space+"/membership?space_type=organization", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d body %s", tc.space, w.Code, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		got, present := body["organization_id"]
		if !present {
			t.Fatalf("%s: organization_id missing from %s", tc.space, w.Body.String())
		}
		if got != tc.wantOrg {
			t.Fatalf("%s: organization_id = %v, want %q", tc.space, got, tc.wantOrg)
		}
	}
}
