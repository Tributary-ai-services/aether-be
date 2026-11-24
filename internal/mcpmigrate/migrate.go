// Package mcpmigrate copies an organization's Aether database connections
// into AIQG's shared MCP connection store (Plan #22 C.6).
//
// One run handles one AIQG tenant: the tenant of the AIQG token it runs with.
// AIQG writes a connection into the caller's own tenant and nowhere else, so
// the destination is decided by AIQG, not by this job. The job's part is to
// copy only that tenant's linked Aether organization's connections:
//
//  1. read the tenant's org link (GET /api/v1/admin/accounts/aether-links);
//  2. list the PostgreSQL Database nodes of that org's spaces (the legacy org
//     space and every HAS_SPACE space), each constrained to its own space's
//     Aether tenant;
//  3. read the tenant's existing connections (GET /api/v1/mcp/connections);
//  4. plan, and with Apply, create the connection
//     (POST /api/v1/mcp/connections, kind database_dsn) and record its id on
//     the Database node.
//
// Properties the plan guarantees:
//   - Dry run unless Apply is set.
//   - Idempotent: a connection is recognized by the marker "aether-db:<id>" in
//     its label, so a rerun after any partial failure creates nothing new and
//     only finishes the binding.
//   - Never deletes or edits Aether's copy (node properties, K8s Secret) or an
//     existing AIQG connection. It adds three properties to the node.
//   - AIQG holds one live connection per (tenant, server). With several
//     candidate connections the job binds none unless the operator names one.
//   - Never prints, logs or returns a secret.
package mcpmigrate

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Candidate is one Aether Database node that might be copied.
type Candidate struct {
	ID              string
	Name            string
	TenantID        string // Aether tenant of the node
	SpaceID         string // the space the node lives in
	SpaceTenantID   string // tenant of that space, found through the org
	Host            string
	Port            int
	Database        string
	SSLMode         string
	SecretName      string
	SecretNamespace string
	MCPServerID     string
	MCPConnectionID string
}

// Link is AIQG's account <-> Aether link (aiqg-dashboard-be store.AccountLink).
type Link struct {
	TenantID       string `json:"tenant_id"`
	DisplayName    string `json:"display_name,omitempty"`
	Kind           string `json:"kind,omitempty"`
	AetherOrgID    string `json:"aether_org_id,omitempty"`
	AetherUserID   string `json:"aether_user_id,omitempty"`
	AetherTenantID string `json:"aether_tenant_id,omitempty"`
}

// Connection is AIQG's ConnectionView: never a secret.
type Connection struct {
	ID       string            `json:"id"`
	ServerID string            `json:"server_id"`
	Label    string            `json:"label"`
	Kind     string            `json:"kind"`
	Params   map[string]string `json:"params"`
	KeyLast4 string            `json:"key_last4"`
}

// NewConnection is the POST body. Secret is the DSN.
type NewConnection struct {
	ServerID string            `json:"server_id"`
	Label    string            `json:"label"`
	Kind     string            `json:"kind"`
	Params   map[string]string `json:"params"`
	Secret   string            `json:"secret"`
}

// Graph reads and binds Aether Database nodes.
type Graph interface {
	// OrgTenant returns the Aether tenant id of an Organization, "" if none.
	OrgTenant(ctx context.Context, orgID string) (string, error)
	// Candidates lists the org's PostgreSQL connections: those in the legacy
	// org space (in orgTenant, the org's own tenant) and those in a space the
	// org owns through HAS_SPACE (in that space's tenant). Each carries the
	// tenant of the space it was found through.
	Candidates(ctx context.Context, orgID, orgTenant string) ([]Candidate, error)
	// Bind records the shared connection on the node, only when the node is
	// unbound or already bound to connID. It reports whether a node changed
	// or already held connID.
	Bind(ctx context.Context, dbID, aetherTenant, serverID, connID string) (bool, error)
}

// Credentials reads a connection's login.
type Credentials interface {
	Credentials(ctx context.Context, c Candidate) (username, password string, err error)
}

// AIQG is the slice of the AIQG API the job uses, as the caller's tenant.
type AIQG interface {
	Links(ctx context.Context) ([]Link, error)
	Connections(ctx context.Context) ([]Connection, error)
	Create(ctx context.Context, in NewConnection) (*Connection, error)
}

