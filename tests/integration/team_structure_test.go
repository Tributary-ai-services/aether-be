//go:build integration

// Plan #22 A.2: team nesting (CHILD_OF, same org, no cycles), the org edge,
// per-org slugs, and archive-instead-of-delete, against a real Neo4j.
// Run with: go test -tags=integration ./tests/integration/ -run TeamStructure
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

func TestTeamStructure(t *testing.T) {
	log, err := logger.New(logger.Config{Level: "error", Format: "console"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := database.NewNeo4jClient(config.DatabaseConfig{
		URI: neo4jURI(), Username: "neo4j", Password: neo4jPassword(), Database: "neo4j", MaxConns: 4,
	}, log)
	if err != nil {
		t.Skipf("Neo4j not reachable at %s (%v)", neo4jURI(), err)
	}
	ctx := context.Background()
	if err := client.CreateConstraints(ctx); err != nil {
		t.Fatal(err)
	}
	run := fmt.Sprintf("test-%d", time.Now().UnixNano())
	orgA, orgB := uuidLike(run, 1), uuidLike(run, 2)
	owner, member, outsider := run+"-owner", run+"-member", run+"-outsider"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(), `
			MATCH (n) WHERE n.id STARTS WITH $run OR n.organization_id IN [$a, $b] OR n.id IN [$a, $b]
			DETACH DELETE n`, map[string]interface{}{"run": run, "a": orgA, "b": orgB})
		client.Close(context.Background())
	})
	if _, err := client.ExecuteQuery(ctx, `
		CREATE (a:Organization {id: $a, name: 'A'}), (b:Organization {id: $b, name: 'B'})
		CREATE (o:User {id: $owner, keycloak_id: $owner, email: $owner + '@t'})
		CREATE (m:User {id: $member, keycloak_id: $member, email: $member + '@t'})
		CREATE (x:User {id: $outsider, keycloak_id: $outsider, email: $outsider + '@t'})
		CREATE (o)-[:MEMBER_OF {role: 'owner'}]->(a), (o)-[:MEMBER_OF {role: 'owner'}]->(b), (m)-[:MEMBER_OF {role: 'member'}]->(a)`,
		map[string]interface{}{"a": orgA, "b": orgB, "owner": owner, "member": member, "outsider": outsider}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	teams := services.NewTeamService(client, log)
	mk := func(org, name, parent, slug string) *models.Team {
		t.Helper()
		tm, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: name, OrganizationID: org, Visibility: "organization",
			ParentTeamID: parent, Slug: slug}, owner)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return tm
	}

	// Create: org edge, derived slug, nesting.
	eng := mk(orgA, "Engineering", "", "")
	router := mk(orgA, "LLM Router", eng.ID, "")
	if eng.Slug != "engineering" || router.Slug != "llm-router" || router.ParentTeamID != eng.ID {
		t.Errorf("created: eng slug %q, router slug %q parent %q", eng.Slug, router.Slug, router.ParentTeamID)
	}
	res, _ := client.ExecuteQuery(ctx, `MATCH (r:Team {id: $r})-[:CHILD_OF]->(e:Team {id: $e}), (r)-[:BELONGS_TO]->(o:Organization {id: $a}) RETURN count(*) AS n`,
		map[string]interface{}{"r": router.ID, "e": eng.ID, "a": orgA})
	if n, _ := res.Records[0].Get("n"); n.(int64) != 1 {
		t.Errorf("CHILD_OF / BELONGS_TO edges missing")
	}

	// Slugs: same name in the same org gets a suffix; same name in another org
	// keeps the plain slug; an explicit duplicate is refused.
	if dup := mk(orgA, "Engineering", "", ""); dup.Slug != "engineering-2" {
		t.Errorf("duplicate-name slug = %q, want engineering-2", dup.Slug)
	}
	if other := mk(orgB, "Engineering", "", ""); other.Slug != "engineering" {
		t.Errorf("other-org slug = %q, want engineering", other.Slug)
	}
	if _, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "X", OrganizationID: orgA, Visibility: "private", Slug: "engineering"}, owner); !errors.IsConflict(err) {
		t.Errorf("explicit duplicate slug: %v, want conflict", err)
	}

	// Cycles and cross-org parents are refused.
	leaf := mk(orgA, "Leaf", router.ID, "")
	if err := teams.MoveTeam(ctx, eng.ID, leaf.ID, owner); !errors.IsConflict(err) {
		t.Errorf("move under own grandchild: %v, want conflict (cycle)", err)
	}
	if err := teams.MoveTeam(ctx, eng.ID, eng.ID, owner); !errors.IsConflict(err) {
		t.Errorf("move under itself: %v, want conflict", err)
	}
	otherOrgTeam := mk(orgB, "B team", "", "")
	if err := teams.MoveTeam(ctx, leaf.ID, otherOrgTeam.ID, owner); !errors.IsValidation(err) {
		t.Errorf("cross-org parent: %v, want validation", err)
	}
	// A legal move, then back to top level.
	if err := teams.MoveTeam(ctx, leaf.ID, eng.ID, owner); err != nil {
		t.Fatalf("legal move: %v", err)
	}
	if err := teams.MoveTeam(ctx, leaf.ID, "", owner); err != nil {
		t.Fatalf("to top level: %v", err)
	}
	res, _ = client.ExecuteQuery(ctx, `MATCH (l:Team {id: $l}) OPTIONAL MATCH (l)-[c:CHILD_OF]->() RETURN count(c) AS n, l.parent_team_id AS p`,
		map[string]interface{}{"l": leaf.ID})
	if n, _ := res.Records[0].Get("n"); n.(int64) != 0 {
		t.Errorf("top-level team still has a CHILD_OF edge")
	}
	if p, _ := res.Records[0].Get("p"); p != nil {
		t.Errorf("parent_team_id = %v, want null", p)
	}
	// An org member who manages nothing cannot move teams.
	if err := teams.MoveTeam(ctx, leaf.ID, eng.ID, member); !errors.IsForbidden(err) {
		t.Errorf("member move: %v, want forbidden", err)
	}

	// Update writes every field (the old builder dropped all but first and last).
	name, desc := "Engineering Org", "everything we build"
	up, err := teams.UpdateTeam(ctx, eng.ID, models.TeamUpdateRequest{Name: &name, Description: &desc}, owner)
	if err != nil || up.Name != name || up.Description != desc {
		t.Errorf("update name+description = %+v, %v", up, err)
	}

	// Archive instead of delete; archived teams refuse writes and leave listings.
	if err := teams.DeleteTeam(ctx, leaf.ID, owner); err != nil {
		t.Fatalf("delete (archive): %v", err)
	}
	got, err := teams.GetTeam(ctx, leaf.ID, owner)
	if err != nil || got.ArchivedAt == nil {
		t.Errorf("archived team readable with archived_at: %+v, %v", got, err)
	}
	if _, err := teams.InviteTeamMember(ctx, leaf.ID, models.TeamInviteRequest{Email: member + "@t", Role: "member"}, owner); !errors.IsConflict(err) {
		t.Errorf("invite to archived team: %v, want conflict", err)
	}
	if _, err := teams.UpdateTeam(ctx, leaf.ID, models.TeamUpdateRequest{Name: &name}, owner); !errors.IsConflict(err) {
		t.Errorf("update archived team: %v, want conflict", err)
	}
	if err := teams.MoveTeam(ctx, router.ID, leaf.ID, owner); !errors.IsValidation(err) {
		t.Errorf("move under archived parent: %v, want validation", err)
	}
	listed, _ := teams.GetTeams(ctx, owner, orgA)
	for _, tm := range listed {
		if tm.ID == leaf.ID {
			t.Errorf("archived team listed")
		}
	}
	if err := teams.DeleteTeam(ctx, leaf.ID, owner); err != nil {
		t.Errorf("archiving twice should be a no-op: %v", err)
	}
	if err := teams.DeleteTeam(ctx, router.ID, outsider); !errors.IsForbidden(err) {
		t.Errorf("outsider archive: %v, want forbidden", err)
	}
}

// uuidLike returns a UUID-shaped id unique to this run (Team.organization_id is validated as a uuid upstream).
func uuidLike(run string, n int) string {
	h := fmt.Sprintf("%032x", time.Now().UnixNano()+int64(n))
	h = h[len(h)-32:]
	return fmt.Sprintf("%s-%s-4%s-8%s-%s", h[0:8], h[8:12], h[13:16], h[17:20], h[20:32])
}
