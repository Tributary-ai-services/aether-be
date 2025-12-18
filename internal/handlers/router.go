package handlers

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// RouterHandler handles proxy requests to the LLM router service
type RouterHandler struct {
	config     *config.RouterConfig
	httpClient *http.Client
	logger     *logger.Logger
	baseURL    string
}

// NewRouterHandler creates a new router handler with proxy functionality
func NewRouterHandler(routerConfig *config.RouterConfig, log *logger.Logger) (*RouterHandler, error) {
	if !routerConfig.Enabled {
		return nil, nil
	}

	// Parse timeout durations
	timeout, err := time.ParseDuration(routerConfig.Service.Timeout)
	if err != nil {
		return nil, errors.BadRequest("invalid router service timeout: " + err.Error())
	}

	connectTimeout, err := time.ParseDuration(routerConfig.Service.ConnectTimeout)
	if err != nil {
		return nil, errors.BadRequest("invalid router service connect timeout: " + err.Error())
	}

	// Create HTTP client with timeouts
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			ResponseHeaderTimeout: connectTimeout,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	return &RouterHandler{
		config:     routerConfig,
		httpClient: httpClient,
		logger:     log.WithService("router_proxy"),
		baseURL:    strings.TrimRight(routerConfig.Service.BaseURL, "/"),
	}, nil
}

// ProxyRequest handles generic proxy requests to the LLM router service
func (h *RouterHandler) ProxyRequest(c *gin.Context, targetPath string) {
	// Replace path parameters in target
	finalTarget := h.buildTargetPath(targetPath, c.Params)
	targetURL := h.baseURL + finalTarget

	// Add query parameters if present
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	h.logger.Info("Proxying request to LLM router",
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("target", targetURL),
		zap.String("user_agent", c.GetHeader("User-Agent")))

	// Create request
	var body io.Reader
	if c.Request.Body != nil {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			h.logger.WithError(err).Error("Failed to read request body")
			c.JSON(http.StatusBadRequest, errors.BadRequest("Failed to read request body"))
			return
		}
		body = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, body)
	if err != nil {
		h.logger.WithError(err).Error("Failed to create proxy request")
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create proxy request"))
		return
	}

	// Copy relevant headers (will handle authentication based on configuration)
	h.copyHeaders(c.Request, req, c)

	// Execute request with retry logic
	resp, err := h.executeWithRetry(req)
	if err != nil {
		h.logger.WithError(err).Error("Failed to execute proxy request")
		c.JSON(http.StatusBadGateway, errors.ExternalService("Router service unavailable", err))
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// Copy response status and body
	c.Status(resp.StatusCode)
	
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.WithError(err).Error("Failed to read response body")
		c.JSON(http.StatusBadGateway, errors.ExternalService("Failed to read router response", err))
		return
	}

	// Determine content type and write response
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		c.Data(resp.StatusCode, contentType, responseBody)
	} else {
		c.Data(resp.StatusCode, contentType, responseBody)
	}

	h.logger.Info("Proxy request completed",
		zap.Int("status_code", resp.StatusCode),
		zap.Int("response_size", len(responseBody)),
		zap.String("target", targetURL))
}

// buildTargetPath replaces path parameters in the target path
func (h *RouterHandler) buildTargetPath(target string, params gin.Params) string {
	result := target
	for _, param := range params {
		placeholder := "{" + param.Key + "}"
		result = strings.ReplaceAll(result, placeholder, param.Value)
	}
	return result
}

