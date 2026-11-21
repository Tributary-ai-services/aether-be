package services

// Team structure (Plan #22 A.2): nesting, slugs, the org edge and archiving.
//
//   (:Team)-[:CHILD_OF]->(:Team)      at most one parent, same org, no cycles
//   (:Team)-[:BELONGS_TO]->(:Organization)
//   t.slug                            unique per org (constraint team_org_slug_unique)
//   t.archived_at                     a team is archived, never deleted
//
// t.parent_team_id mirrors the CHILD_OF edge so reads need no extra match; both
// are written in the same transaction and nothing else writes either.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a team name into its default slug: lowercase ASCII letters and
// digits joined by single hyphens, at most 60 characters. "" when nothing is
// left (the caller then falls back to the team id).
func Slugify(name string) string {
	s := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	return s
}

// ValidSlug reports whether s is a well-formed slug.
func ValidSlug(s string) bool {
	return s != "" && len(s) <= 60 && Slugify(s) == s
}

// ErrTeamCycle is returned when a move would make a team its own ancestor.
var ErrTeamCycle = errors.ConflictWithDetails("A team cannot be moved under itself or one of its descendants",
	map[string]interface{}{"code": "team_cycle"})

// ValidateTeamParent checks a proposed parent for teamID. parentAncestors is
// the new parent followed by every team above it (CHILD_OF*0..), so the move
// creates a cycle exactly when teamID is in that chain. An empty parentID
// (top level) is always valid.
func ValidateTeamParent(teamID, parentID string, parentAncestors []string) error {
	if parentID == "" {
		return nil
	}
	if parentID == teamID {
		return ErrTeamCycle
	}
	for _, a := range parentAncestors {
		if a == teamID {
			return ErrTeamCycle
		}
	}
	return nil
}

// teamWriteState is what every structural write checks first.
type teamWriteState struct {
	OrgID    string
	Archived bool
}

func (s *TeamService) readTeamState(ctx context.Context, tx neo4j.ManagedTransaction, teamID string) (*teamWriteState, error) {
	res, err := tx.Run(ctx, `MATCH (t:Team {id: $id}) RETURN t.organization_id AS org, t.archived_at IS NOT NULL AS archived`,
		map[string]interface{}{"id": teamID})
	if err != nil {
		return nil, err
	}
	if !res.Next(ctx) {
		return nil, errors.NotFoundWithDetails("Team not found", map[string]interface{}{"team_id": teamID})
	}
	org, _ := res.Record().Get("org")
	arch, _ := res.Record().Get("archived")
	st := &teamWriteState{Archived: arch == true}
	st.OrgID, _ = org.(string)
	return st, nil
}

// errTeamArchived refuses writes to an archived team.
func errTeamArchived(teamID string) error {
	return errors.ConflictWithDetails("Team is archived", map[string]interface{}{"code": "team_archived", "team_id": teamID})
}

// setParentTx points teamID at parentID ("" = top level) inside tx: same org,
// parent not archived, no cycle. Replaces any existing CHILD_OF edge.
func (s *TeamService) setParentTx(ctx context.Context, tx neo4j.ManagedTransaction, teamID, orgID, parentID string) error {
	if parentID != "" {
		ps, err := s.readTeamState(ctx, tx, parentID)
		if err != nil {
			if errors.IsNotFound(err) {
				return errors.ValidationWithDetails("Parent team not found", map[string]interface{}{"parent_team_id": parentID})
			}
			return err
		}
		if ps.OrgID != orgID {
			return errors.ValidationWithDetails("A parent team must be in the same organization",
				map[string]interface{}{"code": "parent_other_org", "parent_team_id": parentID})
		}
		if ps.Archived {
			return errors.ValidationWithDetails("Parent team is archived",
				map[string]interface{}{"code": "parent_archived", "parent_team_id": parentID})
		}
		res, err := tx.Run(ctx, `MATCH (p:Team {id: $p})-[:CHILD_OF*0..]->(a:Team) RETURN collect(a.id) AS chain`,
			map[string]interface{}{"p": parentID})
		if err != nil {
			return err
		}
		var chain []string
		if res.Next(ctx) {
			raw, _ := res.Record().Get("chain")
			for _, v := range raw.([]interface{}) {
				chain = append(chain, v.(string))
			}
		}
		if err := ValidateTeamParent(teamID, parentID, chain); err != nil {
			return err
		}
	}
	_, err := tx.Run(ctx, `
		MATCH (t:Team {id: $id})
		OPTIONAL MATCH (t)-[old:CHILD_OF]->()
		DELETE old
		WITH DISTINCT t
		OPTIONAL MATCH (p:Team {id: $parent})
		FOREACH (_ IN CASE WHEN p IS NULL THEN [] ELSE [1] END | CREATE (t)-[:CHILD_OF]->(p))
		SET t.parent_team_id = CASE WHEN p IS NULL THEN null ELSE p.id END`,
		map[string]interface{}{"id": teamID, "parent": parentID})
	return err
}

