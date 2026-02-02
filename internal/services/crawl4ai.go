package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// Crawl4AIService provides web scraping capabilities via Crawl4AI
type Crawl4AIService struct {
	baseURL    string
	httpClient *http.Client
	log        *logger.Logger
	config     *config.Crawl4AIConfig
}

// ============================================================================
// Request/Response Types
// ============================================================================

// Crawl4AIRequest represents a crawl request to Crawl4AI
type Crawl4AIRequest struct {
	URLs               []string `json:"urls"`
	WordCountThreshold int      `json:"word_count_threshold,omitempty"`
	ExcludedTags       []string `json:"excluded_tags,omitempty"`
	ExcludeExternalLinks bool   `json:"exclude_external_links,omitempty"`
	ExcludeExternalImages bool  `json:"exclude_external_images,omitempty"`
	ProcessIframes     bool     `json:"process_iframes,omitempty"`
	RemoveOverlay      bool     `json:"remove_overlay_elements,omitempty"`
	Screenshot         bool     `json:"screenshot,omitempty"`
	ScreenshotFullPage bool     `json:"screenshot_full_page,omitempty"`
}

// Crawl4AIResponse represents the response from Crawl4AI
type Crawl4AIResponse struct {
	Success bool              `json:"success"`
	Results []Crawl4AIResult  `json:"results"`
}

// Crawl4AIMarkdown represents the markdown content from Crawl4AI (can be object or string)
type Crawl4AIMarkdown struct {
	RawMarkdown           string `json:"raw_markdown"`
	MarkdownWithCitations string `json:"markdown_with_citations,omitempty"`
	ReferencesMarkdown    string `json:"references_markdown,omitempty"`
	FitMarkdown           string `json:"fit_markdown,omitempty"`
	FitHTML               string `json:"fit_html,omitempty"`
}

// UnmarshalJSON handles both string and object formats for markdown field
func (m *Crawl4AIMarkdown) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		m.RawMarkdown = str
		return nil
	}

	// If not a string, try to unmarshal as an object
	type Alias Crawl4AIMarkdown
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	return json.Unmarshal(data, aux)
}

// Crawl4AIResult represents a single crawl result
type Crawl4AIResult struct {
	URL              string            `json:"url"`
	Success          bool              `json:"success"`
	HTML             string            `json:"html,omitempty"`
	CleanedHTML      string            `json:"cleaned_html,omitempty"`
	FitHTML          string            `json:"fit_html,omitempty"`
	Markdown         *Crawl4AIMarkdown `json:"markdown,omitempty"`
	ExtractedContent string            `json:"extracted_content,omitempty"`
	Media            Crawl4AIMedia     `json:"media,omitempty"`
	Links            Crawl4AILinks     `json:"links,omitempty"`
	Metadata         Crawl4AIMetadata  `json:"metadata,omitempty"`
	ErrorMessage     string            `json:"error_message,omitempty"`
	Screenshot       string            `json:"screenshot,omitempty"`
	ResponseHeaders  map[string]string `json:"response_headers,omitempty"`
}

// Crawl4AIMedia contains extracted media information
type Crawl4AIMedia struct {
	Images []Crawl4AIImage `json:"images,omitempty"`
	Videos []Crawl4AIVideo `json:"videos,omitempty"`
	Audios []Crawl4AIAudio `json:"audios,omitempty"`
}