// copyHeaders copies relevant headers from the original request to the proxy request
// and handles dual authentication modes (service-to-service vs user authentication)
func (h *RouterHandler) copyHeaders(original *http.Request, proxy *http.Request, c *gin.Context) {
	// Handle authentication based on configuration
	if h.config.Service.UseServiceAuth && h.config.Service.APIKey != "" {
		// Mode 1: Service-to-Service Authentication
		proxy.Header.Set("X-API-Key", h.config.Service.APIKey)
		
		// Forward user context as metadata headers for audit/logging
		if userID, exists := c.Get("user_id"); exists {
			if uid, ok := userID.(string); ok && uid != "" {
				proxy.Header.Set("X-User-Context", uid)
			}
		}
		if userEmail, exists := c.Get("user_email"); exists {
			if email, ok := userEmail.(string); ok && email != "" {
				proxy.Header.Set("X-User-Email", email)
			}
		}
		
		h.logger.Debug("Using service-to-service authentication",
			zap.String("auth_mode", "service"),
			zap.Bool("user_context_forwarded", c.GetString("user_id") != ""))
	} else {
		// Mode 2: User Authentication Pass-through
		if authHeader := original.Header.Get("Authorization"); authHeader != "" {
			proxy.Header.Set("Authorization", authHeader)
			h.logger.Debug("Using user authentication pass-through",
				zap.String("auth_mode", "user"),
				zap.String("auth_header_prefix", authHeader[:min(20, len(authHeader))]))
		}
	}

	// Headers to copy (excluding Authorization which is handled above)
	headersToProxy := []string{
		"Content-Type",
		"Accept",
		"Accept-Encoding",
		"Accept-Language",
		"User-Agent",
		"X-Request-ID",
		"X-Forwarded-For",
		"X-Real-IP",
	}

	for _, header := range headersToProxy {
		if value := original.Header.Get(header); value != "" {
			proxy.Header.Set(header, value)
		}
	}

	// Add additional proxy headers
	proxy.Header.Set("X-Forwarded-Host", original.Host)
	proxy.Header.Set("X-Forwarded-Proto", original.URL.Scheme)
	if original.URL.Scheme == "" {
		proxy.Header.Set("X-Forwarded-Proto", "http") // Default for development
	}
	
	// Add service identification header
	proxy.Header.Set("X-Forwarded-Service", "aether-backend")
}

// Helper function for min (Go doesn't have a built-in min for integers)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// executeWithRetry executes the request with retry logic based on configuration
func (h *RouterHandler) executeWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	maxRetries := h.config.Service.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request for each attempt (to handle body re-reading)
		reqClone, err := h.cloneRequest(req)
		if err != nil {
			return nil, err
		}

		resp, err := h.httpClient.Do(reqClone)
		if err == nil && resp.StatusCode < 500 {
			// Success or client error (4xx) - don't retry
			return resp, nil
		}

		if err != nil {
			lastErr = err
		} else {
			// Server error (5xx) - close response and prepare for retry
			resp.Body.Close()
			lastErr = errors.ExternalService("Server error from router: "+strconv.Itoa(resp.StatusCode), nil)
		}

		// Don't sleep after the last attempt
		if attempt < maxRetries {
			sleepTime := time.Duration(attempt+1) * time.Second
			h.logger.Warn("Retrying proxy request",
				zap.Int("attempt", attempt + 1),
				zap.Int("max_retries", maxRetries),
				zap.Duration("sleep_time", sleepTime),
				zap.String("error", lastErr.Error()))
			time.Sleep(sleepTime)
		}
	}

	return nil, lastErr
}

// cloneRequest creates a copy of the HTTP request for retries
func (h *RouterHandler) cloneRequest(original *http.Request) (*http.Request, error) {
	var body io.Reader
	if original.Body != nil {
		bodyBytes, err := io.ReadAll(original.Body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(bodyBytes)
		// Reset original body for potential future reads
		original.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	req, err := http.NewRequestWithContext(original.Context(), original.Method, original.URL.String(), body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	req.Header = original.Header.Clone()
	return req, nil
}

// Handler methods for specific endpoints

// GetProviders proxies GET /providers requests
func (h *RouterHandler) GetProviders(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.Providers)
}

// GetProvider proxies GET /providers/{name} requests
func (h *RouterHandler) GetProvider(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.ProviderDetail)
}

// GetHealth proxies GET /health requests
func (h *RouterHandler) GetHealth(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.Health)
}

// GetCapabilities proxies GET /capabilities requests
func (h *RouterHandler) GetCapabilities(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.Capabilities)
}

// ChatCompletions proxies POST /chat/completions requests
func (h *RouterHandler) ChatCompletions(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.ChatCompletions)
}

// Completions proxies POST /completions requests
func (h *RouterHandler) Completions(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.Completions)
}

// Messages proxies POST /messages requests
func (h *RouterHandler) Messages(c *gin.Context) {
	h.ProxyRequest(c, h.config.Endpoints.Messages)
}