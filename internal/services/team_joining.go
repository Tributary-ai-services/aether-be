package services

// Joining teams (Plan #22 A.3, design §5.1).
//
//   visibility = organization  an org member may join (self-join)
//   visibility = public        anyone signed in may join
//   visibility = private       a request, approved or denied by a team manager
//
// Joining a team in an org the person is not in adds an implied org `member`
// edge, as on GitHub. Requests are (:User)-[:JOIN_REQUEST {id, status, ...}]->(:Team);
// a decided request is kept (status approved/denied) for the record.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Join outcomes.
const (
	JoinStatusJoined    = "joined"
	JoinStatusRequested = "requested"
)

// ValidateJoinedAt refuses a joined_at in the future (back-dating is allowed:
// "a member since 1 October"). A one-minute allowance absorbs clock skew.
func ValidateJoinedAt(at time.Time, now time.Time) error {
	if at.After(now.Add(time.Minute)) {
		return errors.ValidationWithDetails("joined_at cannot be in the future", map[string]interface{}{"joined_at": at})
	}
	return nil
}

// resolveUserTx returns the Neo4j User.id for an internal id or a Keycloak id.
func resolveUserTx(ctx context.Context, tx neo4j.ManagedTransaction, userID string) (string, error) {
	res, err := tx.Run(ctx, `MATCH (u:User) WHERE u.id = $u OR u.keycloak_id = $u RETURN u.id AS id LIMIT 1`,
		map[string]interface{}{"u": userID})
	if err != nil {
		return "", err
	}
	if !res.Next(ctx) {
		return "", errors.NotFoundWithDetails("User not found", map[string]interface{}{"user_id": userID})
	}
	id, _ := res.Record().Get("id")
	return id.(string), nil
}

// addMemberTx adds a team membership and, when needed, the implied org
// `member` edge. joinedAt is the (possibly back-dated) membership start.
func (s *TeamService) addMemberTx(ctx context.Context, tx neo4j.ManagedTransaction, teamID, orgID, userID, role, invitedBy string, joinedAt time.Time) error {
	_, err := tx.Run(ctx, `
		MATCH (t:Team {id: $team}), (u:User {id: $user})
		CREATE (u)-[:MEMBER_OF {role: $role, joined_at: datetime($joined), invited_by: $by}]->(t)
		WITH u
		MATCH (o:Organization {id: $org})
		MERGE (u)-[m:MEMBER_OF]->(o)
		ON CREATE SET m.role = 'member', m.joined_at = datetime($joined), m.implied_by_team = $team`,
		map[string]interface{}{"team": teamID, "user": userID, "role": role, "by": invitedBy,
			"joined": joinedAt.UTC().Format(time.RFC3339), "org": orgID})
	return err
}

// JoinTeam joins (or asks to join) a team. Returns JoinStatusJoined or
// JoinStatusRequested.
func (s *TeamService) JoinTeam(ctx context.Context, teamID, userID string) (string, error) {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeWrite })
	defer session.Close(ctx)
	out, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, errTeamArchived(teamID)
		}
		uid, err := resolveUserTx(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if role, err := s.teamRoleOf(ctx, tx, teamID, uid); err != nil {
			return nil, err
		} else if role != "" {
			return nil, errors.ConflictWithDetails("Already a member of this team",
				map[string]interface{}{"code": "already_member", "role": role})
		}
		res, err := tx.Run(ctx, `MATCH (t:Team {id: $id}) RETURN t.visibility AS v`, map[string]interface{}{"id": teamID})
		if err != nil {
			return nil, err
		}
		vis := ""
		if res.Next(ctx) {
			v, _ := res.Record().Get("v")
			vis, _ = v.(string)
		}
		orgRole, err := s.orgRoleOf(ctx, tx, st.OrgID, uid)
		if err != nil {
			return nil, err
		}
		switch {
		case vis == "public", vis == "organization" && orgRole != "":
			if err := s.addMemberTx(ctx, tx, teamID, st.OrgID, uid, "member", uid, time.Now()); err != nil {
				return nil, err
			}
			return JoinStatusJoined, nil
		case vis == "organization":
			return nil, errors.ForbiddenWithDetails("Only members of the organization can join this team",
				map[string]interface{}{"code": "not_org_member", "team_id": teamID})
		default: // private: one pending request per person
			_, err := tx.Run(ctx, `
				MATCH (t:Team {id: $team}), (u:User {id: $user})
				OPTIONAL MATCH (u)-[p:JOIN_REQUEST {status: 'pending'}]->(t)
				WITH t, u, p
				FOREACH (_ IN CASE WHEN p IS NULL THEN [1] ELSE [] END |
				  CREATE (u)-[:JOIN_REQUEST {id: $rid, status: 'pending', requested_at: datetime()}]->(t))`,
				map[string]interface{}{"team": teamID, "user": uid, "rid": uuid.New().String()})
			if err != nil {
				return nil, err
			}
			return JoinStatusRequested, nil
		}
	})
	if err != nil {
		return "", err
	}
	s.logger.Info("Team join", zap.String("team_id", teamID), zap.String("user_id", userID), zap.String("outcome", out.(string)))
	return out.(string), nil
}

// requireManagerTx allows a team owner/admin or an org owner/admin.
func (s *TeamService) requireManagerTx(ctx context.Context, tx neo4j.ManagedTransaction, teamID, orgID, userID, what string) error {
	orgRole, err := s.orgRoleOf(ctx, tx, orgID, userID)
	if err != nil {
		return err
	}
	if isManagerRole(orgRole) {
		return nil
	}
	tr, err := s.teamRoleOf(ctx, tx, teamID, userID)
	if err != nil {
		return err
	}
	if isManagerRole(tr) {
		return nil
	}
	return errors.ForbiddenWithDetails("Only team owners/admins or org owners/admins can "+what,
		map[string]interface{}{"team_id": teamID})
}

