package services

// Tenant-isolation lint (TEST-3).
//
// Every Cypher query in this service that touches a tenant-scoped node must
// constrain that node to the caller's tenant, or reach it through a node that
// is already so constrained. This test walks the Go source of internal/ and
// fails when a query does neither.
//
// It exists because the database cannot enforce this. The cluster runs Neo4j
// 5.15 Community, where property-existence and node-key constraints are
// Enterprise-only features — so "every Notebook carries a tenant_id" is not
// expressible as a schema constraint no matter how clean the data is. The only
// place the rule can live is in code, which means the only place it can be
// checked is a test like this one.
//
// WHAT COUNTS AS CONSTRAINED
//
//   - an inline property:      MATCH (n:Notebook {id: $id, tenant_id: $tenant_id})
//   - a WHERE predicate:       MATCH (n:Notebook) WHERE n.tenant_id = $tenant_id
//   - space_id instead:        Space and tenant are 1:1, enforced by the live
//     space_tenant_id_unique constraint, so either key isolates.
//   - a Space anchor:          MATCH (n:Notebook)-[:BELONGS_TO]->(:Space {id: $space_id})
//   - relationship reach:      a node joined to an already-constrained node in
//     the same clause inherits its isolation. This is how child nodes
//     (ChatMessage, WorkflowStep, WorkflowVersion, WorkflowTrigger) are
//     isolated — they are reached through their tenant-filtered parent rather
//     than by their own *_id property.
//
// WHAT THIS CHECK CANNOT SEE
//
// Queries assembled with fmt.Sprintf or strings.Join are only partly visible:
// the literal shows a bare MATCH and the filter arrives from a []string built
// at runtime. SearchChunks, SearchDocuments, SearchNotebooks and ListNotebooks
// are all real filters that this check cannot prove, so they carry an exemption
// explaining where the predicate is seeded. The live two-tenant tests in
// tests/integration/tenant_isolation_test.go are what actually cover those
// paths; this check and those tests are not redundant.
//
// It also reasons one query at a time. A query that is safe only because an
// earlier Go statement already fetched the node under a tenant filter looks
// unconstrained here, and is expected to carry an exemption saying so.
//
// EXEMPTING A QUERY
//
// Put "tenant-exempt: <reason>" in a comment directly above the query literal
// and say why no tenant applies — not that filtering was inconvenient. The
// reason belongs next to the query, not on the enclosing function, because the
// window below is deliberately small.
//
// Every exemption is also listed in wantExemptions. Adding one is therefore a
// visible diff in this file that a reviewer has to approve, rather than a
// comment that quietly widens the rule. If you add or remove an exemption,
// update that list; the test tells you exactly how it changed.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// tenantScopedLabels are the node labels that belong to exactly one tenant.
//
// Deliberately absent: User (a global identity, shared across spaces), and
// Space, Organization and Team — those three *define* tenancy rather than
// living inside it, so a tenant_id predicate on them is either circular or
// simply the wrong axis. They are isolated by membership traversal
// (MEMBER_OF, OWNS, organization_id) instead. See TEST-7.
var tenantScopedLabels = []string{
	"Notebook", "Document", "Agent", "Production", "Database",
	"WorkflowExecution", "WorkflowVersion", "WorkflowStep", "WorkflowTrigger",
	"Workflow", "ChatConversation", "ChatMessage", "Comment",
	"SavedQuery", "Notification", "Invitation", "Chunk",
}

