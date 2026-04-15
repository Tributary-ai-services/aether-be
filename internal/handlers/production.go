package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// extractBearerToken extracts the Bearer token from the Authorization header
func extractBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	// Expected format: "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}

// ProductionHandler handles production-related HTTP requests
type ProductionHandler struct {
	productionService   *services.ProductionService
	progressService     *services.PodcastProgressService
	conversationService *services.ConversationService
	userService         *services.UserService
	teamService         *services.TeamService
	logger              *logger.Logger
}

// NewProductionHandler creates a new production handler
func NewProductionHandler(
	productionService *services.ProductionService,
	progressService *services.PodcastProgressService,
	conversationService *services.ConversationService,
	userService *services.UserService,
	teamService *services.TeamService,
	log *logger.Logger,
) *ProductionHandler {
	return &ProductionHandler{
		productionService:   productionService,
		progressService:     progressService,
		conversationService: conversationService,
		userService:         userService,
		teamService:         teamService,
		logger:              log.WithService("production_handler"),
	}
}

// GetNotebookProducers returns available producer agents for a notebook
// @Summary Get notebook producers
// @Description Get available producer agents for a notebook
// @Tags productions
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Success 200 {object} []models.AgentResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/producers [get]
func (h *ProductionHandler) GetNotebookProducers(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Get auth token for agent-builder API calls
	authToken := extractBearerToken(c)

	producers, err := h.productionService.GetNotebookProducers(c.Request.Context(), notebookID, userID, authToken, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get notebook producers", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, producers)
}

// ExecuteProducer executes a producer agent on a notebook
// @Summary Execute producer
// @Description Execute a producer agent on a notebook to generate content
// @Tags productions
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param agent_id path string true "Agent ID"
// @Param request body models.ProducerExecuteRequest true "Execute request"
// @Success 201 {object} models.ProducerExecuteResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/producers/{agent_id}/execute [post]
func (h *ProductionHandler) ExecuteProducer(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	agentID := c.Param("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get user teams for access control
	userTeams, err := h.getUserTeams(c, userID)
	if err != nil {
		h.logger.Warn("Failed to get user teams, using empty list", zap.Error(err))
		userTeams = []string{}
	}

	// Get auth token
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token is required"))
		return
	}

	// Parse request body
	var req models.ProducerExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Set agent ID from path parameter
	req.AgentID = agentID

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Execute producer
	production, err := h.productionService.ExecuteProducer(c.Request.Context(), notebookID, req, userID, userTeams, authToken, spaceContext)
	if err != nil {
		h.logger.Error("Failed to execute producer", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Producer executed successfully",
		zap.String("production_id", production.ID),
		zap.String("notebook_id", notebookID),
		zap.String("agent_id", agentID),
	)

	c.JSON(http.StatusCreated, models.ProducerExecuteResponse{
		Production: production,
		Content:    production.Content,
	})
}

// ListNotebookProductions lists productions for a notebook
// @Summary List notebook productions
// @Description List all productions for a notebook
// @Tags productions
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param limit query integer false "Limit (default 20, max 100)"
// @Param offset query integer false "Offset (default 0)"
// @Success 200 {object} models.ProductionListResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/productions [get]
func (h *ProductionHandler) ListNotebookProductions(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Parse pagination parameters
	limit := 20
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
			offset = val
		}
	}

	productions, err := h.productionService.ListNotebookProductions(c.Request.Context(), notebookID, userID, spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list notebook productions", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, productions)
}

// GetProduction retrieves a production by ID
// @Summary Get production
// @Description Retrieve a production by ID
// @Tags productions
// @Produce json
// @Security Bearer
// @Param id path string true "Production ID"
// @Success 200 {object} models.ProductionResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/productions/{id} [get]
func (h *ProductionHandler) GetProduction(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	production, err := h.productionService.GetProductionByID(c.Request.Context(), productionID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get production", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, production.ToResponse())
}

// GetProductionContent retrieves the content of a production
// @Summary Get production content
// @Description Retrieve the generated content of a production
// @Tags productions
// @Produce json
// @Security Bearer
// @Param id path string true "Production ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/productions/{id}/content [get]
func (h *ProductionHandler) GetProductionContent(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	content, err := h.productionService.GetProductionContent(c.Request.Context(), productionID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get production content", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Also fetch the production metadata so the frontend gets fresh data
	// (e.g., mediaUrl may have been set after async rendering completed)
	production, prodErr := h.productionService.GetProductionByID(c.Request.Context(), productionID, userID, spaceContext)
	if prodErr != nil {
		// Non-fatal: still return content even if metadata fetch fails
		h.logger.Warn("Failed to fetch production metadata alongside content", zap.Error(prodErr))
		c.JSON(http.StatusOK, gin.H{
			"content": content,
		})
		return
	}

	response := production.ToResponse()
	c.JSON(http.StatusOK, gin.H{
		"content":    content,
		"production": response,
	})
}

// DeleteProduction deletes a production
// @Summary Delete production
// @Description Delete a production and its stored content
// @Tags productions
// @Security Bearer
// @Param id path string true "Production ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/productions/{id} [delete]
func (h *ProductionHandler) DeleteProduction(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	err = h.productionService.DeleteProduction(c.Request.Context(), productionID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete production", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Production deleted successfully", zap.String("production_id", productionID))
	c.Status(http.StatusNoContent)
}

// GetProducerPreferences retrieves producer preferences for the current user
// @Summary Get producer preferences
// @Description Get producer preferences including pinned agents and settings
// @Tags productions
// @Produce json
// @Security Bearer
// @Success 200 {object} models.ProducerPreferences
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/preferences/producers [get]
func (h *ProductionHandler) GetProducerPreferences(c *gin.Context) {
	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get user to access preferences
	user, err := h.userService.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Extract producer preferences from user preferences
	preferences := &models.ProducerPreferences{}
	if user.Preferences != nil {
		if producerPrefs, ok := user.Preferences["producers"]; ok {
			if prefs, ok := producerPrefs.(map[string]interface{}); ok {
				// Parse pinned
				if pinned, ok := prefs["pinned"].(map[string]interface{}); ok {
					preferences.Pinned = &models.PinnedProducers{}
					if global, ok := pinned["global"].([]interface{}); ok {
						for _, id := range global {
							if idStr, ok := id.(string); ok {
								preferences.Pinned.Global = append(preferences.Pinned.Global, idStr)
							}
						}
					}
					if bySpace, ok := pinned["by_space"].(map[string]interface{}); ok {
						preferences.Pinned.BySpace = make(map[string][]string)
						for spaceID, agents := range bySpace {
							if agentList, ok := agents.([]interface{}); ok {
								for _, agent := range agentList {
									if agentStr, ok := agent.(string); ok {
										preferences.Pinned.BySpace[spaceID] = append(preferences.Pinned.BySpace[spaceID], agentStr)
									}
								}
							}
						}
					}
					if byNotebook, ok := pinned["by_notebook"].(map[string]interface{}); ok {
						preferences.Pinned.ByNotebook = make(map[string][]string)
						for notebookID, agents := range byNotebook {
							if agentList, ok := agents.([]interface{}); ok {
								for _, agent := range agentList {
									if agentStr, ok := agent.(string); ok {
										preferences.Pinned.ByNotebook[notebookID] = append(preferences.Pinned.ByNotebook[notebookID], agentStr)
									}
								}
							}
						}
					}
				}
				// Parse order (custom ordering for drag-and-drop)
				if order, ok := prefs["order"].(map[string]interface{}); ok {
					preferences.Order = &models.OrderedProducers{}
					if global, ok := order["global"].([]interface{}); ok {
						for _, id := range global {
							if idStr, ok := id.(string); ok {
								preferences.Order.Global = append(preferences.Order.Global, idStr)
							}
						}
					}
					if bySpace, ok := order["by_space"].(map[string]interface{}); ok {
						preferences.Order.BySpace = make(map[string][]string)
						for spaceID, agents := range bySpace {
							if agentList, ok := agents.([]interface{}); ok {
								for _, agent := range agentList {
									if agentStr, ok := agent.(string); ok {
										preferences.Order.BySpace[spaceID] = append(preferences.Order.BySpace[spaceID], agentStr)
									}
								}
							}
						}
					}
					if byNotebook, ok := order["by_notebook"].(map[string]interface{}); ok {
						preferences.Order.ByNotebook = make(map[string][]string)
						for notebookID, agents := range byNotebook {
							if agentList, ok := agents.([]interface{}); ok {
								for _, agent := range agentList {
									if agentStr, ok := agent.(string); ok {
										preferences.Order.ByNotebook[notebookID] = append(preferences.Order.ByNotebook[notebookID], agentStr)
									}
								}
							}
						}
					}
				}
				// Parse recent
				if recent, ok := prefs["recent"].([]interface{}); ok {
					for _, id := range recent {
						if idStr, ok := id.(string); ok {
							preferences.Recent = append(preferences.Recent, idStr)
						}
					}
				}
				// Parse settings
				if settings, ok := prefs["settings"].(map[string]interface{}); ok {
					preferences.Settings = &models.ProducerSettingsPrefs{}
					if defaultFormat, ok := settings["default_format"].(string); ok {
						preferences.Settings.DefaultFormat = models.ProductionFormat(defaultFormat)
					}
					if defaultType, ok := settings["default_type"].(string); ok {
						preferences.Settings.DefaultType = models.ProductionType(defaultType)
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, preferences)
}

// UpdateProducerPreferences updates producer preferences for the current user
// @Summary Update producer preferences
// @Description Update producer preferences including pin/unpin agents and settings
// @Tags productions
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body models.UpdateProducerPreferencesRequest true "Update request"
// @Success 200 {object} models.ProducerPreferences
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/preferences/producers [patch]
func (h *ProductionHandler) UpdateProducerPreferences(c *gin.Context) {
	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Parse request body
	var req models.UpdateProducerPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Get current user to update preferences
	user, err := h.userService.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Initialize preferences if nil
	if user.Preferences == nil {
		user.Preferences = make(map[string]interface{})
	}

	// Get or create producer preferences
	var producerPrefs map[string]interface{}
	if prefs, ok := user.Preferences["producers"].(map[string]interface{}); ok {
		producerPrefs = prefs
	} else {
		producerPrefs = make(map[string]interface{})
	}

	// Get or create pinned preferences
	var pinnedPrefs map[string]interface{}
	if pinned, ok := producerPrefs["pinned"].(map[string]interface{}); ok {
		pinnedPrefs = pinned
	} else {
		pinnedPrefs = map[string]interface{}{
			"global":      []string{},
			"by_space":    map[string]interface{}{},
			"by_notebook": map[string]interface{}{},
		}
	}

	// Handle pin/unpin global
	if req.PinGlobal != nil {
		globalPins := getStringSlice(pinnedPrefs["global"])
		if !contains(globalPins, *req.PinGlobal) {
			globalPins = append(globalPins, *req.PinGlobal)
		}
		pinnedPrefs["global"] = globalPins
	}
	if req.UnpinGlobal != nil {
		globalPins := getStringSlice(pinnedPrefs["global"])
		pinnedPrefs["global"] = removeFromSlice(globalPins, *req.UnpinGlobal)
	}

	// Handle pin/unpin to space
	if req.PinToSpace != nil && req.SpaceID != "" {
		bySpace := getMapStringSlice(pinnedPrefs["by_space"])
		spacePins := bySpace[req.SpaceID]
		if !contains(spacePins, *req.PinToSpace) {
			spacePins = append(spacePins, *req.PinToSpace)
		}
		bySpace[req.SpaceID] = spacePins
		pinnedPrefs["by_space"] = bySpace
	}
	if req.UnpinFromSpace != nil && req.SpaceID != "" {
		bySpace := getMapStringSlice(pinnedPrefs["by_space"])
		bySpace[req.SpaceID] = removeFromSlice(bySpace[req.SpaceID], *req.UnpinFromSpace)
		pinnedPrefs["by_space"] = bySpace
	}

	// Handle pin/unpin to notebook
	if req.PinToNotebook != nil && req.NotebookID != "" {
		byNotebook := getMapStringSlice(pinnedPrefs["by_notebook"])
		notebookPins := byNotebook[req.NotebookID]
		if !contains(notebookPins, *req.PinToNotebook) {
			notebookPins = append(notebookPins, *req.PinToNotebook)
		}
		byNotebook[req.NotebookID] = notebookPins
		pinnedPrefs["by_notebook"] = byNotebook
	}
	if req.UnpinFromNotebook != nil && req.NotebookID != "" {
		byNotebook := getMapStringSlice(pinnedPrefs["by_notebook"])
		byNotebook[req.NotebookID] = removeFromSlice(byNotebook[req.NotebookID], *req.UnpinFromNotebook)
		pinnedPrefs["by_notebook"] = byNotebook
	}

	// Get or create order preferences
	var orderPrefs map[string]interface{}
	if order, ok := producerPrefs["order"].(map[string]interface{}); ok {
		orderPrefs = order
	} else {
		orderPrefs = map[string]interface{}{
			"global":      []string{},
			"by_space":    map[string]interface{}{},
			"by_notebook": map[string]interface{}{},
		}
	}

	// Handle set order global
	if len(req.SetOrderGlobal) > 0 {
		orderPrefs["global"] = req.SetOrderGlobal
	}

	// Handle set order for space
	if len(req.SetOrderForSpace) > 0 && req.SpaceID != "" {
		bySpace := getMapStringSlice(orderPrefs["by_space"])
		bySpace[req.SpaceID] = req.SetOrderForSpace
		orderPrefs["by_space"] = bySpace
	}

	// Handle set order for notebook
	if len(req.SetOrderForNotebook) > 0 && req.NotebookID != "" {
		byNotebook := getMapStringSlice(orderPrefs["by_notebook"])
		byNotebook[req.NotebookID] = req.SetOrderForNotebook
		orderPrefs["by_notebook"] = byNotebook
	}

	// Handle settings updates
	var settingsPrefs map[string]interface{}
	if settings, ok := producerPrefs["settings"].(map[string]interface{}); ok {
		settingsPrefs = settings
	} else {
		settingsPrefs = make(map[string]interface{})
	}
	if req.DefaultFormat != nil {
		settingsPrefs["default_format"] = string(*req.DefaultFormat)
	}
	if req.DefaultType != nil {
		settingsPrefs["default_type"] = string(*req.DefaultType)
	}

	// Update producer preferences
	producerPrefs["pinned"] = pinnedPrefs
	producerPrefs["order"] = orderPrefs
	producerPrefs["settings"] = settingsPrefs
	user.Preferences["producers"] = producerPrefs

	// Save user preferences
	updateReq := models.UserUpdateRequest{
		Preferences: user.Preferences,
	}
	_, err = h.userService.UpdateUser(c.Request.Context(), userID, updateReq)
	if err != nil {
		h.logger.Error("Failed to update user preferences", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Producer preferences updated successfully", zap.String("user_id", userID))

	// Return updated preferences
	h.GetProducerPreferences(c)
}

// Helper functions

func getStringSlice(v interface{}) []string {
	if v == nil {
		return []string{}
	}
	switch val := v.(type) {
	case []string:
		return val
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return []string{}
	}
}

func getMapStringSlice(v interface{}) map[string][]string {
	result := make(map[string][]string)
	if v == nil {
		return result
	}
	switch val := v.(type) {
	case map[string][]string:
		return val
	case map[string]interface{}:
		for k, v := range val {
			result[k] = getStringSlice(v)
		}
		return result
	default:
		return result
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// BulkDeleteProductions deletes multiple productions
// @Summary Bulk delete productions
// @Description Delete multiple productions and their stored content
// @Tags productions
// @Accept json
// @Security Bearer
// @Param request body models.BulkDeleteProductionsRequest true "Bulk delete request"
// @Success 200 {object} models.BulkDeleteProductionsResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/productions/bulk-delete [post]
func (h *ProductionHandler) BulkDeleteProductions(c *gin.Context) {
	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Parse request body
	var req models.BulkDeleteProductionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if len(req.ProductionIDs) == 0 {
		c.JSON(http.StatusBadRequest, errors.BadRequest("At least one production ID is required"))
		return
	}

	// Limit bulk delete to 100 items at a time
	if len(req.ProductionIDs) > 100 {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Cannot delete more than 100 productions at a time"))
		return
	}

	// Delete each production and track results
	deleted := []string{}
	failed := []models.BulkDeleteFailure{}

	for _, productionID := range req.ProductionIDs {
		err := h.productionService.DeleteProduction(c.Request.Context(), productionID, userID, spaceContext)
		if err != nil {
			h.logger.Warn("Failed to delete production in bulk operation",
				zap.String("production_id", productionID),
				zap.Error(err))
			failed = append(failed, models.BulkDeleteFailure{
				ProductionID: productionID,
				Error:        err.Error(),
			})
		} else {
			deleted = append(deleted, productionID)
		}
	}

	h.logger.Info("Bulk delete productions completed",
		zap.Int("deleted_count", len(deleted)),
		zap.Int("failed_count", len(failed)),
		zap.String("user_id", userID),
	)

	c.JSON(http.StatusOK, models.BulkDeleteProductionsResponse{
		Deleted: deleted,
		Failed:  failed,
	})
}

// ListRenderers returns available renderer workflows
// @Summary List renderers
// @Description Get available renderer workflows for post-processing productions
// @Tags productions
// @Produce json
// @Security Bearer
// @Success 200 {array} map[string]interface{}
// @Failure 500 {object} errors.APIError
// @Router /api/v1/renderers [get]
func (h *ProductionHandler) ListRenderers(c *gin.Context) {
	renderers, err := h.productionService.ListRenderers(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to list renderers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to list renderers"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"renderers": renderers,
	})
}

// getUserTeams retrieves team IDs for a user for access control purposes
func (h *ProductionHandler) getUserTeams(c *gin.Context, userID string) ([]string, error) {
	teamIDs, err := h.teamService.GetUserTeamIDs(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user team IDs",
			zap.String("user_id", userID),
			zap.Error(err))
		// Return empty slice rather than failing the request - user may not be in any teams
		return []string{}, nil
	}

	h.logger.Debug("Retrieved user teams for production access control",
		zap.String("user_id", userID),
		zap.Int("team_count", len(teamIDs)))

	return teamIDs, nil
}

// NotebookChatRequest represents a chat request for a notebook
type NotebookChatRequest struct {
	Message        string                `json:"message" binding:"required"`
	History        []NotebookChatMessage `json:"history,omitempty"`
	ConversationID string                `json:"conversation_id,omitempty"`
	SkillIDs       []string              `json:"skill_ids,omitempty"`
}

// NotebookChatMessage represents a message in the chat history
type NotebookChatMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// NotebookChatResponse represents the response from notebook chat
type NotebookChatResponse struct {
	Message        string `json:"message"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	ConversationID string `json:"conversation_id"`
}

// NotebookChat handles chat requests for a notebook using the internal Notebook Chat Assistant
// @Summary Chat with notebook
// @Description Send a message to chat about notebook content using the AI assistant
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param request body NotebookChatRequest true "Chat request"
// @Success 200 {object} NotebookChatResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/chat [post]
func (h *ProductionHandler) NotebookChat(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get auth token
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token is required"))
		return
	}

	// Parse request body
	var req NotebookChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Handle conversation persistence
	conversationID := req.ConversationID

	// If no conversation_id provided, create a new conversation
	if conversationID == "" && h.conversationService != nil {
		createReq := models.CreateConversationRequest{}
		conv, convErr := h.conversationService.CreateConversation(c.Request.Context(), notebookID, createReq, userID, spaceContext)
		if convErr != nil {
			h.logger.Warn("Failed to create conversation, proceeding without persistence", zap.Error(convErr))
		} else {
			conversationID = conv.ID
		}
	}

	// Build conversation history from persisted messages or client-supplied history
	var conversationHistory []models.ConversationMessage
	if conversationID != "" && h.conversationService != nil {
		// Load history from persisted messages
		persistedMessages, msgErr := h.conversationService.GetAllMessages(c.Request.Context(), conversationID)
		if msgErr != nil {
			h.logger.Warn("Failed to load persisted messages, falling back to client history", zap.Error(msgErr))
		} else if len(persistedMessages) > 0 {
			for _, msg := range persistedMessages {
				conversationHistory = append(conversationHistory, models.ConversationMessage{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		}
	}

	// Fall back to client-supplied history if no persisted messages
	if len(conversationHistory) == 0 {
		for _, msg := range req.History {
			conversationHistory = append(conversationHistory, models.ConversationMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Execute chat using the production service
	response, err := h.productionService.ExecuteNotebookChat(
		c.Request.Context(),
		notebookID,
		req.Message,
		conversationHistory,
		userID,
		authToken,
		spaceContext,
		req.SkillIDs,
	)
	if err != nil {
		h.logger.Error("Failed to execute notebook chat", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Persist messages
	if conversationID != "" && h.conversationService != nil {
		// Persist user message
		_, _ = h.conversationService.AddMessage(c.Request.Context(), conversationID, "user", req.Message, false, nil)
		// Persist assistant response
		_, _ = h.conversationService.AddMessage(c.Request.Context(), conversationID, "assistant", response.Content, false, nil)
		// Auto-name conversation from first message
		_ = h.conversationService.UpdateConversationNameFromMessage(c.Request.Context(), conversationID, req.Message)
	}

	h.logger.Info("Notebook chat completed",
		zap.String("notebook_id", notebookID),
		zap.String("user_id", userID),
		zap.String("conversation_id", conversationID),
	)

	c.JSON(http.StatusOK, NotebookChatResponse{
		Message:        response.Content,
		AgentID:        NotebookChatAssistantID,
		AgentName:      "Notebook Chat Assistant",
		ConversationID: conversationID,
	})
}

// StreamProductionProgress handles SSE for real-time podcast production progress
func (h *ProductionHandler) StreamProductionProgress(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	if h.progressService == nil {
		c.JSON(http.StatusServiceUnavailable, errors.Internal("Progress service not available"))
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Subscribe to progress events
	eventCh := h.progressService.Subscribe(productionID)
	defer h.progressService.Unsubscribe(productionID, eventCh)

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	clientGone := c.Request.Context().Done()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-clientGone:
			return false
		case event := <-eventCh:
			data, err := json.Marshal(event)
			if err != nil {
				h.logger.Error("Failed to marshal progress event", zap.Error(err))
				return true
			}
			c.SSEvent("message", string(data))
			return true
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			c.Writer.Flush()
			return true
		}
	})
}

// GetProductionProgress returns the current progress of a podcast production (polling fallback)
func (h *ProductionHandler) GetProductionProgress(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	if h.progressService == nil {
		c.JSON(http.StatusServiceUnavailable, errors.Internal("Progress service not available"))
		return
	}

	progress, err := h.progressService.GetProgress(c.Request.Context(), productionID)
	if err != nil {
		h.logger.Error("Failed to get production progress", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get progress"))
		return
	}

	if progress == nil {
		c.JSON(http.StatusOK, gin.H{
			"phase":   "unknown",
			"message": "No progress data available",
		})
		return
	}

	c.JSON(http.StatusOK, progress)
}

// RetryProduction retries a failed podcast production
func (h *ProductionHandler) RetryProduction(c *gin.Context) {
	productionID := c.Param("id")
	if productionID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Production ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context from middleware
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Get the production and validate it's in failed state
	production, err := h.productionService.GetProductionByID(c.Request.Context(), productionID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get production", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	if production.Status != models.ProductionStatusFailed {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Can only retry failed productions"))
		return
	}

	// Retry via production service
	err = h.productionService.RetryProduction(c.Request.Context(), production, userID)
	if err != nil {
		h.logger.Error("Failed to retry production", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Production retry initiated",
		zap.String("production_id", productionID),
		zap.String("user_id", userID),
	)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Retry initiated",
		"production": production.ToResponse(),
	})
}
