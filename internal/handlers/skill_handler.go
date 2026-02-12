package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SkillHandler proxies skill CRUD requests to agent-builder
type SkillHandler struct {
	agentBuilderURL string
	httpClient      *http.Client
	logger          *zap.Logger
}

// NewSkillHandler creates a new SkillHandler
func NewSkillHandler(agentBuilderURL string, log *logger.Logger) *SkillHandler {
	return &SkillHandler{
		agentBuilderURL: strings.TrimSuffix(agentBuilderURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log.Logger,
	}
}

// ListSkills handles GET /api/v1/skills
func (h *SkillHandler) ListSkills(c *gin.Context) {
	h.proxyToAgentBuilder(c, "GET", "/api/v1/skills?"+c.Request.URL.RawQuery, nil)
}

// GetSkill handles GET /api/v1/skills/:id
func (h *SkillHandler) GetSkill(c *gin.Context) {
	id := c.Param("id")
	h.proxyToAgentBuilder(c, "GET", "/api/v1/skills/"+id, nil)
}

// CreateSkill handles POST /api/v1/skills
func (h *SkillHandler) CreateSkill(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}
	h.proxyToAgentBuilder(c, "POST", "/api/v1/skills", body)
}

// UpdateSkill handles PUT /api/v1/skills/:id
func (h *SkillHandler) UpdateSkill(c *gin.Context) {
	id := c.Param("id")
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}
	h.proxyToAgentBuilder(c, "PUT", "/api/v1/skills/"+id, body)
}

// DeleteSkill handles DELETE /api/v1/skills/:id
func (h *SkillHandler) DeleteSkill(c *gin.Context) {
	id := c.Param("id")
	h.proxyToAgentBuilder(c, "DELETE", "/api/v1/skills/"+id, nil)
}

// proxyToAgentBuilder forwards a request to agent-builder and returns the response
func (h *SkillHandler) proxyToAgentBuilder(c *gin.Context, method, path string, body []byte) {
	url := h.agentBuilderURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, url, bodyReader)
	if err != nil {
		h.logger.Error("Failed to create proxy request", zap.String("url", url), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// Forward auth token
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("Agent-builder skill request failed", zap.String("url", url), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent-builder service unavailable"})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("Failed to read agent-builder response", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	// Forward status code and body
	if resp.StatusCode >= 400 {
		h.logger.Warn("Agent-builder returned error for skills",
			zap.String("url", url),
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
	}

	// Try to parse as JSON for clean forwarding
	var jsonResp interface{}
	if err := json.Unmarshal(respBody, &jsonResp); err == nil {
		c.JSON(resp.StatusCode, jsonResp)
	} else {
		c.Data(resp.StatusCode, "application/json", respBody)
	}
}

func init() {
	// Suppress unused import warning
	_ = fmt.Sprintf
}
