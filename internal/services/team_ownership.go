package services

// Team-owned repos and source apps (AE-36, Plan #22 design §5.1).
//
//   t.repos        list of "owner/name" GitHub repos, lowercase, sorted, unique
//   t.source_apps  list of AIQG source_app values, case preserved, sorted, unique
//
// They are the non-human members of a team: AIQG's proposer (§7, the
// repo_majority / repo_team rule) attributes traffic to the team that owns the
// repo or source app it came from. Writes are allowed to a team owner/admin or
// an org owner/admin (requireManagerTx), refuse an archived team, and write one
// team.repo.* / team.source_app.* event per item added or removed in the same
// transaction as the change.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Owned-resource kinds. They match AIQG's team_member_history.member_kind.
const (
	OwnedKindRepo      = "repo"
	OwnedKindSourceApp = "source_app"
)

// Bounds on what one team may own.
const (
	MaxTeamRepos      = 200
	MaxTeamSourceApps = 100
	MaxSourceAppLen   = 100
)

var (
	// GitHub owner: letters, digits and single inner hyphens, at most 39.
	repoOwnerRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9]|-[a-z0-9]){0,38}$`)
	// GitHub repo name: letters, digits, '.', '_' and '-', at most 100.
	repoNameRe = regexp.MustCompile(`^[a-z0-9._-]{1,100}$`)
	// A source_app: starts with a letter or digit, then letters, digits and
	// . _ - : @ +. No spaces and no '/', so it can sit in a URL path segment.
	sourceAppRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+-]*$`)
)

// NormalizeRepo returns the canonical "owner/name" form of a repo: trimmed and
// lowercased (GitHub names are case-insensitive), or a validation error.
func NormalizeRepo(s string) (string, error) {
	r := strings.ToLower(strings.TrimSpace(s))
	owner, name, ok := strings.Cut(r, "/")
	if !ok || !repoOwnerRe.MatchString(owner) || !repoNameRe.MatchString(name) || name == "." || name == ".." {
		return "", errors.ValidationWithDetails(`Invalid repo: use "owner/name", e.g. "Tributary-ai-services/aiqg-ui"`,
			map[string]interface{}{"code": "invalid_repo", "repo": s})
	}
	return owner + "/" + name, nil
}

// NormalizeSourceApp returns a trimmed source_app or a validation error. Case
// is preserved: AIQG matches source_app exactly, as its tokens were issued.
func NormalizeSourceApp(s string) (string, error) {
	a := strings.TrimSpace(s)
	if len(a) > MaxSourceAppLen || !sourceAppRe.MatchString(a) {
		return "", errors.ValidationWithDetails(
			fmt.Sprintf("Invalid source app: 1-%d letters, digits or . _ - : @ +, starting with a letter or digit", MaxSourceAppLen),
			map[string]interface{}{"code": "invalid_source_app", "source_app": s})
	}
	return a, nil
}

func normalizerFor(kind string) func(string) (string, error) {
	if kind == OwnedKindRepo {
		return NormalizeRepo
	}
	return NormalizeSourceApp
}

func maxFor(kind string) int {
	if kind == OwnedKindRepo {
		return MaxTeamRepos
	}
	return MaxTeamSourceApps
}

func propFor(kind string) string {
	if kind == OwnedKindRepo {
		return "repos"
	}
	return "source_apps"
}

// NormalizeOwnedList normalizes every entry, drops duplicates and sorts. The
// count bound is checked after deduplication.
func NormalizeOwnedList(kind string, in []string) ([]string, error) {
	norm := normalizerFor(kind)
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		v, err := norm(s)
		if err != nil {
			return nil, err
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) > maxFor(kind) {
		return nil, errors.ValidationWithDetails(fmt.Sprintf("A team may own at most %d %s", maxFor(kind), propFor(kind)),
			map[string]interface{}{"code": "too_many_" + propFor(kind), "count": len(out)})
	}
	sort.Strings(out)
	return out, nil
}

