package mcpmigrate

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Tributary-ai-services/aether-be/internal/database"
)

// ---- Neo4j ----------------------------------------------------------------

// Neo4jGraph implements Graph over aether-be's Neo4j client.
type Neo4jGraph struct{ DB *database.Neo4jClient }

// orgTenantQuery reads an Organization's Aether tenant. It is the lookup that
// yields the tenant: the id comes from AIQG's org link for the caller's own
// AIQG tenant, and every later query is constrained to the tenant it returns.
const orgTenantQuery = `
	MATCH (o:Organization {id: $org_id})
	RETURN o.tenant_id AS tenant_id
	LIMIT 1
`

// OrgTenant implements Graph.
func (g Neo4jGraph) OrgTenant(ctx context.Context, orgID string) (string, error) {
	res, err := g.DB.ExecuteQueryWithLogging(ctx, orgTenantQuery, map[string]any{"org_id": orgID})
	if err != nil {
		return "", err
	}
	if len(res.Records) == 0 {
		return "", nil
	}
	v, _ := res.Records[0].Get("tenant_id")
	s, _ := v.(string)
	return s, nil
}

// candidatesQuery lists an org's PostgreSQL connections: those created in
// the legacy org space (space_id = the org id) and those in a Space the org
// owns through HAS_SPACE, in the org's tenant only.
const candidatesQuery = `
	MATCH (d:Database)
	WHERE d.tenant_id = $tenant_id AND d.type = 'postgres'
	  AND (d.space_id = $org_id OR EXISTS {
	        MATCH (:Organization {id: $org_id})-[:HAS_SPACE]->(sp:Space {tenant_id: $tenant_id})
	        WHERE sp.id = d.space_id })
	RETURN d.id AS id, d.name AS name, d.tenant_id AS tenant_id, d.host AS host, d.port AS port,
	       d.database AS database, d.ssl_mode AS ssl_mode, d.secret_name AS secret_name,
	       d.secret_namespace AS secret_namespace, d.mcp_server_id AS mcp_server_id,
	       d.mcp_connection_id AS mcp_connection_id
	ORDER BY d.id
`

// Candidates implements Graph.
func (g Neo4jGraph) Candidates(ctx context.Context, orgID, tenant string) ([]Candidate, error) {
	res, err := g.DB.ExecuteQueryWithLogging(ctx, candidatesQuery, map[string]any{"org_id": orgID, "tenant_id": tenant})
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(res.Records))
	for _, r := range res.Records {
		get := func(k string) string {
			v, _ := r.Get(k)
			s, _ := v.(string)
			return s
		}
		port := 0
		if v, ok := r.Get("port"); ok {
			if n, ok := v.(int64); ok {
				port = int(n)
			}
		}
		out = append(out, Candidate{ID: get("id"), Name: get("name"), TenantID: get("tenant_id"), Host: get("host"), Port: port,
			Database: get("database"), SSLMode: get("ssl_mode"), SecretName: get("secret_name"), SecretNamespace: get("secret_namespace"),
			MCPServerID: get("mcp_server_id"), MCPConnectionID: get("mcp_connection_id")})
	}
	return out, nil
}

// bindQuery records the shared connection, only on an unbound node or one
// already bound to the same connection.
const bindQuery = `
	MATCH (d:Database {id: $db_id})
	WHERE d.tenant_id = $tenant_id AND (d.mcp_connection_id IS NULL OR d.mcp_connection_id = $conn_id)
	SET d.mcp_server_id = $server_id,
	    d.mcp_connection_id = $conn_id,
	    d.mcp_migrated_at = coalesce(d.mcp_migrated_at, datetime())
	RETURN d.id AS id
`

// Bind implements Graph.
func (g Neo4jGraph) Bind(ctx context.Context, dbID, tenant, serverID, connID string) (bool, error) {
	if connID == "" {
		return false, fmt.Errorf("no connection id to record")
	}
	res, err := g.DB.ExecuteQueryWithLogging(ctx, bindQuery, map[string]any{
		"db_id": dbID, "tenant_id": tenant, "server_id": serverID, "conn_id": connID})
	if err != nil {
		return false, err
	}
	return len(res.Records) == 1, nil
}

// ---- Kubernetes Secrets ------------------------------------------------------

// SecretCredentials reads the login aether-be stored for a connection
// (db-credentials-<id>, keys username and password).
type SecretCredentials struct{ K8s kubernetes.Interface }

// Credentials implements Credentials. Errors name the Secret, never a value.
func (s SecretCredentials) Credentials(ctx context.Context, c Candidate) (string, string, error) {
	if s.K8s == nil || c.SecretName == "" {
		return "", "", fmt.Errorf("no stored login")
	}
	ns := c.SecretNamespace
	if ns == "" {
		ns = "tas-mcp-servers"
	}
	sec, err := s.K8s.CoreV1().Secrets(ns).Get(ctx, c.SecretName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("read secret %s/%s", ns, c.SecretName)
	}
	return string(sec.Data["username"]), string(sec.Data["password"]), nil
}

// ---- AIQG API ---------------------------------------------------------------

// HTTPAIQG calls aiqg-dashboard-be's public API with an AIQG user token.
type HTTPAIQG struct {
	BaseURL string
	Bearer  string
	HTTP    *http.Client
}

func (a HTTPAIQG) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Bearer)
	req.Header.Set("Content-Type", "application/json")
	hc := a.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		// AIQG's error bodies carry a code and a message and never quote a
		// secret (aiqg-dashboard-be mcpconfig: "never include the parse
		// error: it quotes the DSN"). Only the code and message are kept.
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("%s %s: HTTP %d %s %s", method, path, resp.StatusCode, e.Error.Code, e.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Links implements AIQG.
func (a HTTPAIQG) Links(ctx context.Context) ([]Link, error) {
	var out struct {
		Links []Link `json:"links"`
	}
	err := a.do(ctx, http.MethodGet, "/api/v1/admin/accounts/aether-links", nil, &out)
	return out.Links, err
}

// Connections implements AIQG.
func (a HTTPAIQG) Connections(ctx context.Context) ([]Connection, error) {
	var out struct {
		Connections []Connection `json:"connections"`
	}
	err := a.do(ctx, http.MethodGet, "/api/v1/mcp/connections", nil, &out)
	return out.Connections, err
}

// Create implements AIQG.
func (a HTTPAIQG) Create(ctx context.Context, in NewConnection) (*Connection, error) {
	var out struct {
		Connection Connection `json:"connection"`
	}
	if err := a.do(ctx, http.MethodPost, "/api/v1/mcp/connections", in, &out); err != nil {
		return nil, err
	}
	if out.Connection.ID == "" {
		return nil, fmt.Errorf("AIQG returned no connection id")
	}
	return &out.Connection, nil
}

// TenantFromToken reads the tenant_id claim of an AIQG JWT without verifying
// it. The claim only plans the run: AIQG verifies the token on every call and
// writes to the tenant it carries, whatever this function returns.
func TenantFromToken(jwt string) (string, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("AIQG_BEARER is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("AIQG_BEARER payload is not base64url")
	}
	var claims struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("AIQG_BEARER payload is not JSON")
	}
	if claims.TenantID == "" {
		return "", fmt.Errorf("AIQG_BEARER has no tenant_id claim; sign in to AIQG (the aiqg-ui client) as an owner or admin of the target org")
	}
	return claims.TenantID, nil
}
