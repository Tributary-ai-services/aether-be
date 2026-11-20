package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Plan #22 AB-13: aether-be passes agent visibility through to agent-builder,
// which enforces it, and propagates agent-builder's refusals instead of
// flattening them to 502.

func testAgentService(t *testing.T, handler http.HandlerFunc) *AgentService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	log, err := logger.New(logger.Config{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	return &AgentService{agentBuilderURL: srv.URL, httpClient: srv.Client(), logger: log}
}

func captureBody(t *testing.T, into *map[string]interface{}, status int, resp string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			if err := json.Unmarshal(b, into); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}
}

func TestApplyVisibilityToBuilderReq(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name       string
		visibility models.AgentVisibility
		isPublic   *bool
		want       map[string]interface{}
	}{
		{"visibility wins and is sent alone", models.AgentVisibilityOrganization, &yes, map[string]interface{}{"visibility": "organization"}},
		{"legacy true", "", &yes, map[string]interface{}{"is_public": true}},
		{"legacy false", "", &no, map[string]interface{}{"is_public": false}},
		{"nothing sent", "", nil, map[string]interface{}{}},
	}
	for _, tc := range cases {
		got := map[string]interface{}{}
		applyVisibilityToBuilderReq(got, tc.visibility, tc.isPublic)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", tc.name, k, got[k], v)
			}
		}
	}
}

func TestCreateAgentInBuilder_SendsVisibility(t *testing.T) {
	var body map[string]interface{}
	s := testAgentService(t, captureBody(t, &body, http.StatusCreated, `{"agent":{"id":"a1"}}`))
	req := models.AgentCreateRequest{Name: "n", Type: models.AgentTypeQA, Visibility: models.AgentVisibilityOrganization, IsPublic: true}
	if _, err := s.createAgentInBuilder(context.Background(), req, "tok"); err != nil {
		t.Fatalf("createAgentInBuilder: %v", err)
	}
	if body["visibility"] != "organization" {
		t.Fatalf("visibility = %v, want organization", body["visibility"])
	}
	if _, sent := body["is_public"]; sent {
		t.Fatalf("is_public must not be sent alongside visibility, got %v", body["is_public"])
	}
}

func TestCreateAgentInBuilder_LegacyIsPublic(t *testing.T) {
	var body map[string]interface{}
	s := testAgentService(t, captureBody(t, &body, http.StatusCreated, `{"agent":{"id":"a1"}}`))
	req := models.AgentCreateRequest{Name: "n", Type: models.AgentTypeQA, IsPublic: false}
	if _, err := s.createAgentInBuilder(context.Background(), req, "tok"); err != nil {
		t.Fatalf("createAgentInBuilder: %v", err)
	}
	if v, sent := body["is_public"]; !sent || v != false {
		t.Fatalf("is_public = %v (sent %v), want false", v, sent)
	}
	if _, sent := body["visibility"]; sent {
		t.Fatalf("visibility must not be invented for a legacy request")
	}
}

func TestUpdateAgentInBuilder_SendsVisibility(t *testing.T) {
	var body map[string]interface{}
	s := testAgentService(t, captureBody(t, &body, http.StatusOK, `{}`))
	v := models.AgentVisibilityPrivate
	if err := s.updateAgentInBuilder(context.Background(), "a1", models.AgentUpdateRequest{Visibility: &v}, "tok"); err != nil {
		t.Fatalf("updateAgentInBuilder: %v", err)
	}
	if body["visibility"] != "private" {
		t.Fatalf("visibility = %v, want private", body["visibility"])
	}
}

func assertAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	apiErr, ok := errors.AsAPIError(err)
	if !ok {
		t.Fatalf("error %v is not an APIError", err)
	}
	if apiErr.StatusCode != status {
		t.Fatalf("status = %d, want %d (%v)", apiErr.StatusCode, status, apiErr)
	}
	if code != "" && apiErr.Code != code {
		t.Fatalf("code = %v, want %s", apiErr.Code, code)
	}
	if code != "" {
		// The UI reads a top-level `code` from the JSON body.
		b, _ := json.Marshal(apiErr)
		var wire map[string]interface{}
		_ = json.Unmarshal(b, &wire)
		if wire["code"] != code {
			t.Fatalf("wire JSON top-level code = %v, want %s (%s)", wire["code"], code, b)
		}
	}
}

func TestUpdateAgent_PropagatesPublicRequiresAdmin(t *testing.T) {
	var body map[string]interface{}
	s := testAgentService(t, captureBody(t, &body, http.StatusForbidden,
		`{"error":"making an agent public requires an admin role","code":"visibility_public_requires_admin"}`))
	v := models.AgentVisibilityPublic
	_, err := s.UpdateAgent(context.Background(), "a1", models.AgentUpdateRequest{Visibility: &v}, "u1", "tok")
	assertAPIError(t, err, http.StatusForbidden, "visibility_public_requires_admin")
}

func TestCreateAgent_PropagatesOrganizationRequiresOrgSpace(t *testing.T) {
	var body map[string]interface{}
	s := testAgentService(t, captureBody(t, &body, http.StatusBadRequest,
		`{"error":"this space belongs to no organization","code":"visibility_organization_requires_org_space"}`))
	spaceCtx := &models.SpaceContext{SpaceType: models.SpaceTypePersonal, SpaceID: "space_1", UserID: "u1", UserRole: "owner"}
	req := models.AgentCreateRequest{Name: "n", Type: models.AgentTypeQA, Visibility: models.AgentVisibilityOrganization}
	_, err := s.CreateAgent(context.Background(), req, spaceCtx, "tok")
	assertAPIError(t, err, http.StatusBadRequest, "visibility_organization_requires_org_space")
}

func TestBuilderError_VisibilityInvalid(t *testing.T) {
	err := builderError(http.StatusBadRequest, []byte(`{"error":"visibility must be private, organization or public","code":"visibility_invalid"}`))
	assertAPIError(t, err, http.StatusBadRequest, "visibility_invalid")
}

func TestBuilderError_NoCodeFallsBackToGenericCodes(t *testing.T) {
	assertAPIError(t, builderError(http.StatusForbidden, []byte(`{"error":"not yours"}`)), http.StatusForbidden, errors.ErrForbidden)
	assertAPIError(t, builderError(http.StatusBadRequest, []byte(`not json`)), http.StatusBadRequest, errors.ErrBadRequest)
}

func TestBuilderError_OtherStatusesStay502(t *testing.T) {
	err := builderError(http.StatusInternalServerError, []byte(`{"error":"boom"}`))
	assertAPIError(t, err, http.StatusBadGateway, "")
}

func TestVisibilityFromBuilder(t *testing.T) {
	v, org := visibilityFromBuilder(map[string]interface{}{"visibility": "organization", "organization_id": "o1", "is_public": false})
	if v != models.AgentVisibilityOrganization || org != "o1" {
		t.Fatalf("got %s/%s", v, org)
	}
	// An older agent-builder that sends only is_public.
	if v, _ := visibilityFromBuilder(map[string]interface{}{"is_public": true}); v != models.AgentVisibilityPublic {
		t.Fatalf("legacy public -> %s", v)
	}
	if v, _ := visibilityFromBuilder(map[string]interface{}{}); v != models.AgentVisibilityPrivate {
		t.Fatalf("absent -> %s", v)
	}
}