// wantExemptions is the frozen set of queries allowed to skip the tenant
// filter, as "file.go:FunctionName". One entry per exempted query literal.
var wantExemptions = map[string]string{
	// Argo reports workflow completion through a webhook; the handler reaches
	// this via the executionUpdater interface with no user or space context.
	"workflow.go:UpdateExecutionStatus": "Argo status callback, no request context",

	// Filters are real but assembled at runtime, so no literal shows them.
	"chunk.go:SearchChunks":       "tenant_id seeded into whereConditions",
	"document.go:SearchDocuments": "tenant_id + space_id seeded into whereConditions",
	"notebook.go:SearchNotebooks": "tenant_id + space_id seeded into spaceWhereConditions",
	"notebook.go:ListNotebooks":   "space half filtered; shared half cross-space by design",

	// Sharing crosses spaces by design. The SHARED_WITH edge to a specific user
	// is the control, and only the owner can create one — createSharingRelationship
	// does require the notebook to be in the sharer's tenant.
	"notebook.go:GetSharedWithMe":     "cross-space sharing",
	"notebook.go:getSharePermission":  "cross-space sharing",
	"notebook.go:GetNotebookByID":     "cross-space shared fallback",
	"document.go:GetDocumentByID":     "cross-space shared fallback",
	"production.go:GetProductionByID": "cross-space shared fallback",

	// An invitation is cross-tenant by definition: the invitee belongs to a
	// different tenant from the inviter. The capability is the single-use token
	// or the caller's own verified email.
	"invitation.go:AcceptInvitation":          "invitation is cross-tenant by definition",
	"invitation.go:GetPendingInvitations":     "addressed to this user from other tenants",
	"invitation.go:ProcessPendingInvitations": "addressed to this user from other tenants",

	// Resolve the tenant rather than filter by it; the resolved value is bound
	// into the write that follows. Ids are service-held, not client-supplied.
	"document.go:UpdateProcessingResult":              "resolves tenant_id",
	"document.go:updateDocumentStatusWithJobID":       "resolves tenant_id",
	"document.go:updateDocumentStatus":                "resolves tenant_id",
	"document.go:updateDocumentWithProcessingResults": "resolves tenant_id",
	"document.go:updateDocumentStorage":               "resolves tenant_id",

	// Timer-driven or async writers with no request context at all.
	"document.go:RefreshProcessingResults":         "background sweeper",
	"document.go:deleteDocumentRecord":             "upload rollback, service-held id",
	"production.go:CleanupStaleProductions":        "background sweeper",
	"podcast_progress.go:updateNeo4jProgressPhase": "async progress writer",
	"podcast_progress.go:updateNeo4jCompleted":     "async progress writer",
	"podcast_progress.go:updateNeo4jFailed":        "async progress writer",

	// Platform-global rather than tenant data.
	"production.go:ListRenderers": "renderer catalogue is platform-global",

	// Admin integrity audit: it exists to look past the fields a tenant filter
	// would trust, and returns counts, not rows.
	"space.go:checkNotebookConsistency": "platform-wide integrity audit",
	"space.go:countOrphanedEntities":    "platform-wide orphan count",
	"space.go:getTotals":                "platform-wide totals",
}

// knownUnconstrained is the Space/Org/Team layer, tracked as TEST-7 rather than
// exempted: tenant_id is the wrong axis there, but the right one has not been
// settled. Listed so the count cannot grow unnoticed. Note that production
// holds zero Team nodes, so team.go is wholly unexercised against real data.
var knownUnconstrained = map[string]int{
	"team.go":         6,
	"organization.go": 4,
}

var (
	cypherKeyword = regexp.MustCompile(`(?i)\b(OPTIONAL MATCH|MATCH|MERGE|CREATE|SET|DELETE|DETACH DELETE|RETURN|WITH|WHERE|UNWIND|ORDER BY|LIMIT|SKIP|CALL|FOREACH|REMOVE|UNION)\b`)
	anyNodePat    = regexp.MustCompile(`\((\w+)(?:\s*:\s*\w+)?\s*(\{[^}]*\})?\s*\)`)
	spaceAnchor   = regexp.MustCompile(`\((\w*)\s*:\s*Space\s*(\{[^}]*\})?\s*\)`)
	tenantKeyProp = regexp.MustCompile(`\b(tenant_id|space_id)\b`)
	tenantKeyExpr = regexp.MustCompile(`(\w+)\.(tenant_id|space_id)\s*(=|IN|CONTAINS)`)
	funcDecl      = regexp.MustCompile(`(?m)^func (?:\([^)]*\)\s*)?(\w+)\(`)
	backtickStr   = regexp.MustCompile("`([^`]*)`")
)

// exemptionLookback is how many lines above a query literal are searched for
// the marker. Small on purpose: the reason must sit with the query it excuses,
// not on a function several statements away.
const exemptionLookback = 14

type cypherClause struct {
	keyword string
	text    string
}

func splitClauses(query string) []cypherClause {
	idx := cypherKeyword.FindAllStringSubmatchIndex(query, -1)
	out := make([]cypherClause, 0, len(idx))
	for i, m := range idx {
		end := len(query)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, cypherClause{
			keyword: strings.ToUpper(query[m[2]:m[3]]),
			text:    query[m[0]:end],
		})
	}
	return out
}

func isReadClause(kw string) bool {
	return kw == "MATCH" || kw == "OPTIONAL MATCH" || kw == "MERGE"
}

