package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// TeamHandler handles team-related HTTP requests
type TeamHandler struct {
	teamService *services.TeamService
	userService *services.UserService
	logger      *logger.Logger
}

// NewTeamHandler creates a new team handler
func NewTeamHandler(teamService *services.TeamService, userService *services.UserService, log *logger.Logger) *TeamHandler {
	return &TeamHandler{
		teamService: teamService,
		userService: userService,
		logger:      log.WithService("team_handler"),
	}
}

// CreateTeam creates a new team
// @Summary Create a new team
// @Description Create a new team with the provided information
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param team body models.TeamCreateRequest true "Team creation data"
// @Success 201 {object} models.TeamResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams [post]
func (h *TeamHandler) CreateTeam(c *gin.Context) {
	// Ensure user exists in Neo4j database
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to ensure user exists", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.TeamCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid team creation request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	team, err := h.teamService.CreateTeam(c.Request.Context(), req, userID)
	if err != nil {
		h.logger.Error("Failed to create team", zap.Error(err), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team created successfully", zap.String("team_id", team.ID), zap.String("user_id", userID))
	c.JSON(http.StatusCreated, team.ToResponse())
}

// GetTeams gets teams for the current user
// @Summary Get user teams
// @Description Get all teams the current user is a member of or can access
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param organization_id query string false "Filter by organization ID"
// @Success 200 {array} models.TeamResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams [get]
func (h *TeamHandler) GetTeams(c *gin.Context) {
	// Ensure user exists in Neo4j database
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to ensure user exists", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	organizationID := c.Query("organization_id")

	teams, err := h.teamService.GetTeams(c.Request.Context(), userID, organizationID)
	if err != nil {
		h.logger.Error("Failed to get teams", zap.Error(err), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	// Convert to responses
	responses := make([]*models.TeamResponse, len(teams))
	for i, team := range teams {
		responses[i] = team.ToResponse()
	}

	c.JSON(http.StatusOK, responses)
}

// GetTeam gets a specific team by ID
// @Summary Get team by ID
// @Description Get a specific team by its ID
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Success 200 {object} models.TeamResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id} [get]
func (h *TeamHandler) GetTeam(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	team, err := h.teamService.GetTeam(c.Request.Context(), teamID, userID)
	if err != nil {
		h.logger.Error("Failed to get team", zap.Error(err), zap.String("team_id", teamID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	resp := team.ToResponse()
	if team.UserRole == "" {
		// Not a member: say whether a join request is pending (Plan #22 A.3).
		if st, err := h.teamService.MyJoinRequestStatus(c.Request.Context(), team.ID, userID); err == nil {
			resp.JoinRequest = st
		}
	}
	c.JSON(http.StatusOK, resp)
}

// UpdateTeam updates a team
// @Summary Update team
// @Description Update team information
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Param team body models.TeamUpdateRequest true "Team update data"
// @Success 200 {object} models.TeamResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id} [put]
func (h *TeamHandler) UpdateTeam(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	var req models.TeamUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid team update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	team, err := h.teamService.UpdateTeam(c.Request.Context(), teamID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update team", zap.Error(err), zap.String("team_id", teamID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team updated successfully", zap.String("team_id", teamID), zap.String("user_id", userID))
	c.JSON(http.StatusOK, team.ToResponse())
}

// DeleteTeam deletes a team
// @Summary Delete team
// @Description Delete a team (only team owners can delete teams)
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id} [delete]
func (h *TeamHandler) DeleteTeam(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	err = h.teamService.DeleteTeam(c.Request.Context(), teamID, userID)
	if err != nil {
		h.logger.Error("Failed to delete team", zap.Error(err), zap.String("team_id", teamID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team deleted successfully", zap.String("team_id", teamID), zap.String("user_id", userID))
	c.Status(http.StatusNoContent)
}

// GetTeamMembers gets all members of a team
// @Summary Get team members
// @Description Get all members of a specific team
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Success 200 {array} models.TeamMemberResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id}/members [get]
func (h *TeamHandler) GetTeamMembers(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	members, err := h.teamService.GetTeamMembers(c.Request.Context(), teamID, userID)
	if err != nil {
		h.logger.Error("Failed to get team members", zap.Error(err), zap.String("team_id", teamID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	// Convert to responses
	responses := make([]*models.TeamMemberResponse, len(members))
	for i, member := range members {
		responses[i] = member.ToMemberResponse()
	}

	c.JSON(http.StatusOK, responses)
}

// InviteTeamMember invites a new member to the team
// @Summary Invite team member
// @Description Invite a new member to the team by email
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Param invite body models.TeamInviteRequest true "Member invitation data"
// @Success 201 {object} models.TeamMemberResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 409 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id}/members [post]
func (h *TeamHandler) InviteTeamMember(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	var req models.TeamInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid member invitation request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	member, err := h.teamService.InviteTeamMember(c.Request.Context(), teamID, req, userID)
	if err != nil {
		h.logger.Error("Failed to invite team member", zap.Error(err),
			zap.String("team_id", teamID), zap.String("user_id", userID), zap.String("email", req.Email))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team member invited successfully",
		zap.String("team_id", teamID), zap.String("invited_user_id", member.UserID), zap.String("invited_by", userID))
	c.JSON(http.StatusCreated, member.ToMemberResponse())
}

// UpdateTeamMemberRole updates a team member's role
// @Summary Update member role
// @Description Update a team member's role
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Param user_id path string true "User ID"
// @Param role body models.TeamMemberRoleUpdateRequest true "Role update data"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id}/members/{user_id} [put]
func (h *TeamHandler) UpdateTeamMemberRole(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	targetUserID := c.Param("user_id")
	if teamID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID and User ID are required", map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		}))
		return
	}

	var req models.TeamMemberRoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid role update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	err = h.teamService.UpdateTeamMemberRole(c.Request.Context(), teamID, targetUserID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update team member role", zap.Error(err),
			zap.String("team_id", teamID), zap.String("target_user_id", targetUserID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team member role updated successfully",
		zap.String("team_id", teamID), zap.String("target_user_id", targetUserID), zap.String("updated_by", userID))
	c.Status(http.StatusNoContent)
}

// RemoveTeamMember removes a member from the team
// @Summary Remove team member
// @Description Remove a member from the team
// @Tags teams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Team ID"
// @Param user_id path string true "User ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/teams/{id}/members/{user_id} [delete]
func (h *TeamHandler) RemoveTeamMember(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	teamID := c.Param("id")
	targetUserID := c.Param("user_id")
	if teamID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Team ID and User ID are required", map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		}))
		return
	}

	err = h.teamService.RemoveTeamMember(c.Request.Context(), teamID, targetUserID, userID)
	if err != nil {
		h.logger.Error("Failed to remove team member", zap.Error(err),
			zap.String("team_id", teamID), zap.String("target_user_id", targetUserID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Team member removed successfully",
		zap.String("team_id", teamID), zap.String("target_user_id", targetUserID), zap.String("removed_by", userID))
	c.Status(http.StatusNoContent)
}

// MoveTeam moves a team under another team of the same organization, or to
// the top level when parent_team_id is "" (Plan #22 A.2).
// @Router /api/v1/teams/{id}/move [post]
func (h *TeamHandler) MoveTeam(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	teamID := c.Param("id")
	var req models.TeamMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{"error": err.Error()}))
		return
	}
	if err := h.teamService.MoveTeam(c.Request.Context(), teamID, req.ParentTeamID, userID); err != nil {
		handleServiceError(c, err)
		return
	}
	team, err := h.teamService.GetTeam(c.Request.Context(), teamID, userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, team.ToResponse())
}

// JoinTeam joins an organization- or public-visible team, or files a request
// for a private one (Plan #22 A.3). 200 {status: joined} or 202 {status: requested}.
// @Router /api/v1/teams/{id}/join [post]
func (h *TeamHandler) JoinTeam(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	status, err := h.teamService.JoinTeam(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	code := http.StatusOK
	if status == services.JoinStatusRequested {
		code = http.StatusAccepted
	}
	c.JSON(code, gin.H{"status": status})
}

// ListJoinRequests lists a team's pending join requests (managers only).
// @Router /api/v1/teams/{id}/join-requests [get]
func (h *TeamHandler) ListJoinRequests(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	reqs, err := h.teamService.ListJoinRequests(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"requests": reqs})
}

// DecideJoinRequest approves or denies a pending request (managers only).
// @Router /api/v1/teams/{id}/join-requests/{request_id} [post]
func (h *TeamHandler) DecideJoinRequest(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	var req models.TeamJoinDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{"error": err.Error()}))
		return
	}
	if err := h.teamService.DecideJoinRequest(c.Request.Context(), c.Param("id"), c.Param("request_id"), req.Approve, req.JoinedAt, userID); err != nil {
		handleServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SetMemberJoinedAt back-dates or corrects when a member joined (managers only).
// @Router /api/v1/teams/{id}/members/{user_id}/joined-at [put]
func (h *TeamHandler) SetMemberJoinedAt(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	var req models.TeamMemberJoinedAtRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.JoinedAt.IsZero() {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("joined_at (RFC 3339) is required", nil))
		return
	}
	if err := h.teamService.SetMemberJoinedAt(c.Request.Context(), c.Param("id"), c.Param("user_id"), req.JoinedAt, userID); err != nil {
		handleServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Team-owned repos and source apps (AE-36). Each write needs a team
// owner/admin or an org owner/admin, refuses an archived team (409), and
// returns the team. Repos are "owner/name", stored lowercase.

func (h *TeamHandler) changeOwned(c *gin.Context, ch services.OwnedChange) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	team, err := h.teamService.ChangeTeamOwned(c.Request.Context(), c.Param("id"), ch, userID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, team.ToResponse())
}

func badOwnedBody(c *gin.Context, err error, field string) {
	details := map[string]interface{}{"field": field}
	if err != nil {
		details["error"] = err.Error()
	}
	c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", details))
}

// SetTeamRepos replaces a team's repos. Body {"repos": ["owner/name", ...]}; [] clears.
// @Router /api/v1/teams/{id}/repos [put]
func (h *TeamHandler) SetTeamRepos(c *gin.Context) {
	var req models.TeamReposRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Repos == nil {
		badOwnedBody(c, err, "repos")
		return
	}
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindRepo, Set: req.Repos})
}

// AddTeamRepo adds one repo. Body {"repo": "owner/name"}; adding an owned repo is a no-op.
// @Router /api/v1/teams/{id}/repos [post]
func (h *TeamHandler) AddTeamRepo(c *gin.Context) {
	var req models.TeamRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badOwnedBody(c, err, "repo")
		return
	}
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindRepo, Add: []string{req.Repo}})
}

// RemoveTeamRepo removes one repo; 404 when the team does not own it.
// @Router /api/v1/teams/{id}/repos/{owner}/{name} [delete]
func (h *TeamHandler) RemoveTeamRepo(c *gin.Context) {
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindRepo,
		Remove: []string{c.Param("owner") + "/" + c.Param("name")}})
}

// SetTeamSourceApps replaces a team's source apps. Body {"source_apps": [...]}; [] clears.
// @Router /api/v1/teams/{id}/source-apps [put]
func (h *TeamHandler) SetTeamSourceApps(c *gin.Context) {
	var req models.TeamSourceAppsRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SourceApps == nil {
		badOwnedBody(c, err, "source_apps")
		return
	}
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindSourceApp, Set: req.SourceApps})
}

// AddTeamSourceApp adds one source app. Body {"source_app": "..."}.
// @Router /api/v1/teams/{id}/source-apps [post]
func (h *TeamHandler) AddTeamSourceApp(c *gin.Context) {
	var req models.TeamSourceAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badOwnedBody(c, err, "source_app")
		return
	}
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindSourceApp, Add: []string{req.SourceApp}})
}

// RemoveTeamSourceApp removes one source app; 404 when the team does not own it.
// @Router /api/v1/teams/{id}/source-apps/{source_app} [delete]
func (h *TeamHandler) RemoveTeamSourceApp(c *gin.Context) {
	h.changeOwned(c, services.OwnedChange{Kind: services.OwnedKindSourceApp, Remove: []string{c.Param("source_app")}})
}
