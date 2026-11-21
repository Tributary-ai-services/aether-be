//go:build integration

// AE-36: team-owned repos and source apps — who may write them, normalisation,
// archived teams refusing writes, one outbox event per item in the same
// transaction, and the lists on team.* events and the internal snapshot.
// Run with: go test -tags=integration ./tests/integration/ -run TeamOwnership
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

func TestTeamOwnership(t *testing.T) {
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
	org := uuidLike(run, 31)
	owner, orgAdmin, member, outsider := run+"-owner", run+"-orgadmin", run+"-member", run+"-outsider"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(), `
			MATCH (n) WHERE n.id STARTS WITH $run OR n.organization_id = $org OR n.id = $org OR (n:OrgChangeOutbox AND n.org_id = $org)
			DETACH DELETE n`, map[string]interface{}{"run": run, "org": org})
		client.Close(context.Background())
	})
	if _, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox) WHERE e.published_at IS NULL SET e.published_at = datetime()`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExecuteQuery(ctx, `
		CREATE (o:Organization {id: $org, name: 'Org'})
		CREATE (ow:User {id: $owner, keycloak_id: 'kc-' + $owner, email: $owner + '@t'})
		CREATE (ad:User {id: $admin, keycloak_id: 'kc-' + $admin, email: $admin + '@t'})
		CREATE (m:User {id: $member, keycloak_id: 'kc-' + $member, email: $member + '@t'})
		CREATE (x:User {id: $outsider, keycloak_id: 'kc-' + $outsider, email: $outsider + '@t'})
		CREATE (ow)-[:MEMBER_OF {role: 'member'}]->(o), (ad)-[:MEMBER_OF {role: 'admin'}]->(o), (m)-[:MEMBER_OF {role: 'member'}]->(o)`,
		map[string]interface{}{"org": org, "owner": owner, "admin": orgAdmin, "member": member, "outsider": outsider}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	teams := services.NewTeamService(client, log)
	team, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Router", OrganizationID: org, Visibility: "organization"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := teams.JoinTeam(ctx, team.ID, member); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(team.ToResponse()); !strings.Contains(string(b), `"repos":[]`) || !strings.Contains(string(b), `"sourceApps":[]`) {
		t.Errorf("new team response = %s", b)
	}
	// Only this test's events from here on.
	if _, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) SET e.published_at = datetime()`,
		map[string]interface{}{"org": org}); err != nil {
		t.Fatal(err)
	}
	pending := func() []map[string]interface{} {
		t.Helper()
		res, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) WHERE e.published_at IS NULL
			RETURN e.payload AS payload ORDER BY e.occurred_at, e.ord`, map[string]interface{}{"org": org})
		if err != nil {
			t.Fatal(err)
		}
		var out []map[string]interface{}
		for _, r := range res.Records {
			p, _ := r.Get("payload")
			var m map[string]interface{}
			_ = json.Unmarshal([]byte(p.(string)), &m)
			out = append(out, m)
		}
		_, _ = client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) SET e.published_at = datetime()`, map[string]interface{}{"org": org})
		return out
	}
	summary := func(evs []map[string]interface{}) string {
		s := ""
		for _, e := range evs {
			o, _ := e["owned"].(map[string]interface{})
			s += fmt.Sprintf("%s:%v ", e["type"], o["key"])
		}
		return s
	}
	repos := func(rs ...string) *[]string { return &rs }
	repoCh := func(set *[]string, add, rm []string) services.OwnedChange {
		return services.OwnedChange{Kind: services.OwnedKindRepo, Set: set, Add: add, Remove: rm}
	}

	// Who may write: team owner yes, org admin (not in the team) yes, team
	// member no, someone outside the org no.
	for _, who := range []string{member, outsider} {
		_, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(nil, []string{"o/r"}, nil), who)
		if apiErr, ok := errors.AsAPIError(err); !ok || apiErr.StatusCode != 403 {
			t.Errorf("%s: err = %v, want 403", who, err)
		}
	}
	if evs := pending(); len(evs) != 0 {
		t.Fatalf("refused writes wrote events: %v", summary(evs))
	}

	// Set normalises (lowercase, dedupe, sort) and writes one event per item.
	got, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(repos("Tributary-ai-services/AIQG-UI", "tributary-ai-services/aiqg-ui", "a/b"), nil, nil), owner)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.Repos) != "[a/b tributary-ai-services/aiqg-ui]" {
		t.Errorf("repos = %v", got.Repos)
	}
	evs := pending()
	if summary(evs) != "team.repo.added:a/b team.repo.added:tributary-ai-services/aiqg-ui " {
		t.Fatalf("set events = %s", summary(evs))
	}
	tm := evs[1]["team"].(map[string]interface{})
	if fmt.Sprint(tm["repos"]) != "[a/b tributary-ai-services/aiqg-ui]" || fmt.Sprint(tm["source_apps"]) != "[]" {
		t.Errorf("event team state = repos %v source_apps %v", tm["repos"], tm["source_apps"])
	}
	if evs[0]["actor_sub"] != "kc-"+owner || evs[0]["owned"].(map[string]interface{})["kind"] != "repo" {
		t.Errorf("event actor/kind = %v / %v", evs[0]["actor_sub"], evs[0]["owned"])
	}

	// Same set again: no change, no event. Org admin adds and removes.
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(repos("a/b", "Tributary-ai-services/aiqg-ui"), nil, nil), owner); err != nil {
		t.Fatal(err)
	}
	if evs := pending(); len(evs) != 0 {
		t.Errorf("no-op set wrote %s", summary(evs))
	}
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(nil, []string{"c/d", "a/b"}, []string{"A/B"}), orgAdmin); err != nil {
		t.Fatal(err)
	}
	if s := summary(pending()); s != "team.repo.removed:a/b team.repo.added:c/d " {
		t.Errorf("add/remove events = %s", s)
	}
	// Removing a repo the team does not own is a 404 and writes nothing;
	// a malformed repo is a 400.
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(nil, nil, []string{"zz/top"}), owner); !errors.IsNotFound(err) {
		t.Errorf("remove unowned: %v, want 404", err)
	}
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(nil, []string{"not-a-repo"}, nil), owner); err == nil {
		t.Error("malformed repo accepted")
	} else if apiErr, ok := errors.AsAPIError(err); !ok || apiErr.StatusCode != 400 {
		t.Errorf("malformed repo: %v, want 400", err)
	}

	// Source apps keep their case.
	got, err = teams.ChangeTeamOwned(ctx, team.ID, services.OwnedChange{Kind: services.OwnedKindSourceApp, Add: []string{"Claude-Code", "aiqg-ui"}}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.SourceApps) != "[Claude-Code aiqg-ui]" || fmt.Sprint(got.Repos) != "[c/d tributary-ai-services/aiqg-ui]" {
		t.Errorf("after source apps: repos %v apps %v", got.Repos, got.SourceApps)
	}
	if s := summary(pending()); s != "team.source_app.added:Claude-Code team.source_app.added:aiqg-ui " {
		t.Errorf("source app events = %s", s)
	}

	// Every team.* event now carries the lists: a rename shows them.
	name := "Router 2"
	if _, err := teams.UpdateTeam(ctx, team.ID, models.TeamUpdateRequest{Name: &name}, owner); err != nil {
		t.Fatal(err)
	}
	evs = pending()
	if len(evs) != 1 || evs[0]["type"] != services.EvTeamRenamed {
		t.Fatalf("rename events = %s", summary(evs))
	}
	if tm := evs[0]["team"].(map[string]interface{}); fmt.Sprint(tm["repos"]) != "[c/d tributary-ai-services/aiqg-ui]" ||
		fmt.Sprint(tm["source_apps"]) != "[Claude-Code aiqg-ui]" {
		t.Errorf("renamed event team = %v", tm)
	}

	// The internal snapshot carries them too.
	snap, err := services.NewOrgSnapshotService(client, log).Snapshot(ctx, org)
	if err != nil || snap == nil || len(snap.Teams) != 1 {
		t.Fatalf("snapshot = %v, %v", snap, err)
	}
	if fmt.Sprint(snap.Teams[0].Repos) != "[c/d tributary-ai-services/aiqg-ui]" || fmt.Sprint(snap.Teams[0].SourceApps) != "[Claude-Code aiqg-ui]" {
		t.Errorf("snapshot team = %+v", snap.Teams[0])
	}

	// Clearing stores no property; the snapshot then shows [].
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(repos(), nil, nil), owner); err != nil {
		t.Fatal(err)
	}
	if s := summary(pending()); s != "team.repo.removed:c/d team.repo.removed:tributary-ai-services/aiqg-ui " {
		t.Errorf("clear events = %s", s)
	}
	snap, _ = services.NewOrgSnapshotService(client, log).Snapshot(ctx, org)
	if b, _ := json.Marshal(snap.Teams[0]); !strings.Contains(string(b), `"repos":[]`) {
		t.Errorf("cleared snapshot team JSON = %s", b)
	}

	// An archived team refuses writes.
	if err := teams.ArchiveTeam(ctx, team.ID, owner); err != nil {
		t.Fatal(err)
	}
	pending()
	if _, err := teams.ChangeTeamOwned(ctx, team.ID, repoCh(nil, []string{"e/f"}, nil), orgAdmin); err == nil {
		t.Error("archived team accepted a repo")
	} else if apiErr, ok := errors.AsAPIError(err); !ok || apiErr.StatusCode != 409 {
		t.Errorf("archived: %v, want 409", err)
	}
	if evs := pending(); len(evs) != 0 {
		t.Errorf("archived write wrote %s", summary(evs))
	}
}
