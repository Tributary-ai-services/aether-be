package services

// The org token: the credential aether-be calls the tas-mcp proxy with
// (Plan #22 C.6).
//
// The proxy takes the caller's tenant from a TAS-Auth gateway token
// (tas_qg_live_*), resolved through aiqg-dashboard-be's /internal/auth/validate,
// and never from the request. aiqg-dashboard-be stores only token hashes, and
// no AIQG endpoint mints a token for a service, so aether-be cannot derive an
// org's token on its own. Until such an endpoint exists, an operator issues one
// gateway token per linked org in AIQG (source_app "aether") and puts it in a
// Secret that aether-be mounts as AETHER_MCP_ORG_TOKENS_FILE. That makes it a
// long-lived secret per org, and aether-be holds every linked org's; see the
// PR and data-models/aether-be/mcp-via-proxy.md for the trade-off.
//
// File format:
//
//	{
//	  "orgs": {
//	    "<aether organization id>": {
//	      "aiqg_tenant_id": "<AIQG tenant uuid the token was issued in>",
//	      "token": "tas_qg_live_..."
//	    }
//	  }
//	}
//
// Only organizations are mapped. A personal space, or an organization-type
// space with no owning Organization, gets no token and its call is refused:
// the token is never guessed.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// gatewayTokenPrefix is the shape of an AIQG gateway token. The proxy treats
// anything else as malformed and unattributed.
const gatewayTokenPrefix = "tas_qg_live_"

// OrgToken is the proxy credential for one Aether organization.
type OrgToken struct {
	AetherOrgID  string
	AIQGTenantID string
	token        string
}

// Bearer returns the token for the TAS-Auth header. It is the only accessor,
// so the value cannot reach a log through fmt.
func (t OrgToken) Bearer() string { return t.token }

// String never prints the token.
func (t OrgToken) String() string {
	return fmt.Sprintf("OrgToken{org=%s tenant=%s token=[REDACTED]}", t.AetherOrgID, t.AIQGTenantID)
}

// GoString never prints the token either (%#v).
func (t OrgToken) GoString() string { return t.String() }

// OrgTokenSource returns the proxy credential for a request's space.
type OrgTokenSource interface {
	TokenFor(sc *models.SpaceContext) (OrgToken, error)
}

// ErrNoOrgToken is wrapped by every "no token for this space" refusal.
var ErrNoOrgToken = fmt.Errorf("no MCP proxy token for this space")

// orgTokensFile is the on-disk shape.
type orgTokensFile struct {
	Orgs map[string]struct {
		AIQGTenantID string `json:"aiqg_tenant_id"`
		Token        string `json:"token"`
	} `json:"orgs"`
}

// FileOrgTokenSource reads the token file and re-reads it when its
// modification time or size changes (a Secret update swaps the file).
type FileOrgTokenSource struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	tokens  map[string]OrgToken
	loadErr error
}

// NewFileOrgTokenSource returns a source over path. A missing or invalid
// file is not fatal here: every lookup then fails closed with the reason.
func NewFileOrgTokenSource(path string) *FileOrgTokenSource {
	return &FileOrgTokenSource{path: path}
}

// TokenFor implements OrgTokenSource.
func (s *FileOrgTokenSource) TokenFor(sc *models.SpaceContext) (OrgToken, error) {
	orgID, err := proxyOrgOf(sc)
	if err != nil {
		return OrgToken{}, err
	}
	tokens, err := s.load()
	if err != nil {
		return OrgToken{}, err
	}
	t, ok := tokens[orgID]
	if !ok {
		return OrgToken{}, errors.ForbiddenWithDetails("This organization is not connected to the MCP proxy yet", map[string]any{
			"code":            "mcp_org_not_linked",
			"organization_id": orgID,
		})
	}
	return t, nil
}

// proxyOrgOf is the organization whose token a space uses, or a refusal.
func proxyOrgOf(sc *models.SpaceContext) (string, error) {
	if sc == nil {
		return "", errors.ForbiddenWithDetails("Space context required for MCP proxy calls", map[string]any{"code": "mcp_space_required"})
	}
	if sc.SpaceType != models.SpaceTypeOrganization || strings.TrimSpace(sc.OrganizationID) == "" {
		return "", errors.ForbiddenWithDetails("Shared MCP connections are available in organization spaces only", map[string]any{
			"code":       "mcp_org_space_required",
			"space_type": string(sc.SpaceType),
		})
	}
	return sc.OrganizationID, nil
}

func (s *FileOrgTokenSource) load() (map[string]OrgToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return nil, errors.ServiceUnavailable("MCP proxy tokens are not configured (AETHER_MCP_ORG_TOKENS_FILE)")
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		return nil, errors.ServiceUnavailable("MCP proxy tokens are unavailable")
	}
	if s.tokens != nil && fi.ModTime().Equal(s.modTime) && fi.Size() == s.size {
		return s.tokens, s.loadErr
	}

	tokens, perr := parseOrgTokens(s.path)
	s.modTime, s.size = fi.ModTime(), fi.Size()
	if perr != nil {
		// Keep nothing from a bad file: a half-read map could serve a stale
		// org's token after a rotation.
		s.tokens, s.loadErr = map[string]OrgToken{}, perr
		return nil, perr
	}
	s.tokens, s.loadErr = tokens, nil
	return tokens, nil
}

// parseOrgTokens reads and validates the file. Errors never quote a token.
func parseOrgTokens(path string) (map[string]OrgToken, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.ServiceUnavailable("MCP proxy tokens are unavailable")
	}
	var f orgTokensFile
	if err := json.Unmarshal(raw, &f); err != nil {
		// json errors can quote input; report the position-free reason only.
		return nil, errors.ServiceUnavailable("MCP proxy token file is not valid JSON")
	}
	out := make(map[string]OrgToken, len(f.Orgs))
	for org, e := range f.Orgs {
		org = strings.TrimSpace(org)
		tok := strings.TrimSpace(e.Token)
		if org == "" || !strings.HasPrefix(tok, gatewayTokenPrefix) || len(tok) <= len(gatewayTokenPrefix) {
			return nil, errors.ServiceUnavailable(fmt.Sprintf("MCP proxy token file: entry %q needs an AIQG gateway token (%s...)", org, gatewayTokenPrefix))
		}
		out[org] = OrgToken{AetherOrgID: org, AIQGTenantID: strings.ToLower(strings.TrimSpace(e.AIQGTenantID)), token: tok}
	}
	return out, nil
}

// StaticOrgTokenSource is an in-memory source, for tests and tools.
type StaticOrgTokenSource map[string]OrgToken

// NewOrgToken builds an OrgToken; for tests and tools.
func NewOrgToken(orgID, tenantID, bearer string) OrgToken {
	return OrgToken{AetherOrgID: orgID, AIQGTenantID: tenantID, token: bearer}
}

// TokenFor implements OrgTokenSource.
func (s StaticOrgTokenSource) TokenFor(sc *models.SpaceContext) (OrgToken, error) {
	orgID, err := proxyOrgOf(sc)
	if err != nil {
		return OrgToken{}, err
	}
	t, ok := s[orgID]
	if !ok {
		return OrgToken{}, errors.ForbiddenWithDetails("This organization is not connected to the MCP proxy yet", map[string]any{
			"code":            "mcp_org_not_linked",
			"organization_id": orgID,
		})
	}
	return t, nil
}
