package models

import "testing"

// Plan #22 AB-13: the Neo4j mirror's read check follows agent-builder's rule.

func TestCanBeAccessedBy_Visibility(t *testing.T) {
	org := &Agent{OwnerID: "owner", Visibility: AgentVisibilityOrganization, OrganizationID: "orgA"}
	if !org.CanBeAccessedBy("other", nil, "orgA") {
		t.Error("organization agent should be readable from a space of its own organization")
	}
	if org.CanBeAccessedBy("other", nil, "orgB") {
		t.Error("organization agent must not be readable from another organization")
	}
	if org.CanBeAccessedBy("other", nil, "") {
		t.Error("organization agent must not be readable from a personal space")
	}

	// An organization agent that has no organization recorded is readable by
	// no one but its owner, even from a caller whose org is also empty.
	orphan := &Agent{OwnerID: "owner", Visibility: AgentVisibilityOrganization}
	if orphan.CanBeAccessedBy("other", nil, "") {
		t.Error("empty organization ids must never match each other")
	}
	if !orphan.CanBeAccessedBy("owner", nil, "") {
		t.Error("the owner always reads their own agent")
	}

	public := &Agent{OwnerID: "owner", Visibility: AgentVisibilityPublic}
	if !public.CanBeAccessedBy("other", nil, "") {
		t.Error("public agent should be readable by anyone")
	}
	private := &Agent{OwnerID: "owner", Visibility: AgentVisibilityPrivate, OrganizationID: "orgA"}
	if private.CanBeAccessedBy("other", nil, "orgA") {
		t.Error("private agent must not be readable by its organization")
	}
	// A node written before AB-13 has only is_public.
	legacy := &Agent{OwnerID: "owner", IsPublic: true}
	if !legacy.CanBeAccessedBy("other", nil, "") {
		t.Error("legacy is_public=true node should still read as public")
	}
}

func TestNewAgent_TakesVisibilityAndOrganization(t *testing.T) {
	sc := &SpaceContext{SpaceType: SpaceTypeOrganization, SpaceID: "s1", TenantID: "t1", OrganizationID: "orgA"}
	a := NewAgent(AgentCreateRequest{Name: "n", Visibility: AgentVisibilityOrganization}, "u1", sc)
	if a.Visibility != AgentVisibilityOrganization || a.OrganizationID != "orgA" || a.IsPublic {
		t.Fatalf("got visibility=%s org=%s is_public=%v", a.Visibility, a.OrganizationID, a.IsPublic)
	}
	legacy := NewAgent(AgentCreateRequest{Name: "n", IsPublic: true}, "u1", sc)
	if legacy.Visibility != AgentVisibilityPublic || !legacy.IsPublic {
		t.Fatalf("legacy is_public=true -> visibility=%s is_public=%v", legacy.Visibility, legacy.IsPublic)
	}
}

func TestAgentUpdate_VisibilityKeepsIsPublicInStep(t *testing.T) {
	a := &Agent{IsPublic: true, Visibility: AgentVisibilityPublic}
	v := AgentVisibilityOrganization
	a.Update(AgentUpdateRequest{Visibility: &v})
	if a.Visibility != AgentVisibilityOrganization || a.IsPublic {
		t.Fatalf("got visibility=%s is_public=%v", a.Visibility, a.IsPublic)
	}
	f := false
	a.Update(AgentUpdateRequest{IsPublic: &f})
	if a.Visibility != AgentVisibilityPrivate {
		t.Fatalf("legacy is_public=false update -> %s", a.Visibility)
	}
	a.Update(AgentUpdateRequest{})
	if a.Visibility != AgentVisibilityPrivate {
		t.Fatalf("an update with no sharing fields must not change visibility")
	}
}