// orgRoleOf returns userID's role in orgID ("" when not a member).
func (s *TeamService) orgRoleOf(ctx context.Context, tx neo4j.ManagedTransaction, orgID, userID string) (string, error) {
	res, err := tx.Run(ctx, `
		MATCH (o:Organization {id: $org})<-[r:MEMBER_OF]-(u:User)
		WHERE u.id = $user OR u.keycloak_id = $user
		RETURN r.role AS role LIMIT 1`, map[string]interface{}{"org": orgID, "user": userID})
	if err != nil {
		return "", err
	}
	if !res.Next(ctx) {
		return "", nil
	}
	r, _ := res.Record().Get("role")
	role, _ := r.(string)
	return role, nil
}

// teamRoleOf returns userID's role in teamID ("" when not a member).
func (s *TeamService) teamRoleOf(ctx context.Context, tx neo4j.ManagedTransaction, teamID, userID string) (string, error) {
	res, err := tx.Run(ctx, `
		MATCH (t:Team {id: $team})<-[r:MEMBER_OF]-(u:User)
		WHERE u.id = $user OR u.keycloak_id = $user
		RETURN r.role AS role LIMIT 1`, map[string]interface{}{"team": teamID, "user": userID})
	if err != nil {
		return "", err
	}
	if !res.Next(ctx) {
		return "", nil
	}
	r, _ := res.Record().Get("role")
	role, _ := r.(string)
	return role, nil
}

func isManagerRole(r string) bool { return r == "owner" || r == "admin" }

// MoveTeam reparents a team (parentID "" = top level). Allowed for a manager of
// the team who is also a manager of the new parent, or for an org owner/admin.
func (s *TeamService) MoveTeam(ctx context.Context, teamID, parentID, userID string) error {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeWrite })
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, errTeamArchived(teamID)
		}
		orgRole, err := s.orgRoleOf(ctx, tx, st.OrgID, userID)
		if err != nil {
			return nil, err
		}
		if !isManagerRole(orgRole) {
			tr, err := s.teamRoleOf(ctx, tx, teamID, userID)
			if err != nil {
				return nil, err
			}
			allowed := isManagerRole(tr)
			if allowed && parentID != "" {
				pr, err := s.teamRoleOf(ctx, tx, parentID, userID)
				if err != nil {
					return nil, err
				}
				allowed = isManagerRole(pr)
			}
			if !allowed {
				return nil, errors.ForbiddenWithDetails("Moving a team needs a manager of the team and of its new parent, or an org owner/admin",
					map[string]interface{}{"team_id": teamID})
			}
		}
		if err := s.setParentTx(ctx, tx, teamID, st.OrgID, parentID); err != nil {
			return nil, err
		}
		if _, err = tx.Run(ctx, `MATCH (t:Team {id: $id}) SET t.updated_at = datetime($now)`,
			map[string]interface{}{"id": teamID, "now": time.Now().Format(time.RFC3339)}); err != nil {
			return nil, err
		}
		return nil, teamEventTx(ctx, tx, &outboxSeq{}, EvTeamMoved, teamID, userID)
	})
	if err != nil {
		return err
	}
	s.logger.Info("Team moved", zap.String("team_id", teamID), zap.String("parent_team_id", parentID), zap.String("moved_by", userID))
	return nil
}