// Crawl4AIImage represents an extracted image
type Crawl4AIImage struct {
	Src    string `json:"src"`
	Alt    string `json:"alt,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Crawl4AIVideo represents an extracted video
type Crawl4AIVideo struct {
	Src    string `json:"src"`
	Poster string `json:"poster,omitempty"`
}

// Crawl4AIAudio represents an extracted audio
type Crawl4AIAudio struct {
	Src string `json:"src"`
}

// Crawl4AILinks contains extracted links
type Crawl4AILinks struct {
	Internal []Crawl4AILink `json:"internal,omitempty"`
	External []Crawl4AILink `json:"external,omitempty"`
}

// Crawl4AILink represents an extracted link
type Crawl4AILink struct {
	Href       string `json:"href"`
	Text       string `json:"text,omitempty"`
	Title      string `json:"title,omitempty"`
	BaseDomain string `json:"base_domain,omitempty"`
}

// Crawl4AIMetadata contains page metadata
type Crawl4AIMetadata struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Keywords    string `json:"keywords,omitempty"`
	Author      string `json:"author,omitempty"`
}

// Crawl4AIHealthResponse represents health check response
type Crawl4AIHealthResponse struct {
	Status    string  `json:"status"`
	Timestamp float64 `json:"timestamp"`
	Version   string  `json:"version"`
}

// ============================================================================
// URL Probe Types
// ============================================================================

// URLProbeResult contains the result of probing a URL for AI-friendly content
type URLProbeResult struct {
	URL                 string              `json:"url"`
	Accessible          bool                `json:"accessible"`
	StatusCode          int                 `json:"status_code,omitempty"`
	HasLlmsTxt          bool                `json:"has_llms_txt"`
	HasLlmsFullTxt      bool                `json:"has_llms_full_txt"`
	HasAiTxt            bool                `json:"has_ai_txt"`
	RobotsRules         *RobotsRules        `json:"robots_rules,omitempty"`
	RequiresJavaScript  bool                `json:"requires_javascript"`
	HasPaywall          bool                `json:"has_paywall"`
	IsArchiveAvailable  bool                `json:"is_archive_available"`
	ArchiveURL          string              `json:"archive_url,omitempty"`
	RecommendedScraper  string              `json:"recommended_scraper"`
	Reasoning           string              `json:"reasoning"`
	LlmsTxtContent      *LlmsTxtContent     `json:"llms_txt_content,omitempty"`
	AiTxtContent        *AiTxtContent       `json:"ai_txt_content,omitempty"`
	Error               string              `json:"error,omitempty"`
}

// RobotsRules contains parsed robots.txt rules relevant to AI
type RobotsRules struct {
	DisallowedForAI  bool     `json:"disallowed_for_ai"`
	DisallowedPaths  []string `json:"disallowed_paths,omitempty"`
	AllowedPaths     []string `json:"allowed_paths,omitempty"`
	CrawlDelay       int      `json:"crawl_delay,omitempty"`
	Sitemaps         []string `json:"sitemaps,omitempty"`
}

// LlmsTxtContent contains parsed llms.txt content
type LlmsTxtContent struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Sections    []LlmsTxtSection    `json:"sections,omitempty"`
	Links       []LlmsTxtLink       `json:"links,omitempty"`
	Raw         string              `json:"raw,omitempty"`
}

// LlmsTxtSection represents a section in llms.txt
type LlmsTxtSection struct {
	Name    string   `json:"name"`
	Content []string `json:"content"`
}

// LlmsTxtLink represents a link in llms.txt
type LlmsTxtLink struct {
	Text    string `json:"text"`
	URL     string `json:"url"`
	Section string `json:"section,omitempty"`
}

// AiTxtContent contains parsed ai.txt content
type AiTxtContent struct {
	AllowAIAccess   bool   `json:"allow_ai_access"`
	AllowTraining   bool   `json:"allow_training"`
	AllowScraping   bool   `json:"allow_scraping"`
	PreferredFormat string `json:"preferred_format,omitempty"`
	RateLimit       string `json:"rate_limit,omitempty"`
	Contact         string `json:"contact,omitempty"`
	Raw             string `json:"raw,omitempty"`
}

// ScrapeResult represents the result of scraping a URL
type ScrapeResult struct {
	URL         string            `json:"url"`
	Success     bool              `json:"success"`
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content"`
	ContentType string            `json:"content_type"`
	Markdown    string            `json:"markdown,omitempty"`
	HTML        string            `json:"html,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Media       *Crawl4AIMedia    `json:"media,omitempty"`
	Links       *Crawl4AILinks    `json:"links,omitempty"`
	ScraperUsed string            `json:"scraper_used"`
	ScrapedAt   time.Time         `json:"scraped_at"`
	Error       string            `json:"error,omitempty"`
}

// Scraper type constants
const (
	ScraperDirectFetch = "direct_fetch"
	ScraperCrawl4AI    = "crawl4ai"
	ScraperArchiveOrg  = "archive_org"
	ScraperPlaywright  = "playwright"
	ScraperManual      = "manual"
)

// Known paywalled domains
var paywalledDomains = []string{
	"nytimes.com",
	"wsj.com",
	"ft.com",
	"economist.com",
	"bloomberg.com",
	"washingtonpost.com",
	"medium.com",
	"substack.com",
	"patreon.com",
}

// AI user agents for robots.txt parsing
var aiUserAgents = []string{
	"gptbot",
	"chatgpt-user",
	"claude-web",
	"anthropic-ai",
	"cohere-ai",
	"perplexitybot",
	"bytespider",
	"ccbot",
	"diffbot",
	"facebookbot",
	"google-extended",
}

// ============================================================================
// Constructor
// ============================================================================

// NewCrawl4AIService creates a new Crawl4AI service
func NewCrawl4AIService(cfg *config.Crawl4AIConfig, log *logger.Logger) *Crawl4AIService {
	timeout := 60 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	return &Crawl4AIService{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		log:    log.WithService("crawl4ai"),
		config: cfg,
	}
}

// ============================================================================
// Health Check
// ============================================================================

// HealthCheck checks if Crawl4AI service is available
func (s *Crawl4AIService) HealthCheck(ctx context.Context) (*Crawl4AIHealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Crawl4AI health endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Crawl4AI health check failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var health Crawl4AIHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode health response: %w", err)
	}

	return &health, nil
}

// ============================================================================
// URL Probing
// ============================================================================

// ProbeURL probes a URL to detect AI-friendly content and determine the best scraping strategy
func (s *Crawl4AIService) ProbeURL(ctx context.Context, targetURL string) (*URLProbeResult, error) {
	s.log.Debug("Probing URL", zap.String("url", targetURL))

	result := &URLProbeResult{
		URL:                targetURL,
		Accessible:         false,
		RequiresJavaScript: true, // Assume true until proven otherwise
		RecommendedScraper: ScraperCrawl4AI,
	}

	// Normalize URL
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		result.Error = fmt.Sprintf("Invalid URL: %v", err)
		result.Reasoning = "URL could not be parsed"
		return result, nil
	}

	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "https"
	}
	targetURL = parsedURL.String()
	result.URL = targetURL

	// Check if domain is known paywalled
	result.HasPaywall = s.isPaywalledDomain(parsedURL.Host)

	// Probe well-known files in parallel
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Check llms.txt
	llmsTxt, err := s.fetchWellKnownFile(ctx, baseURL+"/.well-known/llms.txt")
	if err == nil && llmsTxt != "" {
		result.HasLlmsTxt = true
		result.LlmsTxtContent = s.parseLlmsTxt(llmsTxt)
	}

	// Check llms-full.txt
	llmsFullTxt, err := s.fetchWellKnownFile(ctx, baseURL+"/.well-known/llms-full.txt")
	if err == nil && llmsFullTxt != "" {
		result.HasLlmsFullTxt = true
	}

	// Check ai.txt
	aiTxt, err := s.fetchWellKnownFile(ctx, baseURL+"/.well-known/ai.txt")
	if err == nil && aiTxt != "" {
		result.HasAiTxt = true
		result.AiTxtContent = s.parseAiTxt(aiTxt)
	}

	// Check robots.txt
	robotsTxt, err := s.fetchWellKnownFile(ctx, baseURL+"/robots.txt")
	if err == nil && robotsTxt != "" {
		result.RobotsRules = s.parseRobotsTxt(robotsTxt)
	}

	// Check if main URL is accessible
	headResp, err := s.httpClient.Head(targetURL)
	if err == nil {
		result.Accessible = true
		result.StatusCode = headResp.StatusCode
		headResp.Body.Close()

		// Check if JavaScript is required (simple heuristic)
		contentType := headResp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			// Most modern sites require JS, but static HTML sites don't
			result.RequiresJavaScript = true
		}
	}

	// Check archive.org availability
	archiveURL, available := s.checkArchiveAvailability(ctx, targetURL)
	result.IsArchiveAvailable = available
	result.ArchiveURL = archiveURL

	// Determine recommended scraper
	result.RecommendedScraper, result.Reasoning = s.determineRecommendedScraper(result)

	return result, nil
}

// fetchWellKnownFile fetches a well-known file from a URL
func (s *Crawl4AIService) fetchWellKnownFile(ctx context.Context, fileURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "AetherBot/1.0 (AI-friendly content discovery)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// isPaywalledDomain checks if a domain is known to have a paywall
func (s *Crawl4AIService) isPaywalledDomain(host string) bool {
	host = strings.ToLower(host)
	for _, domain := range paywalledDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// parseLlmsTxt parses llms.txt content
func (s *Crawl4AIService) parseLlmsTxt(content string) *LlmsTxtContent {
	result := &LlmsTxtContent{Raw: content}
	lines := strings.Split(content, "\n")

	var currentSection *LlmsTxtSection

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Title (# heading)
		if strings.HasPrefix(trimmed, "# ") && result.Title == "" {
			result.Title = strings.TrimPrefix(trimmed, "# ")
			continue
		}

		// Description (> blockquote)
		if strings.HasPrefix(trimmed, "> ") && result.Description == "" {
			result.Description = strings.TrimPrefix(trimmed, "> ")
			continue
		}

		// Section (## heading)
		if strings.HasPrefix(trimmed, "## ") {
			currentSection = &LlmsTxtSection{
				Name:    strings.TrimPrefix(trimmed, "## "),
				Content: []string{},
			}
			result.Sections = append(result.Sections, *currentSection)
			continue
		}

		// Links [text](url)
		if strings.Contains(trimmed, "](") {
			// Simple link extraction
			start := strings.Index(trimmed, "[")
			mid := strings.Index(trimmed, "](")
			end := strings.Index(trimmed, ")")
			if start >= 0 && mid > start && end > mid {
				text := trimmed[start+1 : mid]
				linkURL := trimmed[mid+2 : end]
				sectionName := ""
				if currentSection != nil {
					sectionName = currentSection.Name
				}
				result.Links = append(result.Links, LlmsTxtLink{
					Text:    text,
					URL:     linkURL,
					Section: sectionName,
				})
			}
		}

		// Add to current section
		if currentSection != nil && len(result.Sections) > 0 {
			result.Sections[len(result.Sections)-1].Content = append(
				result.Sections[len(result.Sections)-1].Content, trimmed)
		}
	}

	return result
}

// parseAiTxt parses ai.txt content
func (s *Crawl4AIService) parseAiTxt(content string) *AiTxtContent {
	result := &AiTxtContent{
		AllowAIAccess: true,
		AllowTraining: true,
		AllowScraping: true,
		Raw:           content,
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))

		if strings.HasPrefix(lower, "allow-ai:") {
			val := strings.TrimSpace(strings.TrimPrefix(lower, "allow-ai:"))
			result.AllowAIAccess = val == "true" || val == "yes"
		}
		if strings.HasPrefix(lower, "allow-training:") {
			val := strings.TrimSpace(strings.TrimPrefix(lower, "allow-training:"))
			result.AllowTraining = val == "true" || val == "yes"
		}
		if strings.HasPrefix(lower, "allow-scraping:") {
			val := strings.TrimSpace(strings.TrimPrefix(lower, "allow-scraping:"))
			result.AllowScraping = val == "true" || val == "yes"
		}
		if strings.HasPrefix(lower, "preferred-format:") {
			result.PreferredFormat = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
		if strings.HasPrefix(lower, "rate-limit:") {
			result.RateLimit = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
		if strings.HasPrefix(lower, "contact:") {
			result.Contact = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}

	return result
}

// parseRobotsTxt parses robots.txt for AI-relevant rules
func (s *Crawl4AIService) parseRobotsTxt(content string) *RobotsRules {
	result := &RobotsRules{
		DisallowedPaths: []string{},
		AllowedPaths:    []string{},
		Sitemaps:        []string{},
	}

	lines := strings.Split(content, "\n")
	var currentUserAgent string
	isRelevantAgent := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}

		// User-agent directive
		if strings.HasPrefix(lower, "user-agent:") {
			agent := strings.TrimSpace(strings.TrimPrefix(lower, "user-agent:"))
			currentUserAgent = agent
			isRelevantAgent = agent == "*"
			for _, aiAgent := range aiUserAgents {
				if strings.Contains(agent, aiAgent) {
					isRelevantAgent = true
					break
				}
			}
			continue
		}

		// Only process rules for relevant user agents
		if !isRelevantAgent {
			continue
		}

		// Disallow directive
		if strings.HasPrefix(lower, "disallow:") {
			path := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if path == "/" || path == "/*" {
				result.DisallowedForAI = true
			}
			if path != "" {
				result.DisallowedPaths = append(result.DisallowedPaths, path)
			}
		}

		// Allow directive
		if strings.HasPrefix(lower, "allow:") {
			path := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if path != "" {
				result.AllowedPaths = append(result.AllowedPaths, path)
			}
		}

		// Crawl-delay
		if strings.HasPrefix(lower, "crawl-delay:") {
			var delay int
			fmt.Sscanf(strings.TrimPrefix(lower, "crawl-delay:"), "%d", &delay)
			result.CrawlDelay = delay
		}

		// Sitemap
		if strings.HasPrefix(lower, "sitemap:") {
			sitemap := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if sitemap != "" {
				result.Sitemaps = append(result.Sitemaps, sitemap)
			}
		}
	}

	_ = currentUserAgent // Suppress unused variable warning

	return result
}

// checkArchiveAvailability checks if a URL is available on archive.org
func (s *Crawl4AIService) checkArchiveAvailability(ctx context.Context, targetURL string) (string, bool) {
	apiURL := fmt.Sprintf("https://archive.org/wayback/available?url=%s", url.QueryEscape(targetURL))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", false
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var result struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Timestamp string `json:"timestamp"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false
	}

	if result.ArchivedSnapshots.Closest.Available {
		return result.ArchivedSnapshots.Closest.URL, true
	}

	return "", false
}

