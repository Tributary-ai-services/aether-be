package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// RegistrationHandler handles public user registration
type RegistrationHandler struct {
	keycloakClient *auth.KeycloakClient
	logger         *logger.Logger
}

// NewRegistrationHandler creates a new RegistrationHandler
func NewRegistrationHandler(kc *auth.KeycloakClient, log *logger.Logger) *RegistrationHandler {
	return &RegistrationHandler{
		keycloakClient: kc,
		logger:         log.WithService("registration"),
	}
}

// RegisterRequest represents a user registration request
type RegisterRequest struct {
	FirstName string `json:"firstName" binding:"required"`
	LastName  string `json:"lastName" binding:"required"`
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8"`
}

// Register handles POST /api/v1/auth/register (public, no auth required)
func (h *RegistrationHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": "Invalid registration data: " + err.Error(),
		})
		return
	}

	// Normalize email
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	h.logger.Info("User registration attempt",
		zap.String("email", req.Email),
		zap.String("first_name", req.FirstName),
	)

	// Create user in Keycloak
	keycloakUserID, err := h.keycloakClient.RegisterUser(
		c.Request.Context(),
		req.FirstName,
		req.LastName,
		req.Email,
		req.Password,
	)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			c.JSON(http.StatusConflict, gin.H{
				"code":    "USER_EXISTS",
				"message": "A user with this email already exists. Please sign in instead.",
			})
			return
		}

		h.logger.Error("Failed to register user in Keycloak",
			zap.String("email", req.Email),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "REGISTRATION_FAILED",
			"message": "Failed to create account. Please try again later.",
		})
		return
	}

	h.logger.Info("User registered successfully",
		zap.String("email", req.Email),
		zap.String("keycloak_user_id", keycloakUserID),
	)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully. You can now sign in.",
		"userId":  keycloakUserID,
	})
}
