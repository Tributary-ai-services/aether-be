package services

// Org change events (Plan #22 A.4, design §5.1).
//
// Every team and org-membership mutation writes an (:OrgChangeOutbox) node in
// the SAME Neo4j transaction as the change, so an event is never lost (the
// change committed, the event did not) or invented (the event committed, the
// change did not). OrgChangeRelay publishes pending outbox nodes, oldest
// first, to Kafka topic aether.org.changes.v1 keyed by org id, then marks them
// published. A relay that dies between publishing and marking re-publishes on
// restart, so delivery is at-least-once and consumers dedupe on event_id (the
// CloudEvent id). Publishing is gated by AETHER_ORG_EVENTS_ENABLED; the outbox
// is always written, so turning the flag on later loses nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// OrgChangesTopic is the Kafka topic for org and team changes.
const OrgChangesTopic = "aether.org.changes.v1"

// Org change event types. The CloudEvent type is "com.tas.aether.org." + type.
const (
	EvTeamCreated       = "team.created"
	EvTeamRenamed       = "team.renamed"
	EvTeamMoved         = "team.moved"
	EvTeamArchived      = "team.archived"
	EvMemberAdded       = "team.member.added"
	EvMemberRemoved     = "team.member.removed"
	EvMemberRoleChanged = "team.member.role_changed"
	EvMemberJoinedAt    = "team.member.joined_at_changed"
	EvOrgMemberAdded    = "org.member.added"
	EvOrgMemberRemoved  = "org.member.removed"
	EvOrgMemberRole     = "org.member.role_changed"
	EvOrgDeleted        = "org.deleted"

	// AE-36: a team's owned repos and source apps, one event per item.
	EvTeamRepoAdded        = "team.repo.added"
	EvTeamRepoRemoved      = "team.repo.removed"
	EvTeamSourceAppAdded   = "team.source_app.added"
	EvTeamSourceAppRemoved = "team.source_app.removed"
)

const orgEventSource = "urn:tas:service:aether-be:org"