// ArchiveTeam archives a team: hidden from listings, refuses writes, keeps its
// node, member edges, notebooks and CHILD_OF edges for history (Plan #22 A.2;
// it used to DETACH DELETE the team and its notebooks). Team owners and org
// owners/admins only.
func (s *TeamService) ArchiveTeam(ctx context.Context, teamID, userID string) error {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeWrite })
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, nil // idempotent
		}
		orgRole, err := s.orgRoleOf(ctx, tx, st.OrgID, userID)
		if err != nil {
			return nil, err
		}
		if !isManagerRole(orgRole) {
			tr, err := s.teamRoleOf(ctx, tx, teamID, userID)
			if err != nil {
				return nil, err
			}
			if tr != "owner" {
				return nil, errors.ForbiddenWithDetails("Only team owners or org owners/admins can archive a team",
					map[string]interface{}{"user_role": tr, "team_id": teamID})
			}
		}
		if _, err = tx.Run(ctx, `MATCH (t:Team {id: $id}) SET t.archived_at = datetime(), t.archived_reason = 'archived', t.updated_at = datetime()`,
			map[string]interface{}{"id": teamID}); err != nil {
			return nil, err
		}
		return nil, teamEventTx(ctx, tx, &outboxSeq{}, EvTeamArchived, teamID, userID)
	})
	if err != nil {
		return err
	}
	s.logger.Info("Team archived", zap.String("team_id", teamID), zap.String("archived_by", userID))
	return nil
}

// assignSlugTx sets the team's slug: the requested one (validated) or one
// derived from the name, made unique within the org by a numeric suffix. The
// database constraint team_org_slug_unique is the final guard.
func (s *TeamService) assignSlugTx(ctx context.Context, tx neo4j.ManagedTransaction, teamID, orgID, requested, name string) (string, error) {
	base := requested
	if base != "" {
		if !ValidSlug(base) {
			return "", errors.ValidationWithDetails("Invalid slug: use lowercase letters, digits and single hyphens",
				map[string]interface{}{"slug": requested})
		}
	} else if base = Slugify(name); base == "" {
		base = "team"
	}
	res, err := tx.Run(ctx, `MATCH (t:Team {organization_id: $org}) WHERE t.id <> $id AND t.slug IS NOT NULL RETURN collect(t.slug) AS taken`,
		map[string]interface{}{"org": orgID, "id": teamID})
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	if res.Next(ctx) {
		raw, _ := res.Record().Get("taken")
		for _, v := range raw.([]interface{}) {
			taken[v.(string)] = true
		}
	}
	slug := base
	if taken[slug] {
		if requested != "" {
			return "", errors.ConflictWithDetails("Slug already used in this organization",
				map[string]interface{}{"code": "slug_taken", "slug": requested})
		}
		for i := 2; taken[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
	}
	if _, err := tx.Run(ctx, `MATCH (t:Team {id: $id}) SET t.slug = $slug`, map[string]interface{}{"id": teamID, "slug": slug}); err != nil {
		return "", err
	}
	return slug, nil
}

// structuralUpdate applies the A.2 parts of a TeamUpdateRequest before the
// plain property update: refuse an archived team, then slug, then parent.
func (s *TeamService) structuralUpdate(ctx context.Context, teamID string, req models.TeamUpdateRequest, userID string) error {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeWrite })
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, errTeamArchived(teamID)
		}
		if req.Slug != nil {
			if !ValidSlug(*req.Slug) {
				return nil, errors.ValidationWithDetails("Invalid slug: use lowercase letters, digits and single hyphens",
					map[string]interface{}{"slug": *req.Slug})
			}
			if _, err := s.assignSlugTx(ctx, tx, teamID, st.OrgID, *req.Slug, ""); err != nil {
				return nil, err
			}
			if req.Name == nil { // a name change emits team.renamed in UpdateTeam's own transaction
				if err := teamEventTx(ctx, tx, &outboxSeq{}, EvTeamRenamed, teamID, userID); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	if req.ParentTeamID != nil {
		return s.MoveTeam(ctx, teamID, *req.ParentTeamID, userID)
	}
	return nil
}

// ensureWritable refuses membership writes to an archived (or missing) team.
func (s *TeamService) ensureWritable(ctx context.Context, teamID string) error {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeRead })
	defer session.Close(ctx)
	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, errTeamArchived(teamID)
		}
		return nil, nil
	})
	return err
}