// ListJoinRequests returns a team's pending requests (managers only).
func (s *TeamService) ListJoinRequests(ctx context.Context, teamID, userID string) ([]models.TeamJoinRequest, error) {
	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeRead })
	defer session.Close(ctx)
	out, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if err := s.requireManagerTx(ctx, tx, teamID, st.OrgID, userID, "see join requests"); err != nil {
			return nil, err
		}
		res, err := tx.Run(ctx, `
			MATCH (u:User)-[q:JOIN_REQUEST {status: 'pending'}]->(t:Team {id: $team})
			RETURN q.id AS id, u.id AS user_id, u.email AS email, u.full_name AS name, q.requested_at AS at
			ORDER BY q.requested_at`, map[string]interface{}{"team": teamID})
		if err != nil {
			return nil, err
		}
		list := []models.TeamJoinRequest{}
		for res.Next(ctx) {
			r := res.Record()
			q := models.TeamJoinRequest{TeamID: teamID, Status: "pending"}
			if v, ok := r.Get("id"); ok {
				q.ID, _ = v.(string)
			}
			if v, ok := r.Get("user_id"); ok {
				q.UserID, _ = v.(string)
			}
			if v, ok := r.Get("email"); ok {
				q.Email, _ = v.(string)
			}
			if v, ok := r.Get("name"); ok {
				q.Name, _ = v.(string)
			}
			if v, ok := r.Get("at"); ok {
				q.RequestedAt, _ = v.(time.Time)
			}
			list = append(list, q)
		}
		return list, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return out.([]models.TeamJoinRequest), nil
}

// DecideJoinRequest approves or denies a pending request (managers only). An
// approval adds the person as a member (and the implied org edge); joinedAt,
// when set, back-dates the membership.
func (s *TeamService) DecideJoinRequest(ctx context.Context, teamID, requestID string, approve bool, joinedAt *time.Time, userID string) error {
	if joinedAt != nil {
		if err := ValidateJoinedAt(*joinedAt, time.Now()); err != nil {
			return err
		}
	}
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
		if err := s.requireManagerTx(ctx, tx, teamID, st.OrgID, userID, "decide join requests"); err != nil {
			return nil, err
		}
		decider, err := resolveUserTx(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		res, err := tx.Run(ctx, `
			MATCH (u:User)-[q:JOIN_REQUEST {id: $rid, status: 'pending'}]->(t:Team {id: $team})
			SET q.status = $status, q.decided_by = $by, q.decided_at = datetime()
			RETURN u.id AS user_id`,
			map[string]interface{}{"rid": requestID, "team": teamID, "by": decider,
				"status": map[bool]string{true: "approved", false: "denied"}[approve]})
		if err != nil {
			return nil, err
		}
		if !res.Next(ctx) {
			return nil, errors.NotFoundWithDetails("No pending join request with that id", map[string]interface{}{"request_id": requestID})
		}
		if !approve {
			return nil, nil
		}
		uidV, _ := res.Record().Get("user_id")
		uid := uidV.(string)
		if role, err := s.teamRoleOf(ctx, tx, teamID, uid); err != nil {
			return nil, err
		} else if role != "" {
			return nil, nil // already a member (added another way); the request is still closed
		}
		at := time.Now()
		if joinedAt != nil {
			at = *joinedAt
		}
		return nil, s.addMemberTx(ctx, tx, teamID, st.OrgID, uid, "member", decider, at)
	})
	return err
}

// SetMemberJoinedAt back-dates (or corrects) when a member joined the team.
// Team owners/admins and org owners/admins only.
func (s *TeamService) SetMemberJoinedAt(ctx context.Context, teamID, targetUserID string, joinedAt time.Time, userID string) error {
	if err := ValidateJoinedAt(joinedAt, time.Now()); err != nil {
		return err
	}
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
		if err := s.requireManagerTx(ctx, tx, teamID, st.OrgID, userID, "change when someone joined"); err != nil {
			return nil, err
		}
		res, err := tx.Run(ctx, `
			MATCH (u:User)-[r:MEMBER_OF]->(t:Team {id: $team})
			WHERE u.id = $target OR u.keycloak_id = $target
			SET r.joined_at = datetime($at)
			RETURN count(r) AS n`,
			map[string]interface{}{"team": teamID, "target": targetUserID, "at": joinedAt.UTC().Format(time.RFC3339)})
		if err != nil {
			return nil, err
		}
		if res.Next(ctx) {
			if n, _ := res.Record().Get("n"); n.(int64) == 0 {
				return nil, errors.NotFoundWithDetails("Not a member of this team", map[string]interface{}{"user_id": targetUserID})
			}
		}
		return nil, nil
	})
	return err
}

// MyJoinRequestStatus returns the caller's pending request for the team, if any
// ("pending" or "").
func (s *TeamService) MyJoinRequestStatus(ctx context.Context, teamID, userID string) (string, error) {
	res, err := s.neo4j.ExecuteQuery(ctx, `
		MATCH (u:User)-[q:JOIN_REQUEST {status: 'pending'}]->(:Team {id: $team})
		WHERE u.id = $user OR u.keycloak_id = $user
		RETURN q.status AS s LIMIT 1`, map[string]interface{}{"team": teamID, "user": userID})
	if err != nil {
		return "", err
	}
	if len(res.Records) == 0 {
		return "", nil
	}
	v, _ := res.Records[0].Get("s")
	str, _ := v.(string)
	return str, nil
}