// Options for one run.
type Options struct {
	TenantID   string // the AIQG tenant the token writes to (from its claim)
	ServerID   string // the proxy server id, normally postgres-mcp
	DatabaseID string // choose one candidate when there are several
	Apply      bool
	// RefusedSecrets are credentials Secrets never copied (default:
	// SEC44Secrets). A connection that logs in with one is refused.
	RefusedSecrets []string
	// SharedHostsAllowed are tas-shared hosts the operator has cleared for
	// copying (they must also be on the proxy's egress allowlist, MCP-19).
	// Any other tas-shared host is refused.
	SharedHostsAllowed []string
}

// SEC44Secrets are the Secrets in tas-mcp-servers known to hold the tasuser
// superuser password (BACKLOG SEC-44). Copying one would put a superuser
// login into a tenant's stored connection.
var SEC44Secrets = []string{"dbhub-secrets", "db-credentials-0d462710", "db-credentials-335d056c"}

// superuserLogins are shared-platform logins never copied into a tenant's
// connection, whichever Secret they are stored in (SEC-43/SEC-44).
var superuserLogins = map[string]bool{"tasuser": true, "postgres": true}

// sharedHostNames are tas-shared services reachable by a bare name.
var sharedHostNames = map[string]bool{"postgres-shared": true, "tas-postgres-shared": true}

// isSharedHost reports whether host is shared TAS infrastructure (namespace
// tas-shared): the platform's own databases, never a tenant's.
func isSharedHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if sharedHostNames[h] {
		return true
	}
	for _, suf := range []string{".tas-shared", ".tas-shared.svc", ".tas-shared.svc.cluster.local"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

// refusal returns why a candidate must never be copied, or "".
func refusal(c Candidate, opt Options) string {
	refused := opt.RefusedSecrets
	if refused == nil {
		refused = SEC44Secrets
	}
	for _, s := range refused {
		if c.SecretName != "" && c.SecretName == s {
			return "its credentials Secret " + s + " holds a superuser login (SEC-44); re-create the connection with a limited role"
		}
	}
	if isSharedHost(c.Host) {
		for _, h := range opt.SharedHostsAllowed {
			if strings.EqualFold(strings.TrimSpace(h), c.Host) {
				return ""
			}
		}
		return "its host is shared TAS infrastructure (tas-shared) and not on -shared-hosts-allowed / the proxy egress allowlist"
	}
	return ""
}

// Action kinds.
const (
	ActCreate       = "create"
	ActBind         = "bind"           // AIQG already has it (marker); record the id
	ActDone         = "already_shared" // nothing to do
	ActConflict     = "conflict"       // AIQG holds another connection for the server
	ActAmbiguous    = "ambiguous"      // several candidates, none chosen
	ActNotSelected  = "not_selected"   // another candidate was chosen
	ActNoCreds      = "no_credentials" // no readable login
	ActUnsupported  = "unsupported"    // values postgres-mcp cannot carry
	ActStaleBinding = "stale_binding"  // node names a connection AIQG no longer has
	ActRefused      = "refused"        // a superuser secret (SEC-44) or an uncleared tas-shared host
	ActError        = "error"
)

// Step is one planned or applied action. It never holds a secret.
type Step struct {
	DatabaseID   string
	Name         string
	Host         string
	Database     string
	Action       string
	Reason       string
	ConnectionID string
	Applied      bool
}

// Report is a run's outcome.
type Report struct {
	TenantID       string
	AetherOrgID    string
	AetherTenantID string
	ServerID       string
	Apply          bool
	Steps          []Step
}

// Failed reports whether any step needs an operator.
func (r *Report) Failed() bool {
	for _, s := range r.Steps {
		switch s.Action {
		case ActConflict, ActAmbiguous, ActError, ActStaleBinding, ActRefused:
			return true
		case ActCreate, ActBind:
			if r.Apply && !s.Applied {
				return true
			}
		}
	}
	return false
}

// Marker is how a copied connection is recognized in AIQG.
func Marker(dbID string) string { return "aether-db:" + dbID }

const maxLabelLen = 128

// Label is the AIQG label for a copied connection: the Aether name plus the
// marker, which is never truncated away.
func Label(c Candidate) string {
	suffix := " (" + Marker(c.ID) + ")"
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "Aether connection"
	}
	if len(name)+len(suffix) > maxLabelLen {
		name = strings.TrimSpace(name[:maxLabelLen-len(suffix)])
	}
	return name + suffix
}