// determineRecommendedScraper determines the best scraper based on probe results
func (s *Crawl4AIService) determineRecommendedScraper(result *URLProbeResult) (string, string) {
	// Priority 1: AI-friendly sites with llms.txt
	if result.HasLlmsTxt || result.HasLlmsFullTxt {
		return ScraperDirectFetch, "Site provides AI-friendly content via llms.txt. Direct fetch will retrieve optimized content."
	}

	// Priority 2: Check ai.txt permissions
	if result.AiTxtContent != nil && !result.AiTxtContent.AllowAIAccess {
		if result.IsArchiveAvailable {
			return ScraperArchiveOrg, "Site blocks AI access via ai.txt. Using archive.org as a fallback."
		}
		return ScraperManual, "Site blocks AI access via ai.txt. Please copy/paste the content manually."
	}

	// Priority 3: Check robots.txt for AI agent rules
	if result.RobotsRules != nil && result.RobotsRules.DisallowedForAI {
		if result.IsArchiveAvailable {
			return ScraperArchiveOrg, "Site blocks AI crawlers via robots.txt. Using archive.org as a fallback."
		}
		return ScraperManual, "Site blocks AI crawlers via robots.txt. Please copy/paste the content manually."
	}

	// Priority 4: Paywall detection
	if result.HasPaywall {
		if result.IsArchiveAvailable {
			return ScraperArchiveOrg, "Paywall detected. Using archive.org to retrieve cached version of the content."
		}
		return ScraperManual, "Paywall detected and no archive available. Please copy/paste the content manually."
	}

	// Priority 5: JavaScript requirement
	if result.RequiresJavaScript {
		return ScraperCrawl4AI, "Site requires JavaScript rendering. Crawl4AI will handle dynamic content and browser emulation."
	}

	// Default: Direct fetch for simple pages
	return ScraperDirectFetch, "Site is accessible and does not require JavaScript rendering."
}

