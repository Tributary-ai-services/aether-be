package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// DataSourceHandler handles data source operations including URL probing and web scraping
type DataSourceHandler struct {
	crawl4aiService *services.Crawl4AIService
	log             *logger.Logger
}

// NewDataSourceHandler creates a new data source handler
func NewDataSourceHandler(crawl4aiService *services.Crawl4AIService, log *logger.Logger) *DataSourceHandler {
	return &DataSourceHandler{
		crawl4aiService: crawl4aiService,
		log:             log.WithService("data_source_handler"),
	}
}

// ============================================================================
// Request/Response Types
// ============================================================================

// ProbeURLRequest represents a URL probe request
type ProbeURLRequest struct {
	URL string `json:"url" binding:"required"`
}

// ProbeURLResponse represents a URL probe response
type ProbeURLResponse struct {
	Success bool                     `json:"success"`
	Data    *services.URLProbeResult `json:"data,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

// ScrapeURLRequest represents a URL scrape request
type ScrapeURLRequest struct {
	URL         string                 `json:"url" binding:"required"`
	ScraperType string                 `json:"scraper_type,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ScrapeURLResponse represents a URL scrape response
type ScrapeURLResponse struct {
	Success bool                   `json:"success"`
	Data    *services.ScrapeResult `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// HealthResponse represents the Crawl4AI health check response
type Crawl4AIHealthResponse struct {
	Success bool                            `json:"success"`
	Data    *services.Crawl4AIHealthResponse `json:"data,omitempty"`
	Error   string                          `json:"error,omitempty"`
}

// ============================================================================
// Handlers
// ============================================================================

// ProbeURL probes a URL to detect AI-friendly content and determine the best scraping strategy
// @Summary Probe a URL for AI-friendly content
// @Description Checks a URL for llms.txt, ai.txt, robots.txt rules, paywall detection, and archive.org availability
// @Tags Data Sources
// @Accept json
// @Produce json
// @Param request body ProbeURLRequest true "URL to probe"
// @Success 200 {object} ProbeURLResponse
// @Failure 400 {object} ProbeURLResponse
// @Failure 500 {object} ProbeURLResponse
// @Router /api/v1/data-sources/probe-url [post]
func (h *DataSourceHandler) ProbeURL(c *gin.Context) {
	var req ProbeURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ProbeURLResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.URL == "" {
		c.JSON(http.StatusBadRequest, ProbeURLResponse{
			Success: false,
			Error:   "URL is required",
		})
		return
	}

	// Check if Crawl4AI is enabled
	if !h.crawl4aiService.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, ProbeURLResponse{
			Success: false,
			Error:   "Web scraping service is not enabled",
		})
		return
	}

	// Probe the URL
	result, err := h.crawl4aiService.ProbeURL(c.Request.Context(), req.URL)
	if err != nil {
		h.log.Error("Failed to probe URL",
			zap.String("url", req.URL),
			zap.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, ProbeURLResponse{
			Success: false,
			Error:   "Failed to probe URL: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ProbeURLResponse{
		Success: true,
		Data:    result,
	})
}

// ScrapeURL scrapes a URL using the specified or recommended scraper
// @Summary Scrape a URL for content
// @Description Scrapes a URL using Crawl4AI, direct fetch, or archive.org depending on the scraper type
// @Tags Data Sources
// @Accept json
// @Produce json
// @Param request body ScrapeURLRequest true "URL to scrape and scraper options"
// @Success 200 {object} ScrapeURLResponse
// @Failure 400 {object} ScrapeURLResponse
// @Failure 500 {object} ScrapeURLResponse
// @Router /api/v1/data-sources/scrape-url [post]
func (h *DataSourceHandler) ScrapeURL(c *gin.Context) {
	var req ScrapeURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ScrapeURLResponse{
			Success: false,
			Error:   "Invalid request: " + err.Error(),
		})
		return
	}

	if req.URL == "" {
		c.JSON(http.StatusBadRequest, ScrapeURLResponse{
			Success: false,
			Error:   "URL is required",
		})
		return
	}

	// Check if Crawl4AI is enabled
	if !h.crawl4aiService.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, ScrapeURLResponse{
			Success: false,
			Error:   "Web scraping service is not enabled",
		})
		return
	}

	// Default scraper type if not specified
	scraperType := req.ScraperType
	if scraperType == "" {
		scraperType = services.ScraperCrawl4AI
	}

	// Initialize options map if nil
	options := req.Options
	if options == nil {
		options = make(map[string]interface{})
	}

	// Scrape the URL
	result, err := h.crawl4aiService.ScrapeURL(c.Request.Context(), req.URL, scraperType, options)
	if err != nil {
		h.log.Error("Failed to scrape URL",
			zap.String("url", req.URL),
			zap.String("scraper", scraperType),
			zap.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, ScrapeURLResponse{
			Success: false,
			Error:   "Failed to scrape URL: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ScrapeURLResponse{
		Success: true,
		Data:    result,
	})
}

// GetCrawl4AIHealth checks the health of the Crawl4AI service
// @Summary Check Crawl4AI service health
// @Description Returns the health status of the Crawl4AI web scraping service
// @Tags Data Sources
// @Produce json
// @Success 200 {object} Crawl4AIHealthResponse
// @Failure 500 {object} Crawl4AIHealthResponse
// @Router /api/v1/data-sources/health [get]
func (h *DataSourceHandler) GetCrawl4AIHealth(c *gin.Context) {
	// Check if Crawl4AI is enabled
	if !h.crawl4aiService.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, Crawl4AIHealthResponse{
			Success: false,
			Error:   "Web scraping service is not enabled",
		})
		return
	}

	health, err := h.crawl4aiService.HealthCheck(c.Request.Context())
	if err != nil {
		h.log.Error("Crawl4AI health check failed", zap.String("error", err.Error()))
		c.JSON(http.StatusServiceUnavailable, Crawl4AIHealthResponse{
			Success: false,
			Error:   "Crawl4AI service unavailable: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, Crawl4AIHealthResponse{
		Success: true,
		Data:    health,
	})
}

// GetScraperTypes returns the available scraper types
// @Summary Get available scraper types
// @Description Returns a list of available web scrapers and their descriptions
// @Tags Data Sources
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/data-sources/scrapers [get]
func (h *DataSourceHandler) GetScraperTypes(c *gin.Context) {
	scrapers := []map[string]interface{}{
		{
			"id":          services.ScraperDirectFetch,
			"name":        "Direct Fetch",
			"description": "Simple HTTP fetch for static content and AI-friendly sites with llms.txt",
			"best_for":    "Static HTML pages, sites with llms.txt",
		},
		{
			"id":          services.ScraperCrawl4AI,
			"name":        "Crawl4AI",
			"description": "Full browser rendering with JavaScript execution for dynamic sites",
			"best_for":    "JavaScript-heavy sites, SPAs, dynamic content",
		},
		{
			"id":          services.ScraperArchiveOrg,
			"name":        "Archive.org",
			"description": "Fetch cached versions from the Wayback Machine",
			"best_for":    "Paywalled content, blocked sites, historical versions",
		},
		{
			"id":          services.ScraperPlaywright,
			"name":        "Playwright",
			"description": "Advanced browser automation for complex interactive sites",
			"best_for":    "Sites requiring authentication or complex interactions",
		},
		{
			"id":          services.ScraperManual,
			"name":        "Manual",
			"description": "User copies and pastes content manually",
			"best_for":    "Sites that block all automated access",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"scrapers": scrapers,
	})
}