// unconstrainedPatterns returns the tenant-scoped node patterns in a query that
// are neither constrained themselves nor reachable from one that is.
func unconstrainedPatterns(query string, labelPat *regexp.Regexp) []string {
	clauses := splitClauses(query)
	safe := map[string]bool{}

	// Seed: a tenant-scoped node carrying tenant_id or space_id inline.
	for _, c := range clauses {
		for _, m := range labelPat.FindAllStringSubmatch(c.text, -1) {
			if tenantKeyProp.MatchString(m[3]) {
				safe[m[1]] = true
			}
		}
	}
	// Seed: anchored on one specific Space node (Space and tenant are 1:1).
	for _, c := range clauses {
		if !isReadClause(c.keyword) {
			continue
		}
		for _, m := range spaceAnchor.FindAllStringSubmatch(c.text, -1) {
			if m[1] != "" && regexp.MustCompile(`\b(id|tenant_id)\b`).MatchString(m[2]) {
				safe[m[1]] = true
			}
		}
	}
	// Seed: a WHERE (or inline) predicate on tenant_id / space_id.
	for _, c := range clauses {
		for _, m := range tenantKeyExpr.FindAllStringSubmatch(c.text, -1) {
			safe[m[1]] = true
		}
	}
	// Propagate along relationships to a fixpoint: a node joined to a
	// constrained node inherits its isolation.
	for range 8 {
		grew := false
		for _, c := range clauses {
			if !isReadClause(c.keyword) || !strings.Contains(c.text, "-") {
				continue
			}
			vars := []string{}
			anchored := false
			for _, m := range anyNodePat.FindAllStringSubmatch(c.text, -1) {
				vars = append(vars, m[1])
				if safe[m[1]] {
					anchored = true
				}
			}
			if !anchored {
				continue
			}
			for _, v := range vars {
				if !safe[v] {
					safe[v] = true
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}

	var bad []string
	for _, c := range clauses {
		if !isReadClause(c.keyword) {
			continue
		}
		for _, m := range labelPat.FindAllStringSubmatch(c.text, -1) {
			variable, label, props := m[1], m[2], m[3]
			if tenantKeyProp.MatchString(props) || (variable != "" && safe[variable]) {
				continue
			}
			bad = append(bad, c.keyword+" ("+variable+":"+label+")")
		}
	}
	return bad
}

func enclosingFunc(src string, offset int) string {
	name := "?"
	for _, m := range funcDecl.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= offset {
			break
		}
		name = src[m[2]:m[3]]
	}
	return name
}

func hasExemption(src string, offset int) bool {
	lines := strings.Split(src[:offset], "\n")
	if len(lines) > exemptionLookback {
		lines = lines[len(lines)-exemptionLookback:]
	}
	return strings.Contains(strings.Join(lines, "\n"), "tenant-exempt:")
}

func TestEveryTenantScopedQueryIsIsolated(t *testing.T) {
	labelPat := regexp.MustCompile(`\((\w*)\s*:\s*(` + strings.Join(tenantScopedLabels, "|") + `)\b\s*(\{[^}]*\})?\s*\)`)

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve internal/: %v", err)
	}

	gotExemptions := map[string]bool{}
	gotUnconstrained := map[string]int{}
	var violations []string

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		base := filepath.Base(path)

		for _, m := range backtickStr.FindAllStringSubmatchIndex(src, -1) {
			query := src[m[2]:m[3]]
			if !regexp.MustCompile(`\b(MATCH|MERGE)\b`).MatchString(query) {
				continue
			}
			bad := unconstrainedPatterns(query, labelPat)
			if len(bad) == 0 {
				continue
			}
			key := base + ":" + enclosingFunc(src, m[0])
			if hasExemption(src, m[0]) {
				gotExemptions[key] = true
				continue
			}
			if _, deferred := knownUnconstrained[base]; deferred {
				gotUnconstrained[base]++
				continue
			}
			line := strings.Count(src[:m[0]], "\n") + 1
			violations = append(violations, base+":"+strconv.Itoa(line)+" ("+enclosingFunc(src, m[0])+") "+strings.Join(bad, "; "))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d quer%s read or write a tenant-scoped node without binding it to a tenant.\n"+
			"Add tenant_id (or space_id) to the pattern, reach the node through a parent that\n"+
			"already has one, or — if no tenant applies — put a \"tenant-exempt: <reason>\"\n"+
			"comment directly above the query and add it to wantExemptions in this file.\n\n  %s",
			len(violations), plural(len(violations), "y", "ies"), strings.Join(violations, "\n  "))
	}

	// An exemption that disappears is as interesting as one that appears: it
	// usually means a query was refactored and the comment was left behind, or
	// that a path became filterable and nobody noticed.
	for key := range wantExemptions {
		if !gotExemptions[key] {
			t.Errorf("wantExemptions lists %q but no exempted query was found there — "+
				"if the query is now filtered, or moved, drop the entry.", key)
		}
	}
	for key := range gotExemptions {
		if _, ok := wantExemptions[key]; !ok {
			t.Errorf("%s carries a tenant-exempt comment that wantExemptions does not list. "+
				"Add it, with the reason, so the exemption is reviewed rather than assumed.", key)
		}
	}

	for file, want := range knownUnconstrained {
		if got := gotUnconstrained[file]; got != want {
			t.Errorf("%s has %d unconstrained tenant-scoped quer%s, expected %d (TEST-7). "+
				"If you fixed some, lower the number; if this grew, the Space/Org/Team layer "+
				"took on new unisolated reads.", file, got, plural(got, "y", "ies"), want)
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
