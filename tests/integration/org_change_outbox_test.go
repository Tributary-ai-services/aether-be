//go:build integration

// Plan #22 A.4: every team / org-member change writes its event to the outbox
// in the same transaction; the relay publishes the outbox to
// aether.org.changes.v1 keyed by org, at least once, and an event survives the
// relay dying between "published" and "marked". Run with:
// go test -tags=integration ./tests/integration/ -run OrgChangeOutbox
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

type captureWriter struct {
	mu   sync.Mutex
	msgs []kafka.Message
	fail error
}

func (w *captureWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail != nil {
		return w.fail
	}
	w.msgs = append(w.msgs, msgs...)
	return nil
}

func TestOrgChangeOutbox(t *testing.T) {
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
	org := uuidLike(run, 11)
	owner, member := run+"-owner", run+"-member"
	t.Cleanup(func() {
		_, _ = client.ExecuteQuery(context.Background(), `
			MATCH (n) WHERE n.id STARTS WITH $run OR n.organization_id = $org OR n.id = $org OR (n:OrgChangeOutbox AND n.org_id = $org)
			DETACH DELETE n`, map[string]interface{}{"run": run, "org": org})
		client.Close(context.Background())
	})
	// Only this test's events: older pending events from other tests are set aside.
	if _, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox) WHERE e.published_at IS NULL SET e.published_at = datetime()`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExecuteQuery(ctx, `
		CREATE (o:Organization {id: $org, name: 'Org'})
		CREATE (ow:User {id: $owner, keycloak_id: 'kc-' + $owner, email: $owner + '@t'})
		CREATE (m:User {id: $member, keycloak_id: 'kc-' + $member, email: $member + '@t'})
		CREATE (ow)-[:MEMBER_OF {role: 'owner'}]->(o)`,
		map[string]interface{}{"org": org, "owner": owner, "member": member}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	teams := services.NewTeamService(client, log)

	pending := func() []map[string]interface{} {
		t.Helper()
		res, err := client.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox {org_id: $org}) WHERE e.published_at IS NULL
			RETURN e.type AS type, e.payload AS payload ORDER BY e.occurred_at, e.ord`, map[string]interface{}{"org": org})
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
		return out
	}
	types := func(evs []map[string]interface{}) []string {
		var ts []string
		for _, e := range evs {
			ts = append(ts, e["type"].(string))
		}
		return ts
	}

	// Each mutation writes its events in its own transaction.
	parent, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Eng", OrganizationID: org, Visibility: "organization"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	child, err := teams.CreateTeam(ctx, models.TeamCreateRequest{Name: "Router", OrganizationID: org, Visibility: "public"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := teams.JoinTeam(ctx, child.ID, member); err != nil { // public: joins, and is added to the org
		t.Fatal(err)
	}
	if err := teams.MoveTeam(ctx, child.ID, parent.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err := teams.MoveTeam(ctx, parent.ID, child.ID, owner); err == nil { // cycle: refused, rolled back
		t.Fatal("cycle move should fail")
	}
	if err := teams.UpdateTeamMemberRole(ctx, child.ID, member, models.TeamMemberRoleUpdateRequest{Role: "admin"}, owner); err != nil {
		t.Fatal(err)
	}
	if err := teams.RemoveTeamMember(ctx, child.ID, member, owner); err != nil {
		t.Fatal(err)
	}
	if err := teams.RemoveTeamMember(ctx, child.ID, member, owner); err == nil { // not a member any more: 404, no event
		t.Error("removing a non-member should fail")
	}
	if err := teams.ArchiveTeam(ctx, child.ID, owner); err != nil {
		t.Fatal(err)
	}

	evs := pending()
	want := []string{
		services.EvTeamCreated, services.EvMemberAdded, // Eng + its owner
		services.EvTeamCreated, services.EvMemberAdded, // Router + its owner
		services.EvOrgMemberAdded, services.EvMemberAdded, // member joins the public team: implied org edge, then the team
		services.EvTeamMoved, // the refused cycle move wrote nothing
		services.EvMemberRoleChanged,
		services.EvMemberRemoved, // the second, failed removal wrote nothing
		services.EvTeamArchived,
	}
	if fmt.Sprint(types(evs)) != fmt.Sprint(want) {
		t.Fatalf("events =\n  %v\nwant\n  %v", types(evs), want)
	}
	// Members are identified by Keycloak sub, never the Aether user id.
	join := evs[5]["member"].(map[string]interface{})
	if join["sub"] != "kc-"+member || evs[5]["actor_sub"] != "kc-"+member {
		t.Errorf("member/actor sub = %v / %v, want kc-%s", join["sub"], evs[5]["actor_sub"], member)
	}
	moved := evs[6]["team"].(map[string]interface{})
	if moved["parent_team_id"] != parent.ID || moved["slug"] != "router" {
		t.Errorf("moved team state = %v", moved)
	}
	if arch := evs[9]["team"].(map[string]interface{}); arch["archived"] != true {
		t.Errorf("archived event team.archived = %v", arch["archived"])
	}

	// Relay: publishes all, oldest first, keyed by org, then marks them.
	w := &captureWriter{}
	relay := services.NewOrgChangeRelayWithWriter(client, w, log, nil)
	n, err := relay.PublishOnce(ctx)
	if err != nil || n != len(want) {
		t.Fatalf("PublishOnce = %d, %v; want %d", n, err, len(want))
	}
	for i, m := range w.msgs {
		if string(m.Key) != org {
			t.Errorf("msg %d key = %q, want org id", i, m.Key)
		}
		ce, err := kafkabind.Decode(m.Value, m.Headers)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if ce.Type != "com.tas.aether.org."+want[i] {
			t.Errorf("msg %d type = %s, want %s", i, ce.Type, want[i])
		}
	}
	if len(pending()) != 0 {
		t.Errorf("events left pending after a clean publish")
	}
	if n, _ := relay.PublishOnce(ctx); n != 0 {
		t.Errorf("second publish sent %d, want 0", n)
	}

	// Kafka down: nothing is marked, so the retry publishes it.
	if _, err := teams.JoinTeam(ctx, parent.ID, owner); err == nil {
		t.Fatal("owner is already a member of Eng")
	}
	if err := teams.SetMemberJoinedAt(ctx, parent.ID, owner, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), owner); err != nil {
		t.Fatal(err)
	}
	down := &captureWriter{fail: errors.New("broker unavailable")}
	if _, err := services.NewOrgChangeRelayWithWriter(client, down, log, nil).PublishOnce(ctx); err == nil {
		t.Fatal("publish to a failing broker should fail")
	}
	if len(pending()) != 1 {
		t.Fatalf("pending after broker failure = %d, want 1", len(pending()))
	}

	// The relay dies after Kafka accepted the batch but before marking it. On
	// restart it publishes again; deduped on event_id it arrives exactly once.
	crash := &captureWriter{}
	_, err = services.NewOrgChangeRelayWithWriter(client, crash, log, func() error { return errors.New("relay killed") }).PublishOnce(ctx)
	if err == nil {
		t.Fatal("simulated crash should surface")
	}
	if len(pending()) != 1 {
		t.Fatalf("an event published but unmarked must stay pending, got %d", len(pending()))
	}
	again := &captureWriter{}
	if n, err := services.NewOrgChangeRelayWithWriter(client, again, log, nil).PublishOnce(ctx); err != nil || n != 1 {
		t.Fatalf("restarted relay published %d, %v; want 1", n, err)
	}
	seen := map[string]int{}
	for _, m := range append(crash.msgs, again.msgs...) {
		ce, _ := kafkabind.Decode(m.Value, m.Headers)
		seen[ce.ID]++
	}
	if len(seen) != 1 {
		t.Errorf("distinct events across the crash = %d, want exactly 1 (deduped on event_id)", len(seen))
	}
	for id, c := range seen {
		if c != 2 {
			t.Errorf("event %s delivered %d times; at-least-once means 2 here, which consumers dedupe", id, c)
		}
	}
	if len(pending()) != 0 {
		t.Errorf("event still pending after the restarted relay")
	}
}
