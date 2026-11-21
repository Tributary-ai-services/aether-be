//go:build integration

// Plan #22 2.1/2.2: the internal org snapshot AIQG seeds and reconciles its
// team-history copy from. Run with: go test -tags=integration ./tests/integration/ -run OrgSnapshot
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

func TestOrgSnapshot(t *testing.T) {
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
	org, other := uuidLike(run, 21), uuidLike(run, 22)
	owner, member := run+"-owner", run+"-member"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(), `
			MATCH (n) WHERE n.id STARTS WITH $run OR n.organization_id IN [$org, $other] OR n.id IN [$org, $other]
			   OR (n:OrgChangeOutbox AND n.org_id IN [$org, $other])
			DETACH DELETE n`, map[string]interface{}{"run": run, "org": org, "other": other})
		client.Close(context.Background())
	})
	if _, err := client.ExecuteQuery(ctx, `
		CREATE (o:Organization {id: $org, name: 'TAS', slug: 'tas'}), (x:Organization {id: $other, name: 'Other'})
		CREATE (ow:User {id: $owner, keycloak_id: 'kc-owner-' + $run, email: $owner + '@t'})
		CREATE (m:User {id: $member, keycloak_id: 'kc-member-' + $run, email: $member + '@t'})
		CREATE (ow)-[:MEMBER_OF {role: 'owner', joined_at: datetime('2026-09-01T00:00:00Z')}]->(o)`,
		map[string]interface{}{"org": org, "other": other, "owner": owner, "member": member, "run": run}); err != nil {
		t.Fatal(err)
	}
	teams := services.NewTeamService(client, log)
	eng, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Engineering", OrganizationID: org, Visibility: "organization"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	router, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "LLM Router", OrganizationID: org, Visibility: "public", ParentTeamID: eng.ID}, owner)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Old", OrganizationID: org, Visibility: "private"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := teams.JoinTeam(ctx, router.ID, member); err != nil { // public: joins + implied org member
		t.Fatal(err)
	}
	if err := teams.ArchiveTeam(ctx, gone.ID, owner); err != nil {
		t.Fatal(err)
	}
	// The owner is not a member of the other org, so cannot create a team there.
	if _, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Elsewhere", OrganizationID: other, Visibility: "public"}, owner); !errors.IsForbidden(err) {
		t.Fatalf("create in a foreign org: %v, want forbidden", err)
	}

	svc := services.NewOrgSnapshotService(client, log)
	snap, err := svc.Snapshot(ctx, org)
	if err != nil || snap == nil {
		t.Fatalf("Snapshot = %v, %v", snap, err)
	}
	if snap.Org.Name != "TAS" || len(snap.Teams) != 3 {
		t.Fatalf("org %q with %d teams, want TAS with 3 (archived included)", snap.Org.Name, len(snap.Teams))
	}
	byID := map[string]services.SnapshotTeam{}
	for _, tm := range snap.Teams {
		byID[tm.ID] = tm
	}
	if byID[router.ID].ParentTeamID != eng.ID || byID[router.ID].Slug != "llm-router" {
		t.Errorf("router = %+v", byID[router.ID])
	}
	if byID[gone.ID].ArchivedAt == nil || byID[eng.ID].ArchivedAt != nil {
		t.Errorf("archived flags wrong: gone=%v eng=%v", byID[gone.ID].ArchivedAt, byID[eng.ID].ArchivedAt)
	}
	// Team members: owner in all 3 (creator), member in router; by Keycloak sub.
	count := map[string]int{}
	for _, m := range snap.TeamMembers {
		count[m.Sub]++
		if m.JoinedAt == nil || m.Role == "" {
			t.Errorf("membership lacks role/joined_at: %+v", m)
		}
	}
	if count["kc-owner-"+run] != 3 || count["kc-member-"+run] != 1 {
		t.Errorf("team memberships by sub = %v", count)
	}
	// Org members: the seeded owner (joined 2026-09-01) and the implied member.
	if len(snap.OrgMembers) != 2 || snap.OrgMembers[0].Sub != "kc-owner-"+run || !snap.OrgMembers[0].JoinedAt.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("org members = %+v", snap.OrgMembers)
	}
	for _, m := range append(snap.TeamMembers, snap.OrgMembers...) {
		if m.Sub == owner || m.Sub == member {
			t.Errorf("snapshot leaked an Aether user id: %+v", m)
		}
	}

	if none, err := svc.Snapshot(ctx, run+"-missing"); none != nil || err != nil {
		t.Errorf("missing org = %v, %v; want nil, nil", none, err)
	}
	orgs, err := svc.ListOrgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range orgs {
		found = found || o.ID == org
	}
	if !found {
		t.Errorf("ListOrgs does not include the org")
	}
}