// bridgeSafe is what postgres-mcp accepts in host, username, database and
// SSL mode: the bridge puts them into a URL unescaped, and the proxy refuses
// any other value (tas-mcp docs/TENANT_SERVERS.md, "Connection required").
var bridgeSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// DSN builds the database URL. Every part is escaped.
func DSN(c Candidate, username, password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(username, password),
		Host: c.Host + ":" + strconv.Itoa(portOr(c.Port)), Path: "/" + c.Database}
	if c.SSLMode != "" {
		u.RawQuery = url.Values{"sslmode": {c.SSLMode}}.Encode()
	}
	return u.String()
}

func portOr(p int) int {
	if p <= 0 {
		return 5432
	}
	return p
}

// Run plans and, with Apply, applies one tenant's migration.
func Run(ctx context.Context, g Graph, creds Credentials, api AIQG, opt Options) (*Report, error) {
	if opt.ServerID == "" {
		opt.ServerID = "postgres-mcp"
	}
	tenant := strings.ToLower(strings.TrimSpace(opt.TenantID))
	if tenant == "" {
		return nil, fmt.Errorf("the AIQG token carries no tenant_id claim")
	}
	rep := &Report{TenantID: tenant, ServerID: opt.ServerID, Apply: opt.Apply}

	links, err := api.Links(ctx)
	if err != nil {
		return nil, fmt.Errorf("read AIQG org links: %w", err)
	}
	var link *Link
	for i := range links {
		if strings.EqualFold(links[i].TenantID, tenant) {
			link = &links[i]
			break
		}
	}
	if link == nil || link.Kind != "org" || link.AetherOrgID == "" {
		return nil, fmt.Errorf("AIQG tenant %s is not linked to an Aether organization; link it first (Plan #22 2.1)", tenant)
	}
	rep.AetherOrgID = link.AetherOrgID

	orgTenant, err := g.OrgTenant(ctx, link.AetherOrgID)
	if err != nil {
		return nil, fmt.Errorf("read Aether organization: %w", err)
	}
	if orgTenant == "" {
		return nil, fmt.Errorf("aether organization %s not found or has no tenant", link.AetherOrgID)
	}
	if link.AetherTenantID != "" && link.AetherTenantID != orgTenant {
		return nil, fmt.Errorf("the AIQG link names Aether tenant %s but organization %s is in %s; fix the link before migrating",
			link.AetherTenantID, link.AetherOrgID, orgTenant)
	}
	rep.AetherTenantID = orgTenant

	cands, err := g.Candidates(ctx, link.AetherOrgID, orgTenant)
	if err != nil {
		return nil, fmt.Errorf("list Aether connections: %w", err)
	}
	conns, err := api.Connections(ctx)
	if err != nil {
		return nil, fmt.Errorf("read AIQG connections: %w", err)
	}
	var existing *Connection
	byID := map[string]Connection{}
	for i := range conns {
		byID[conns[i].ID] = conns[i]
		if conns[i].ServerID == opt.ServerID {
			existing = &conns[i]
		}
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })

	// Classify every candidate before writing anything.
	var pending []int
	for i := range cands {
		c := cands[i]
		if c.TenantID == "" || c.TenantID != c.SpaceTenantID {
			// Defense in depth: the graph query already constrains this.
			rep.Steps = append(rep.Steps, step(c, ActError, "node is not in the tenant of an org space; not copied"))
			continue
		}
		if c.MCPConnectionID != "" {
			if cv, ok := byID[c.MCPConnectionID]; ok && cv.ServerID == opt.ServerID {
				s := step(c, ActDone, "already in the shared store")
				s.ConnectionID = c.MCPConnectionID
				rep.Steps = append(rep.Steps, s)
			} else {
				rep.Steps = append(rep.Steps, step(c, ActStaleBinding, "the node names a connection this tenant no longer has; re-add it in AIQG, then clear mcp_connection_id"))
			}
			continue
		}
		if existing != nil && strings.Contains(existing.Label, Marker(c.ID)) {
			s := step(c, ActBind, "AIQG already holds this connection; recording its id")
			s.ConnectionID = existing.ID
			rep.Steps = append(rep.Steps, s)
			continue
		}
		pending = append(pending, len(rep.Steps))
		rep.Steps = append(rep.Steps, step(c, ActCreate, ""))
	}

	// AIQG holds one connection per (tenant, server).
	switch {
	case existing != nil && len(pending) > 0 && !anyBind(rep.Steps):
		for _, i := range pending {
			rep.Steps[i].Action = ActConflict
			rep.Steps[i].Reason = fmt.Sprintf("this tenant already has a %s connection (%q); AIQG keeps one per server", opt.ServerID, existing.Label)
		}
		pending = nil
	case existing != nil && len(pending) > 0:
		for _, i := range pending {
			rep.Steps[i].Action = ActNotSelected
			rep.Steps[i].Reason = "another connection of this org is the shared one; AIQG keeps one per server"
		}
		pending = nil
	case len(pending) > 1 && opt.DatabaseID == "":
		for _, i := range pending {
			rep.Steps[i].Action = ActAmbiguous
			rep.Steps[i].Reason = fmt.Sprintf("%d PostgreSQL connections and AIQG keeps one per server; rerun with -database-id", len(pending))
		}
		pending = nil
	case len(pending) > 0 && opt.DatabaseID != "":
		var chosen []int
		for _, i := range pending {
			if rep.Steps[i].DatabaseID == opt.DatabaseID {
				chosen = append(chosen, i)
			} else {
				rep.Steps[i].Action = ActNotSelected
				rep.Steps[i].Reason = "not the -database-id chosen"
			}
		}
		pending = chosen
	}

	byStepID := map[string]Candidate{}
	for _, c := range cands {
		byStepID[c.ID] = c
	}

	for i := range rep.Steps {
		s := &rep.Steps[i]
		c := byStepID[s.DatabaseID]
		switch s.Action {
		case ActBind:
			if opt.Apply {
				applyBind(ctx, g, c, opt.ServerID, s)
			}
		case ActCreate:
			if !contains(pending, i) {
				continue
			}
			if why := refusal(c, opt); why != "" {
				s.Action, s.Reason = ActRefused, why
				continue
			}
			if !bridgeSafe.MatchString(c.Host) || !bridgeSafe.MatchString(c.Database) || (c.SSLMode != "" && !bridgeSafe.MatchString(c.SSLMode)) {
				s.Action, s.Reason = ActUnsupported, "host, database or SSL mode has characters postgres-mcp cannot carry"
				continue
			}
			user, pass, err := creds.Credentials(ctx, c)
			if err != nil || user == "" || pass == "" {
				s.Action, s.Reason = ActNoCreds, "no readable stored login for this connection; add it in AIQG by hand"
				continue
			}
			if superuserLogins[strings.ToLower(strings.TrimSpace(user))] {
				s.Action, s.Reason = ActRefused, "its stored login is a shared superuser (SEC-43/SEC-44); re-create the connection with a limited role"
				continue
			}
			if !bridgeSafe.MatchString(user) {
				s.Action, s.Reason = ActUnsupported, "the username has characters postgres-mcp cannot carry"
				continue
			}
			s.Reason = "copy to the shared store as " + opt.ServerID
			if !opt.Apply {
				continue
			}
			out, err := api.Create(ctx, NewConnection{ServerID: opt.ServerID, Label: Label(c), Kind: "database_dsn",
				Params: map[string]string{"argument": "_connection", "format": "object"}, Secret: DSN(c, user, pass)})
			if err != nil {
				s.Action, s.Reason = ActError, "AIQG refused the connection: "+err.Error()
				continue
			}
			s.ConnectionID = out.ID
			applyBind(ctx, g, c, opt.ServerID, s)
		}
	}
	return rep, nil
}

func applyBind(ctx context.Context, g Graph, c Candidate, serverID string, s *Step) {
	ok, err := g.Bind(ctx, c.ID, c.TenantID, serverID, s.ConnectionID)
	switch {
	case err != nil:
		s.Action, s.Reason = ActError, "the connection is in AIQG but recording it on the node failed (rerun to finish): "+err.Error()
	case !ok:
		s.Action, s.Reason = ActError, "the node is bound to a different connection; not changed"
	default:
		s.Applied = true
	}
}

func anyBind(steps []Step) bool {
	for _, s := range steps {
		if s.Action == ActBind {
			return true
		}
	}
	return false
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func step(c Candidate, action, reason string) Step {
	return Step{DatabaseID: c.ID, Name: c.Name, Host: c.Host, Database: c.Database, Action: action, Reason: reason}
}