// OwnedDiff returns what moving from old to new adds and removes, each sorted.
func OwnedDiff(old, new []string) (added, removed []string) {
	o := make(map[string]bool, len(old))
	for _, v := range old {
		o[v] = true
	}
	n := make(map[string]bool, len(new))
	for _, v := range new {
		n[v] = true
		if !o[v] {
			added = append(added, v)
		}
	}
	for _, v := range old {
		if !n[v] {
			removed = append(removed, v)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// OwnedChange is one write to a team's repos or source apps: Set replaces the
// whole list; otherwise Add and Remove apply (Add first). Removing an entry
// the team does not own is a 404, so a typo is not silently a no-op.
type OwnedChange struct {
	Kind   string // OwnedKindRepo | OwnedKindSourceApp
	Set    *[]string
	Add    []string
	Remove []string
}

func ownedEventTypes(kind string) (added, removed string) {
	if kind == OwnedKindRepo {
		return EvTeamRepoAdded, EvTeamRepoRemoved
	}
	return EvTeamSourceAppAdded, EvTeamSourceAppRemoved
}

// stringList reads a Neo4j list property ([]interface{} of strings).
func stringList(v interface{}) []string {
	raw, _ := v.([]interface{})
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ChangeTeamOwned applies ch to a team and returns the team as userID sees it.
func (s *TeamService) ChangeTeamOwned(ctx context.Context, teamID string, ch OwnedChange, userID string) (*models.Team, error) {
	if ch.Kind != OwnedKindRepo && ch.Kind != OwnedKindSourceApp {
		return nil, fmt.Errorf("unknown owned kind %q", ch.Kind)
	}
	prop := propFor(ch.Kind)
	norm := normalizerFor(ch.Kind)
	// Validate the input before opening a transaction.
	var set []string
	if ch.Set != nil {
		var err error
		if set, err = NormalizeOwnedList(ch.Kind, *ch.Set); err != nil {
			return nil, err
		}
	}
	add, err := NormalizeOwnedList(ch.Kind, ch.Add)
	if err != nil {
		return nil, err
	}
	remove := make([]string, 0, len(ch.Remove))
	for _, r := range ch.Remove {
		v, err := norm(r)
		if err != nil {
			return nil, err
		}
		remove = append(remove, v)
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) { c.AccessMode = neo4j.AccessModeWrite })
	defer session.Close(ctx)
	var added, removed []string
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		st, err := s.readTeamState(ctx, tx, teamID)
		if err != nil {
			return nil, err
		}
		if st.Archived {
			return nil, errTeamArchived(teamID)
		}
		if err := s.requireManagerTx(ctx, tx, teamID, st.OrgID, userID, "change a team's "+prop); err != nil {
			return nil, err
		}
		res, err := tx.Run(ctx, fmt.Sprintf(`MATCH (t:Team {id: $id}) RETURN coalesce(t.%s, []) AS cur`, prop),
			map[string]interface{}{"id": teamID})
		if err != nil {
			return nil, err
		}
		var cur []string
		if res.Next(ctx) {
			v, _ := res.Record().Get("cur")
			cur = stringList(v)
		}
		next := set
		if ch.Set == nil {
			has := make(map[string]bool, len(cur))
			for _, v := range cur {
				has[v] = true
			}
			for _, r := range remove {
				if !has[r] {
					return nil, errors.NotFoundWithDetails("The team does not own that "+strings.ReplaceAll(ch.Kind, "_", " "),
						map[string]interface{}{"code": ch.Kind + "_not_owned", ch.Kind: r, "team_id": teamID})
				}
			}
			next, err = NormalizeOwnedList(ch.Kind, append(append([]string{}, cur...), add...))
			if err != nil {
				return nil, err
			}
			drop := make(map[string]bool, len(remove))
			for _, r := range remove {
				drop[r] = true
			}
			kept := next[:0]
			for _, v := range next {
				if !drop[v] {
					kept = append(kept, v)
				}
			}
			next = kept
		}
		added, removed = OwnedDiff(cur, next)
		if len(added) == 0 && len(removed) == 0 {
			return nil, nil // nothing changed: no write, no event
		}
		var stored interface{} = next
		if len(next) == 0 {
			stored = nil // an empty list is stored as no property
		}
		if _, err := tx.Run(ctx, fmt.Sprintf(`MATCH (t:Team {id: $id}) SET t.%s = $list, t.updated_at = datetime($now)`, prop),
			map[string]interface{}{"id": teamID, "list": stored, "now": time.Now().Format(time.RFC3339)}); err != nil {
			return nil, err
		}
		evAdd, evRemove := ownedEventTypes(ch.Kind)
		seq := &outboxSeq{}
		for _, v := range removed {
			if err := ownedEventTx(ctx, tx, seq, evRemove, teamID, ch.Kind, v, userID); err != nil {
				return nil, err
			}
		}
		for _, v := range added {
			if err := ownedEventTx(ctx, tx, seq, evAdd, teamID, ch.Kind, v, userID); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		if _, ok := errors.AsAPIError(err); ok {
			return nil, err
		}
		s.logger.Error("Failed to change team "+prop, zap.Error(err), zap.String("team_id", teamID))
		return nil, errors.DatabaseWithDetails("Failed to change team "+prop, err, map[string]interface{}{"team_id": teamID})
	}
	if len(added)+len(removed) > 0 {
		s.logger.Info("Team "+prop+" changed", zap.String("team_id", teamID), zap.Strings("added", added),
			zap.Strings("removed", removed), zap.String("changed_by", userID))
	}
	return s.GetTeam(ctx, teamID, userID)
}
