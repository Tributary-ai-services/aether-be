//go:build integration

// Plan #22 A.1: deleting an organization ARCHIVES its teams instead of
// DETACH DELETE-ing them. AIQG's team history and billing reports refer to
// teams by id, so a deleted team orphans both. The team node and its member
// edges survive with archived_at set, and archived teams drop out of every
// listing. Run with: go test -tags=integration ./tests/integration/ -run OrgDeleteArchives
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

func TestOrgDeleteArchivesTeams(t *testing.T) {
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
	run := fmt.Sprintf("test-%d", time.Now().UnixNano())
	org, owner, member := run+"-org", run+"-owner", run+"-member"
	teamA, teamB, other := run+"-team-a", run+"-team-b", run+"-team-other-org"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(),
			`MATCH (n) WHERE n.id STARTS WITH $run DETACH DELETE n`, map[string]interface{}{"run": run})
		client.Close(context.Background())
	})

	if _, err := client.ExecuteQuery(ctx, `
		CREATE (o:Organization {id: $org, name: 'Org', slug: $org})
		CREATE (ow:User {id: $owner, keycloak_id: $owner, email: $owner + '@test'})
		CREATE (m:User {id: $member, keycloak_id: $member, email: $member + '@test'})
		CREATE (ow)-[:MEMBER_OF {role: 'owner'}]->(o)
		CREATE (ta:Team {id: $ta, name: 'A', description: '', organization_id: $org, visibility: 'organization', created_at: datetime(), created_by: $owner})
		CREATE (tb:Team {id: $tb, name: 'B', description: '', organization_id: $org, visibility: 'private', created_at: datetime(), created_by: $owner})
		CREATE (tx:Team {id: $tx, name: 'X', description: '', organization_id: $run + '-elsewhere', visibility: 'organization', created_at: datetime(), created_by: $owner})
		CREATE (m)-[:MEMBER_OF {role: 'member', joined_at: datetime()}]->(ta)
		CREATE (m)-[:MEMBER_OF {role: 'member', joined_at: datetime()}]->(tx)
		CREATE (ow)-[:MEMBER_OF {role: 'owner', joined_at: datetime()}]->(tb)`,
		map[string]interface{}{"org": org, "owner": owner, "member": member, "ta": teamA, "tb": teamB, "tx": other, "run": run}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	teams := services.NewTeamService(client, log)
	before, err := teams.GetUserTeamIDs(ctx, member)
	if err != nil || len(before) != 2 {
		t.Fatalf("before delete: member teams = %v, %v; want 2", before, err)
	}

	orgs := services.NewOrganizationService(client, nil, log)
	if err := orgs.DeleteOrganization(ctx, org, owner); err != nil {
		t.Fatalf("DeleteOrganization: %v", err)
	}

	res, err := client.ExecuteQuery(ctx, `
		MATCH (t:Team) WHERE t.id IN [$ta, $tb, $tx]
		OPTIONAL MATCH (t)<-[r:MEMBER_OF]-()
		RETURN t.id AS id, t.archived_at IS NOT NULL AS archived, t.archived_reason AS reason, count(r) AS members`,
		map[string]interface{}{"ta": teamA, "tb": teamB, "tx": other})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][3]interface{}{}
	for _, r := range res.Records {
		id, _ := r.Get("id")
		a, _ := r.Get("archived")
		reason, _ := r.Get("reason")
		n, _ := r.Get("members")
		got[id.(string)] = [3]interface{}{a, reason, n}
	}
	for _, id := range []string{teamA, teamB} {
		g, ok := got[id]
		if !ok {
			t.Fatalf("team %s was deleted; it must be archived", id)
		}
		if g[0] != true || g[1] != "organization_deleted" || g[2].(int64) != 1 {
			t.Errorf("team %s = archived:%v reason:%v members:%v; want archived, organization_deleted, 1 member edge kept", id, g[0], g[1], g[2])
		}
	}
	if g := got[other]; g[0] != false {
		t.Errorf("another org's team was archived: %v", g)
	}
	orgLeft, _ := client.ExecuteQuery(ctx, `MATCH (o:Organization {id: $org}) RETURN count(o) AS n`, map[string]interface{}{"org": org})
	if n, _ := orgLeft.Records[0].Get("n"); n.(int64) != 0 {
		t.Errorf("organization was not deleted")
	}

	// Plan #22 A.4: the delete wrote team.archived per team and org.deleted.
	ev, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) RETURN e.type AS type ORDER BY e.ord`,
		map[string]interface{}{"org": org})
	if err != nil {
		t.Fatal(err)
	}
	var evTypes []string
	for _, r := range ev.Records {
		v, _ := r.Get("type")
		evTypes = append(evTypes, v.(string))
	}
	if fmt.Sprint(evTypes) != "[team.archived team.archived org.deleted]" {
		t.Errorf("org delete events = %v", evTypes)
	}
	_, _ = client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) DELETE e`, map[string]interface{}{"org": org})

	after, err := teams.GetUserTeamIDs(ctx, member)
	if err != nil || len(after) != 1 || after[0] != other {
		t.Errorf("after delete: member teams = %v, %v; want only %s (archived teams hidden)", after, err, other)
	}
	listed, err := teams.GetTeams(ctx, member, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range listed {
		if tm.ID == teamA || tm.ID == teamB {
			t.Errorf("archived team %s still listed", tm.ID)
		}
	}
	byOrg, err := teams.GetTeams(ctx, owner, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(byOrg) != 0 {
		t.Errorf("org listing still shows %d archived teams", len(byOrg))
	}
}