// ============================================================================
// Web Scraping
// ============================================================================

// ScrapeURL scrapes a URL using the specified or recommended scraper
func (s *Crawl4AIService) ScrapeURL(ctx context.Context, targetURL string, scraperType string, options map[string]interface{}) (*ScrapeResult, error) {
	s.log.Info("Scraping URL",
		zap.String("url", targetURL),
		zap.String("scraper", scraperType))

	result := &ScrapeResult{
		URL:         targetURL,
		ScraperUsed: scraperType,
		ScrapedAt:   time.Now(),
	}

	var err error
	switch scraperType {
	case ScraperCrawl4AI:
		err = s.scrapeWithCrawl4AI(ctx, targetURL, options, result)
	case ScraperDirectFetch:
		err = s.scrapeDirectFetch(ctx, targetURL, result)
	case ScraperArchiveOrg:
		archiveURL, _ := options["archive_url"].(string)
		err = s.scrapeArchiveOrg(ctx, targetURL, archiveURL, result)
	default:
		// Default to Crawl4AI
		result.ScraperUsed = ScraperCrawl4AI
		err = s.scrapeWithCrawl4AI(ctx, targetURL, options, result)
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result, nil
	}

	result.Success = true
	return result, nil
}

// scrapeWithCrawl4AI scrapes a URL using Crawl4AI service
func (s *Crawl4AIService) scrapeWithCrawl4AI(ctx context.Context, targetURL string, options map[string]interface{}, result *ScrapeResult) error {
	crawlReq := Crawl4AIRequest{
		URLs:               []string{targetURL},
		WordCountThreshold: 10,
	}

	// Apply options
	if threshold, ok := options["word_count_threshold"].(int); ok {
		crawlReq.WordCountThreshold = threshold
	}
	if screenshot, ok := options["screenshot"].(bool); ok {
		crawlReq.Screenshot = screenshot
	}

	reqBody, err := json.Marshal(crawlReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/crawl", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Crawl4AI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Crawl4AI returned status %d: %s", resp.StatusCode, string(body))
	}

	var crawlResp Crawl4AIResponse
	if err := json.NewDecoder(resp.Body).Decode(&crawlResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !crawlResp.Success || len(crawlResp.Results) == 0 {
		return fmt.Errorf("Crawl4AI returned no results")
	}

	crawlResult := crawlResp.Results[0]
	if !crawlResult.Success {
		return fmt.Errorf("crawl failed: %s", crawlResult.ErrorMessage)
	}

	// Extract markdown content - Crawl4AI returns it as an object with raw_markdown field
	markdown := ""
	if crawlResult.Markdown != nil && crawlResult.Markdown.RawMarkdown != "" {
		markdown = crawlResult.Markdown.RawMarkdown
	}

	// Populate result - use markdown as primary content since that's where Crawl4AI puts the text
	result.Title = crawlResult.Metadata.Title
	result.Markdown = markdown
	result.HTML = crawlResult.HTML
	result.Media = &crawlResult.Media
	result.Links = &crawlResult.Links
	result.Metadata = map[string]string{
		"title":       crawlResult.Metadata.Title,
		"description": crawlResult.Metadata.Description,
		"keywords":    crawlResult.Metadata.Keywords,
		"author":      crawlResult.Metadata.Author,
	}

	// Set Content field - prefer markdown (text content), fall back to cleaned HTML, then raw HTML
	if markdown != "" {
		result.Content = markdown
		result.ContentType = "text/markdown"
	} else if crawlResult.CleanedHTML != "" {
		result.Content = crawlResult.CleanedHTML
		result.ContentType = "text/html"
	} else if crawlResult.HTML != "" {
		result.Content = crawlResult.HTML
		result.ContentType = "text/html"
	}

	// Log content sizes for debugging
	s.log.Debug("Crawl4AI scrape completed",
		zap.String("url", targetURL),
		zap.String("title", result.Title),
		zap.Int("markdown_length", len(result.Markdown)),
		zap.Int("content_length", len(result.Content)),
		zap.Int("html_length", len(result.HTML)))

	return nil
}

