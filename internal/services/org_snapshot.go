package services

// Org snapshots for service-to-service reads (Plan #22 2.1/2.2). AIQG keeps a
// time-stamped copy of Aether's org structure: it seeds the copy from a
// snapshot, keeps it current from aether.org.changes.v1, and compares the two
// nightly. Members are identified by Keycloak sub (the key AIQG joins on).
// Archived teams are included: history refers to them.

import (
	"context"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// OrgSummary is one organization in the internal listing.
type OrgSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

// SnapshotTeam is a team's current state.
type SnapshotTeam struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Slug         string     `json:"slug,omitempty"`
	ParentTeamID string     `json:"parent_team_id,omitempty"`
	Visibility   string     `json:"visibility,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	// AE-36: what the team owns. Always present ([] when none).
	Repos      []string `json:"repos"`
	SourceApps []string `json:"source_apps"`
}

// SnapshotMember is one membership. TeamID is empty for an org membership.
type SnapshotMember struct {
	TeamID   string     `json:"team_id,omitempty"`
	Sub      string     `json:"sub"`
	Role     string     `json:"role,omitempty"`
	JoinedAt *time.Time `json:"joined_at,omitempty"`
}

// OrgSnapshot is the whole org structure at TakenAt.
type OrgSnapshot struct {
	Org         OrgSummary       `json:"org"`
	TakenAt     time.Time        `json:"taken_at"`
	Teams       []SnapshotTeam   `json:"teams"`
	TeamMembers []SnapshotMember `json:"team_members"`
	OrgMembers  []SnapshotMember `json:"org_members"`
}

// OrgSnapshotService reads org structure for internal callers.
type OrgSnapshotService struct {
	neo4j  *database.Neo4jClient
	logger *logger.Logger
}

// NewOrgSnapshotService constructs the service.
func NewOrgSnapshotService(client *database.Neo4jClient, log *logger.Logger) *OrgSnapshotService {
	return &OrgSnapshotService{neo4j: client, logger: log.WithService("org_snapshot")}
}

func strProp(v interface{}) string { s, _ := v.(string); return s }

func timeProp(v interface{}) *time.Time {
	if t, ok := v.(time.Time); ok {
		u := t.UTC()
		return &u
	}
	return nil
}

// ListOrgs returns every organization.
func (s *OrgSnapshotService) ListOrgs(ctx context.Context) ([]OrgSummary, error) {
	res, err := s.neo4j.ExecuteQuery(ctx, `MATCH (o:Organization) RETURN o.id AS id, o.name AS name, o.slug AS slug ORDER BY o.name`, nil)
	if err != nil {
		return nil, err
	}
	out := make([]OrgSummary, 0, len(res.Records))
	for _, r := range res.Records {
		id, _ := r.Get("id")
		name, _ := r.Get("name")
		slug, _ := r.Get("slug")
		out = append(out, OrgSummary{ID: strProp(id), Name: strProp(name), Slug: strProp(slug)})
	}
	return out, nil
}

// Snapshot returns one org's teams and memberships, or (nil, nil) when the
// org does not exist.
func (s *OrgSnapshotService) Snapshot(ctx context.Context, orgID string) (*OrgSnapshot, error) {
	p := map[string]interface{}{"org": orgID}
	org, err := s.neo4j.ExecuteQuery(ctx, `MATCH (o:Organization {id: $org}) RETURN o.name AS name, o.slug AS slug`, p)
	if err != nil {
		return nil, err
	}
	if len(org.Records) == 0 {
		return nil, nil
	}
	name, _ := org.Records[0].Get("name")
	slug, _ := org.Records[0].Get("slug")
	snap := &OrgSnapshot{Org: OrgSummary{ID: orgID, Name: strProp(name), Slug: strProp(slug)}, TakenAt: time.Now().UTC(),
		Teams: []SnapshotTeam{}, TeamMembers: []SnapshotMember{}, OrgMembers: []SnapshotMember{}}

	teams, err := s.neo4j.ExecuteQuery(ctx, `
		MATCH (t:Team {organization_id: $org})
		RETURN t.id AS id, t.name AS name, t.slug AS slug, t.parent_team_id AS parent, t.visibility AS vis,
		       t.created_at AS created, t.archived_at AS archived,
		       coalesce(t.repos, []) AS repos, coalesce(t.source_apps, []) AS apps
		ORDER BY t.created_at`, p)
	if err != nil {
		return nil, err
	}
	for _, r := range teams.Records {
		g := func(k string) interface{} { v, _ := r.Get(k); return v }
		snap.Teams = append(snap.Teams, SnapshotTeam{ID: strProp(g("id")), Name: strProp(g("name")), Slug: strProp(g("slug")),
			ParentTeamID: strProp(g("parent")), Visibility: strProp(g("vis")), CreatedAt: timeProp(g("created")), ArchivedAt: timeProp(g("archived")),
			Repos: stringList(g("repos")), SourceApps: stringList(g("apps"))})
	}
	tm, err := s.neo4j.ExecuteQuery(ctx, `
		MATCH (u:User)-[r:MEMBER_OF]->(t:Team {organization_id: $org})
		RETURN t.id AS team, u.keycloak_id AS sub, r.role AS role, r.joined_at AS joined
		ORDER BY t.id, r.joined_at`, p)
	if err != nil {
		return nil, err
	}
	for _, r := range tm.Records {
		g := func(k string) interface{} { v, _ := r.Get(k); return v }
		snap.TeamMembers = append(snap.TeamMembers, SnapshotMember{TeamID: strProp(g("team")), Sub: strProp(g("sub")),
			Role: strProp(g("role")), JoinedAt: timeProp(g("joined"))})
	}
	om, err := s.neo4j.ExecuteQuery(ctx, `
		MATCH (u:User)-[r:MEMBER_OF]->(:Organization {id: $org})
		RETURN u.keycloak_id AS sub, r.role AS role, r.joined_at AS joined
		ORDER BY r.joined_at`, p)
	if err != nil {
		return nil, err
	}
	for _, r := range om.Records {
		g := func(k string) interface{} { v, _ := r.Get(k); return v }
		snap.OrgMembers = append(snap.OrgMembers, SnapshotMember{Sub: strProp(g("sub")), Role: strProp(g("role")), JoinedAt: timeProp(g("joined"))})
	}
	return snap, nil
}
