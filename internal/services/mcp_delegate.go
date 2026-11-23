package services

// The delegate credential: how aether-be acts for an org on the tas-mcp
// proxy (Plan #22 C.6, AIQG-88).
//
// aether-be holds ONE service credential, which the proxy accepts only for
// tool invoke (tools/list and tools/call), never for registry writes,
// broadcast or LLM traffic. On each call aether-be names who it acts for:
//
//	TAS-MCP-Delegate-Token:    the credential
//	TAS-MCP-On-Behalf-Of-Org:  the Aether organization of the caller's space
//	TAS-MCP-On-Behalf-Of-User: the caller's Keycloak sub
//
// The proxy asks aiqg-dashboard-be which AIQG tenant the org is linked to and
// whether the user is a member of it, and only then injects that tenant's
// stored connection. A wrong org, an unlinked org or a non-member is refused
// by the proxy, not trusted from here.
//
// The credential is read from AETHER_MCP_DELEGATE_TOKEN_FILE (a mounted
// Secret, re-read when it changes, so rotation needs no restart), or else
// from AETHER_MCP_DELEGATE_TOKEN. It is never logged or formatted.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DelegateCredential returns the current delegate credential.
type DelegateCredential interface {
	Token() (string, error)
}

// ProxyCaller is who a proxied call acts for. It holds no secret.
type ProxyCaller struct {
	AetherOrgID string
	UserSub     string
}

// CallerFor returns the org and user a space's call acts for, or a refusal.
// Only organization spaces with an owning Organization qualify: a personal
// space, or an org-type space with no HAS_SPACE edge, never borrows an org.
func CallerFor(sc *models.SpaceContext) (ProxyCaller, error) {
	if sc == nil {
		return ProxyCaller{}, errors.ForbiddenWithDetails("Space context required for MCP proxy calls", map[string]any{"code": "mcp_space_required"})
	}
	if sc.SpaceType != models.SpaceTypeOrganization || strings.TrimSpace(sc.OrganizationID) == "" {
		return ProxyCaller{}, errors.ForbiddenWithDetails("Shared MCP connections are available in organization spaces only", map[string]any{
			"code":       "mcp_org_space_required",
			"space_type": string(sc.SpaceType),
		})
	}
	if strings.TrimSpace(sc.UserID) == "" {
		return ProxyCaller{}, errors.ForbiddenWithDetails("A signed-in user is required for MCP proxy calls", map[string]any{"code": "mcp_user_required"})
	}
	return ProxyCaller{AetherOrgID: sc.OrganizationID, UserSub: sc.UserID}, nil
}

// minDelegateTokenLen matches the proxy's startup rule.
const minDelegateTokenLen = 32

func checkDelegateToken(tok string) error {
	if len(tok) < minDelegateTokenLen || strings.ContainsAny(tok, " \t\r\n") {
		return errors.ServiceUnavailable("MCP delegate credential is invalid (AETHER_MCP_DELEGATE_TOKEN_FILE)")
	}
	return nil
}

// StaticDelegateCredential is a fixed credential (env, tests).
type StaticDelegateCredential string

// Token implements DelegateCredential.
func (s StaticDelegateCredential) Token() (string, error) {
	tok := strings.TrimSpace(string(s))
	if tok == "" {
		return "", errors.ServiceUnavailable("MCP delegate credential is not configured (AETHER_MCP_DELEGATE_TOKEN_FILE)")
	}
	if err := checkDelegateToken(tok); err != nil {
		return "", err
	}
	return tok, nil
}

// FileDelegateCredential reads the credential from a file and re-reads it when
// the file's modification time or size changes (a Secret update swaps it).
type FileDelegateCredential struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	token   string
	loaded  bool
}

// NewFileDelegateCredential returns a credential over path.
func NewFileDelegateCredential(path string) *FileDelegateCredential {
	return &FileDelegateCredential{path: path}
}

// Token implements DelegateCredential. Errors never quote the file.
func (f *FileDelegateCredential) Token() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fi, err := os.Stat(f.path)
	if err != nil {
		return "", errors.ServiceUnavailable("MCP delegate credential is unavailable")
	}
	if f.loaded && fi.ModTime().Equal(f.modTime) && fi.Size() == f.size {
		if f.token == "" {
			return "", errors.ServiceUnavailable("MCP delegate credential is invalid (AETHER_MCP_DELEGATE_TOKEN_FILE)")
		}
		return f.token, nil
	}
	// The path is AETHER_MCP_DELEGATE_TOKEN_FILE, set by the operator in the
	// Deployment; no request can choose it.
	raw, err := os.ReadFile(filepath.Clean(f.path)) // #nosec G304 -- operator-configured path
	if err != nil {
		return "", errors.ServiceUnavailable("MCP delegate credential is unavailable")
	}
	f.modTime, f.size, f.loaded = fi.ModTime(), fi.Size(), true
	tok := strings.TrimSpace(string(raw))
	if err := checkDelegateToken(tok); err != nil {
		// Keep nothing from a bad file, so a stale value is never sent.
		f.token = ""
		return "", err
	}
	f.token = tok
	return tok, nil
}

// NewDelegateCredential picks the file when a path is set, else the value.
func NewDelegateCredential(path, value string) DelegateCredential {
	if strings.TrimSpace(path) != "" {
		return NewFileDelegateCredential(path)
	}
	return StaticDelegateCredential(value)
}