// OrgChangeTeam is the team's state after the change.
type OrgChangeTeam struct {
	ID           string `json:"id"`
	ParentTeamID string `json:"parent_team_id,omitempty"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Visibility   string `json:"visibility,omitempty"`
	Archived     bool   `json:"archived"`
	// Repos and SourceApps are what the team owns after the change (AE-36).
	// Always present (never omitted), so [] means "owns none" while an absent
	// field means an event written before AE-36.
	Repos      []string `json:"repos"`
	SourceApps []string `json:"source_apps"`
}

// OrgChangeOwned is the repo or source app a team.repo.* / team.source_app.*
// event is about. Kind matches AIQG's team_member_history.member_kind.
type OrgChangeOwned struct {
	Kind string `json:"kind"` // repo | source_app
	Key  string `json:"key"`  // "owner/name" (lowercase) | the source_app
}

// OrgChangeMember is the member the change is about. Sub is the Keycloak sub,
// the key AIQG joins on (owner_sub); Aether's own user id is not sent.
type OrgChangeMember struct {
	Sub       string    `json:"sub"`
	Role      string    `json:"role,omitempty"`
	ValidFrom time.Time `json:"valid_from"` // joined_at for adds; the change time otherwise
}

// OrgChangeEvent is the CloudEvent data of aether.org.changes.v1.
type OrgChangeEvent struct {
	EventID    string           `json:"event_id"`
	Type       string           `json:"type"`
	OrgID      string           `json:"org_id"`
	OccurredAt time.Time        `json:"occurred_at"`
	ActorSub   string           `json:"actor_sub,omitempty"`
	Team       *OrgChangeTeam   `json:"team,omitempty"`
	Member     *OrgChangeMember `json:"member,omitempty"`
	Owned      *OrgChangeOwned  `json:"owned,omitempty"`
}

// outboxSeq orders events written in one transaction (they share its clock).
type outboxSeq struct{ n int }

func (o *outboxSeq) next() int { o.n++; return o.n }

// subOf returns a user's Keycloak sub (or "" when unknown).
func subOf(ctx context.Context, tx neo4j.ManagedTransaction, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	res, err := tx.Run(ctx, `MATCH (u:User) WHERE u.id = $u OR u.keycloak_id = $u RETURN u.keycloak_id AS sub LIMIT 1`,
		map[string]interface{}{"u": userID})
	if err != nil {
		return "", err
	}
	if !res.Next(ctx) {
		return "", nil
	}
	v, _ := res.Record().Get("sub")
	s, _ := v.(string)
	return s, nil
}

// teamSnapshotTx reads a team's current state for an event.
func teamSnapshotTx(ctx context.Context, tx neo4j.ManagedTransaction, teamID string) (*OrgChangeTeam, string, error) {
	res, err := tx.Run(ctx, `MATCH (t:Team {id: $id})
		RETURN t.organization_id AS org, t.name AS name, t.slug AS slug, t.parent_team_id AS parent,
		       t.visibility AS vis, t.archived_at IS NOT NULL AS archived,
		       coalesce(t.repos, []) AS repos, coalesce(t.source_apps, []) AS apps`, map[string]interface{}{"id": teamID})
	if err != nil {
		return nil, "", err
	}
	if !res.Next(ctx) {
		return nil, "", fmt.Errorf("team %s not found for event", teamID)
	}
	r := res.Record()
	str := func(k string) string { v, _ := r.Get(k); s, _ := v.(string); return s }
	arch, _ := r.Get("archived")
	repos, _ := r.Get("repos")
	apps, _ := r.Get("apps")
	return &OrgChangeTeam{ID: teamID, ParentTeamID: str("parent"), Name: str("name"), Slug: str("slug"),
		Visibility: str("vis"), Archived: arch == true, Repos: stringList(repos), SourceApps: stringList(apps)}, str("org"), nil
}

// writeOutboxTx appends one event to the outbox inside tx.
func writeOutboxTx(ctx context.Context, tx neo4j.ManagedTransaction, seq *outboxSeq, ev OrgChangeEvent) error {
	if ev.EventID == "" {
		ev.EventID = uuid.New().String()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	teamID := ""
	if ev.Team != nil {
		teamID = ev.Team.ID
	}
	_, err = tx.Run(ctx, `CREATE (:OrgChangeOutbox {event_id: $id, type: $type, org_id: $org, team_id: $team,
		occurred_at: datetime($at), ord: $ord, payload: $payload})`,
		map[string]interface{}{"id": ev.EventID, "type": ev.Type, "org": ev.OrgID, "team": teamID,
			"at": ev.OccurredAt.Format(time.RFC3339Nano), "ord": seq.next(), "payload": string(payload)})
	return err
}

// teamEventTx writes a team-level event with the team's current state.
func teamEventTx(ctx context.Context, tx neo4j.ManagedTransaction, seq *outboxSeq, typ, teamID, actorID string) error {
	team, org, err := teamSnapshotTx(ctx, tx, teamID)
	if err != nil {
		return err
	}
	actor, err := subOf(ctx, tx, actorID)
	if err != nil {
		return err
	}
	return writeOutboxTx(ctx, tx, seq, OrgChangeEvent{Type: typ, OrgID: org, ActorSub: actor, Team: team})
}

// ownedEventTx writes a team.repo.* / team.source_app.* event: the item, and
// the team's state after the change.
func ownedEventTx(ctx context.Context, tx neo4j.ManagedTransaction, seq *outboxSeq, typ, teamID, kind, key, actorID string) error {
	team, org, err := teamSnapshotTx(ctx, tx, teamID)
	if err != nil {
		return err
	}
	actor, err := subOf(ctx, tx, actorID)
	if err != nil {
		return err
	}
	return writeOutboxTx(ctx, tx, seq, OrgChangeEvent{Type: typ, OrgID: org, ActorSub: actor, Team: team,
		Owned: &OrgChangeOwned{Kind: kind, Key: key}})
}

// memberEventTx writes a membership event (team-level when teamID != "",
// org-level otherwise).
func memberEventTx(ctx context.Context, tx neo4j.ManagedTransaction, seq *outboxSeq, typ, orgID, teamID, memberID, role, actorID string, validFrom time.Time) error {
	sub, err := subOf(ctx, tx, memberID)
	if err != nil {
		return err
	}
	actor, err := subOf(ctx, tx, actorID)
	if err != nil {
		return err
	}
	ev := OrgChangeEvent{Type: typ, OrgID: orgID, ActorSub: actor,
		Member: &OrgChangeMember{Sub: sub, Role: role, ValidFrom: validFrom.UTC()}}
	if teamID != "" {
		team, _, err := teamSnapshotTx(ctx, tx, teamID)
		if err != nil {
			return err
		}
		ev.Team = team
	}
	return writeOutboxTx(ctx, tx, seq, ev)
}

// orgEventWriter is the slice of *kafka.Writer the relay needs.
type orgEventWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// OrgChangeRelay publishes the outbox.
type OrgChangeRelay struct {
	neo4j    *database.Neo4jClient
	writer   orgEventWriter
	logger   *logger.Logger
	Interval time.Duration
	Batch    int
	// beforeMark is a test hook: called after a batch is written to Kafka and
	// before it is marked published. Returning an error simulates the relay
	// dying in exactly that gap.
	beforeMark func() error
}

// NewOrgChangeRelay builds a relay writing to brokers. Nil when brokers is empty.
func NewOrgChangeRelay(client *database.Neo4jClient, brokers []string, log *logger.Logger) *OrgChangeRelay {
	if len(brokers) == 0 {
		return nil
	}
	w := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: OrgChangesTopic, Balancer: &kafka.Hash{},
		BatchTimeout: 10 * time.Millisecond, WriteTimeout: 5 * time.Second, RequiredAcks: kafka.RequireAll}
	return &OrgChangeRelay{neo4j: client, writer: w, logger: log.WithService("org_change_relay"), Interval: 2 * time.Second, Batch: 100}
}

// NewOrgChangeRelayWithWriter is for tests.
func NewOrgChangeRelayWithWriter(client *database.Neo4jClient, w orgEventWriter, log *logger.Logger, beforeMark func() error) *OrgChangeRelay {
	return &OrgChangeRelay{neo4j: client, writer: w, logger: log.WithService("org_change_relay"), Interval: time.Second, Batch: 100, beforeMark: beforeMark}
}

// Run publishes until ctx is done.
func (r *OrgChangeRelay) Run(ctx context.Context) {
	if r == nil {
		return
	}
	r.logger.Info("org change relay started", zap.String("topic", OrgChangesTopic))
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		if n, err := r.PublishOnce(ctx); err != nil {
			r.logger.Warn("org change relay: publish failed (retrying)", zap.Error(err))
		} else if n > 0 {
			r.logger.Info("org change relay: published", zap.Int("events", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// PublishOnce publishes one batch of pending events, oldest first, and marks
// them. Returns the number published.
func (r *OrgChangeRelay) PublishOnce(ctx context.Context) (int, error) {
	res, err := r.neo4j.ExecuteQuery(ctx, `
		MATCH (e:OrgChangeOutbox) WHERE e.published_at IS NULL
		RETURN e.event_id AS id, e.type AS type, e.org_id AS org, e.payload AS payload, e.occurred_at AS at
		ORDER BY e.occurred_at, e.ord LIMIT $n`, map[string]interface{}{"n": r.Batch})
	if err != nil {
		return 0, err
	}
	if len(res.Records) == 0 {
		return 0, nil
	}
	msgs := make([]kafka.Message, 0, len(res.Records))
	ids := make([]string, 0, len(res.Records))
	for _, rec := range res.Records {
		id, _ := rec.Get("id")
		typ, _ := rec.Get("type")
		org, _ := rec.Get("org")
		payload, _ := rec.Get("payload")
		at, _ := rec.Get("at")
		ce := tasevents.NewWithID(id.(string), "com.tas.aether.org."+typ.(string), orgEventSource,
			tasevents.WithTenant(org.(string), ""), tasevents.WithSubject(org.(string)),
			tasevents.WithTime(at.(time.Time)), tasevents.WithRawData(json.RawMessage(payload.(string))))
		value, headers, err := kafkabind.Encode(ce)
		if err != nil {
			return 0, fmt.Errorf("encode %s: %w", id, err)
		}
		msgs = append(msgs, kafka.Message{Key: []byte(org.(string)), Value: value, Headers: headers, Time: ce.Time})
		ids = append(ids, id.(string))
	}
	if err := r.writer.WriteMessages(ctx, msgs...); err != nil {
		return 0, err // nothing marked: retried next time
	}
	if r.beforeMark != nil {
		if err := r.beforeMark(); err != nil {
			return 0, err
		}
	}
	if _, err := r.neo4j.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox) WHERE e.event_id IN $ids SET e.published_at = datetime()`,
		map[string]interface{}{"ids": ids}); err != nil {
		return 0, err // published but unmarked: re-published next time, deduped by event_id
	}
	return len(ids), nil
}

// PruneOutbox deletes published events older than keep.
func (r *OrgChangeRelay) PruneOutbox(ctx context.Context, keep time.Duration) error {
	if r == nil {
		return nil
	}
	_, err := r.neo4j.ExecuteQuery(ctx, `MATCH (e:OrgChangeOutbox) WHERE e.published_at < datetime() - duration($keep) DELETE e`,
		map[string]interface{}{"keep": fmt.Sprintf("PT%dS", int(keep.Seconds()))})
	return err
}
