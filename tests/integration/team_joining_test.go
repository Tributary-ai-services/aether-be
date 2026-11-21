//go:build integration

// Plan #22 A.3: joining teams by visibility, the private-team request queue,
// the implied org membership, back-dated joined_at, and who may see a team.
// Run with: go test -tags=integration ./tests/integration/ -run TeamJoining
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

func TestTeamJoining(t *testing.T) {
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
	_ = client.CreateConstraints(ctx)
	run := fmt.Sprintf("test-%d", time.Now().UnixNano())
	org := uuidLike(run, 7)
	owner, member, outsider, stranger, invitee := run+"-owner", run+"-member", run+"-outsider", run+"-stranger", run+"-invitee"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(),
			`MATCH (n) WHERE n.id STARTS WITH $run OR n.organization_id = $org OR n.id = $org DETACH DELETE n`,
			map[string]interface{}{"run": run, "org": org})
		client.Close(context.Background())
	})
	if _, err := client.ExecuteQuery(ctx, `
		CREATE (o:Organization {id: $org, name: 'Org'})
		WITH o
		UNWIND $users AS uid
		CREATE (:User {id: uid, keycloak_id: 'kc-' + uid, email: uid + '@t', full_name: uid})
		WITH DISTINCT o
		MATCH (ow:User {id: $owner}), (m:User {id: $member})
		CREATE (ow)-[:MEMBER_OF {role: 'owner'}]->(o), (m)-[:MEMBER_OF {role: 'member'}]->(o)`,
		map[string]interface{}{"org": org, "owner": owner, "member": member,
			"users": []string{owner, member, outsider, stranger, invitee}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	teams := services.NewTeamService(client, log)
	mk := func(name, vis string) *models.Team {
		tm, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: name, OrganizationID: org, Visibility: vis}, owner)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return tm
	}
	orgTeam, pubTeam, privTeam := mk("Org team", "organization"), mk("Public team", "public"), mk("Private team", "private")

	// organization: org members self-join, non-members are refused.
	if _, err := teams.JoinTeam(ctx, orgTeam.ID, outsider); !errors.IsForbidden(err) {
		t.Errorf("outsider join org team: %v, want forbidden", err)
	}
	if st, err := teams.JoinTeam(ctx, orgTeam.ID, "kc-"+member); err != nil || st != services.JoinStatusJoined {
		t.Errorf("member join org team (by keycloak id) = %q, %v", st, err)
	}
	if _, err := teams.JoinTeam(ctx, orgTeam.ID, member); !errors.IsConflict(err) {
		t.Errorf("second join: %v, want conflict", err)
	}

	// public: anyone joins, and gains the implied org membership.
	if st, err := teams.JoinTeam(ctx, pubTeam.ID, outsider); err != nil || st != services.JoinStatusJoined {
		t.Errorf("outsider join public team = %q, %v", st, err)
	}
	res, _ := client.ExecuteQuery(ctx, `MATCH (:User {id: $u})-[m:MEMBER_OF]->(:Organization {id: $org}) RETURN m.role AS role, m.implied_by_team AS by`,
		map[string]interface{}{"u": outsider, "org": org})
	if len(res.Records) != 1 {
		t.Fatalf("implied org edge: %d rows, want 1", len(res.Records))
	}
	if r, _ := res.Records[0].Get("role"); r != "member" {
		t.Errorf("implied org role = %v", r)
	}
	if by, _ := res.Records[0].Get("by"); by != pubTeam.ID {
		t.Errorf("implied_by_team = %v", by)
	}

	// private: a request queue; one pending request per person.
	for i := 0; i < 2; i++ {
		if st, err := teams.JoinTeam(ctx, privTeam.ID, member); err != nil || st != services.JoinStatusRequested {
			t.Errorf("request #%d = %q, %v", i+1, st, err)
		}
	}
	if st, _ := teams.MyJoinRequestStatus(ctx, privTeam.ID, member); st != "pending" {
		t.Errorf("my request status = %q", st)
	}
	if _, err := teams.ListJoinRequests(ctx, privTeam.ID, member); !errors.IsForbidden(err) {
		t.Errorf("member lists requests: %v, want forbidden", err)
	}
	reqs, err := teams.ListJoinRequests(ctx, privTeam.ID, owner)
	if err != nil || len(reqs) != 1 || reqs[0].UserID != member {
		t.Fatalf("pending requests = %+v, %v; want exactly the member's", reqs, err)
	}

	// Visibility: an org member reads the private team by id but does not see
	// it listed; a stranger can do neither.
	if _, err := teams.GetTeam(ctx, privTeam.ID, member); err != nil {
		t.Errorf("org member reads private team by id: %v", err)
	}
	if _, err := teams.GetTeam(ctx, privTeam.ID, stranger); !errors.IsNotFound(err) {
		t.Errorf("stranger reads private team: %v, want not found", err)
	}
	if _, err := teams.GetTeam(ctx, orgTeam.ID, stranger); !errors.IsNotFound(err) {
		t.Errorf("stranger reads org team: %v, want not found", err)
	}
	if _, err := teams.GetTeam(ctx, pubTeam.ID, stranger); err != nil {
		t.Errorf("stranger reads public team: %v", err)
	}
	listed := func(user string) map[string]bool {
		ts, err := teams.GetTeams(ctx, user, org)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, tm := range ts {
			m[tm.ID] = true
		}
		return m
	}
	if l := listed(member); l[privTeam.ID] || !l[orgTeam.ID] || !l[pubTeam.ID] {
		t.Errorf("member listing = %v, want org+public, not private", l)
	}
	if l := listed(owner); !l[privTeam.ID] {
		t.Errorf("owner (a member of the private team) does not see it listed")
	}
	if l := listed(stranger); l[orgTeam.ID] || l[privTeam.ID] || !l[pubTeam.ID] {
		t.Errorf("stranger listing = %v, want public only", l)
	}

	// Approve, back-dated to 1 October.
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := teams.DecideJoinRequest(ctx, privTeam.ID, reqs[0].ID, true, &since, member); !errors.IsForbidden(err) {
		t.Errorf("member decides own request: %v, want forbidden", err)
	}
	if err := teams.DecideJoinRequest(ctx, privTeam.ID, reqs[0].ID, true, &since, owner); err != nil {
		t.Fatalf("approve: %v", err)
	}
	joined := func(team, user string) time.Time {
		r, _ := client.ExecuteQuery(ctx, `MATCH (:User {id: $u})-[m:MEMBER_OF]->(:Team {id: $t}) RETURN m.joined_at AS at`,
			map[string]interface{}{"u": user, "t": team})
		if len(r.Records) != 1 {
			t.Fatalf("membership %s in %s: %d rows", user, team, len(r.Records))
		}
		v, _ := r.Records[0].Get("at")
		return v.(time.Time)
	}
	if got := joined(privTeam.ID, member); !got.Equal(since) {
		t.Errorf("approved joined_at = %v, want %v", got, since)
	}
	if err := teams.DecideJoinRequest(ctx, privTeam.ID, reqs[0].ID, true, nil, owner); !errors.IsNotFound(err) {
		t.Errorf("deciding a closed request: %v, want not found", err)
	}

	// Deny leaves no membership.
	if st, _ := teams.JoinTeam(ctx, privTeam.ID, stranger); st != services.JoinStatusRequested {
		t.Fatalf("stranger request = %q", st)
	}
	reqs, _ = teams.ListJoinRequests(ctx, privTeam.ID, owner)
	if len(reqs) != 1 {
		t.Fatalf("pending after approve = %d, want 1 (the stranger's)", len(reqs))
	}
	if err := teams.DecideJoinRequest(ctx, privTeam.ID, reqs[0].ID, false, nil, owner); err != nil {
		t.Fatalf("deny: %v", err)
	}
	r, _ := client.ExecuteQuery(ctx, `MATCH (:User {id: $u})-[m:MEMBER_OF]->(:Team {id: $t}) RETURN count(m) AS n`,
		map[string]interface{}{"u": stranger, "t": privTeam.ID})
	if n, _ := r.Records[0].Get("n"); n.(int64) != 0 {
		t.Errorf("denied request created a membership")
	}

	// joined_at: managers only, never in the future.
	future := time.Now().Add(48 * time.Hour)
	if err := teams.SetMemberJoinedAt(ctx, orgTeam.ID, member, future, owner); !errors.IsValidation(err) {
		t.Errorf("future joined_at: %v, want validation", err)
	}
	if err := teams.SetMemberJoinedAt(ctx, orgTeam.ID, member, since, member); !errors.IsForbidden(err) {
		t.Errorf("member back-dates self: %v, want forbidden", err)
	}
	if err := teams.SetMemberJoinedAt(ctx, orgTeam.ID, member, since, owner); err != nil || !joined(orgTeam.ID, member).Equal(since) {
		t.Errorf("owner back-dates member: %v", err)
	}

	// Invite: back-dated, and the invitee gains the implied org membership.
	if _, err := teams.InviteTeamMember(ctx, orgTeam.ID, models.TeamInviteRequest{Email: invitee + "@t", Role: "member", JoinedAt: &since}, owner); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if got := joined(orgTeam.ID, invitee); !got.Equal(since) {
		t.Errorf("invite joined_at = %v, want %v", got, since)
	}
	r, _ = client.ExecuteQuery(ctx, `MATCH (:User {id: $u})-[m:MEMBER_OF]->(:Organization {id: $org}) RETURN count(m) AS n`,
		map[string]interface{}{"u": invitee, "org": org})
	if n, _ := r.Records[0].Get("n"); n.(int64) != 1 {
		t.Errorf("invitee has no implied org membership")
	}
	// An existing org membership is never duplicated or downgraded.
	r, _ = client.ExecuteQuery(ctx, `MATCH (:User {id: $u})-[m:MEMBER_OF]->(:Organization {id: $org}) RETURN collect(m.role) AS roles`,
		map[string]interface{}{"u": owner, "org": org})
	if roles, _ := r.Records[0].Get("roles"); fmt.Sprint(roles) != "[owner]" {
		t.Errorf("owner org roles = %v, want [owner]", roles)
	}
}