// scrapeDirectFetch scrapes a URL using direct HTTP fetch
func (s *Crawl4AIService) scrapeDirectFetch(ctx context.Context, targetURL string, result *ScrapeResult) error {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "AetherBot/1.0 (AI content processing)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	content := string(body)
	result.Content = content
	result.ContentType = resp.Header.Get("Content-Type")
	result.HTML = content

	// Extract title from HTML
	result.Title = extractHTMLTitle(content)

	// Initialize metadata map
	result.Metadata = map[string]string{
		"title": result.Title,
	}

	return nil
}

// extractHTMLTitle extracts the title from HTML content
func extractHTMLTitle(html string) string {
	// Simple regex-free approach to extract title
	startTag := "<title>"
	endTag := "</title>"

	// Case-insensitive search
	htmlLower := strings.ToLower(html)
	start := strings.Index(htmlLower, startTag)
	if start == -1 {
		return ""
	}
	start += len(startTag)

	end := strings.Index(htmlLower[start:], endTag)
	if end == -1 {
		return ""
	}

	title := html[start : start+end]
	// Clean up whitespace and newlines
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.ReplaceAll(title, "\r", " ")
	// Collapse multiple spaces
	for strings.Contains(title, "  ") {
		title = strings.ReplaceAll(title, "  ", " ")
	}

	return title
}

// scrapeArchiveOrg scrapes a URL from archive.org
func (s *Crawl4AIService) scrapeArchiveOrg(ctx context.Context, targetURL, archiveURL string, result *ScrapeResult) error {
	// If no archive URL provided, get the latest one
	if archiveURL == "" {
		var available bool
		archiveURL, available = s.checkArchiveAvailability(ctx, targetURL)
		if !available {
			return fmt.Errorf("no archive available for this URL")
		}
	}

	// Fetch from archive.org using Crawl4AI for JS rendering
	return s.scrapeWithCrawl4AI(ctx, archiveURL, nil, result)
}

// ============================================================================
// Enabled Check
// ============================================================================

// IsEnabled returns whether the Crawl4AI service is enabled
func (s *Crawl4AIService) IsEnabled() bool {
	return s.config != nil && s.config.Enabled
}
