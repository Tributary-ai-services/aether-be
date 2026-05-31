package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// MicrosoftGraphProvider implements CloudDriveProvider using Microsoft Graph API v1.0
type MicrosoftGraphProvider struct {
	httpClient *http.Client
	logger     *logger.Logger
}

// NewMicrosoftGraphProvider creates a new Microsoft Graph provider
func NewMicrosoftGraphProvider(tokenSource oauth2.TokenSource, log *logger.Logger) *MicrosoftGraphProvider {
	return &MicrosoftGraphProvider{
		httpClient: oauth2.NewClient(context.Background(), tokenSource),
		logger:     log.WithService("microsoft_graph_provider"),
	}
}

const graphAPIBase = "https://graph.microsoft.com/v1.0"

// Microsoft Graph API response types
type graphDriveItemList struct {
	Value      []graphDriveItem `json:"value"`
	NextLink   string           `json:"@odata.nextLink"`
	TotalCount int              `json:"@odata.count"`
}

type graphDriveItem struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	Size                 int64            `json:"size"`
	WebURL               string           `json:"webUrl"`
	LastModifiedDateTime string           `json:"lastModifiedDateTime"`
	File                 *graphFileInfo   `json:"file,omitempty"`
	Folder               *graphFolderInfo `json:"folder,omitempty"`
	LastModifiedBy       *graphIdentity   `json:"lastModifiedBy,omitempty"`
	ParentReference      *graphParent     `json:"parentReference,omitempty"`
	DownloadURL          string           `json:"@microsoft.graph.downloadUrl,omitempty"`
}

type graphFileInfo struct {
	MimeType string `json:"mimeType"`
}

type graphFolderInfo struct {
	ChildCount int `json:"childCount"`
}

type graphIdentity struct {
	User struct {
		DisplayName string `json:"displayName"`
	} `json:"user"`
}

type graphParent struct {
	DriveID string `json:"driveId"`
	Path    string `json:"path"`
}

type graphSiteList struct {
	Value []graphSite `json:"value"`
}

type graphSite struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	WebURL      string `json:"webUrl"`
}

type graphDriveList struct {
	Value []graphDrive `json:"value"`
}

type graphDrive struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListFiles lists files in OneDrive or SharePoint
func (p *MicrosoftGraphProvider) ListFiles(ctx context.Context, path string, opts *ListFilesOptions) (*FileListResult, error) {
	var url string

	if opts != nil && opts.SiteID != "" && opts.LibraryID != "" {
		// SharePoint: list files in a document library drive
		if path == "" || path == "/" || path == "root" {
			url = fmt.Sprintf("%s/sites/%s/drives/%s/root/children", graphAPIBase, opts.SiteID, opts.LibraryID)
		} else {
			url = fmt.Sprintf("%s/sites/%s/drives/%s/items/%s/children", graphAPIBase, opts.SiteID, opts.LibraryID, path)
		}
	} else {
		// OneDrive: list files in user's drive
		if path == "" || path == "/" || path == "root" {
			url = fmt.Sprintf("%s/me/drive/root/children", graphAPIBase)
		} else {
			url = fmt.Sprintf("%s/me/drive/items/%s/children", graphAPIBase, path)
		}
	}

	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}
	url += fmt.Sprintf("?$top=%d&$orderby=name", pageSize)

	if opts != nil && opts.PageToken != "" {
		// For Microsoft Graph, PageToken is the full nextLink URL
		url = opts.PageToken
	}

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("microsoft graph API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// 404 = drive not provisioned or empty root — return empty list
		return &FileListResult{Items: []*FileItem{}}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("microsoft graph API returned %d: %s", resp.StatusCode, string(body))
	}

	var itemList graphDriveItemList
	if err := json.NewDecoder(resp.Body).Decode(&itemList); err != nil {
		return nil, fmt.Errorf("failed to decode graph response: %w", err)
	}

	items := make([]*FileItem, 0, len(itemList.Value))
	for _, item := range itemList.Value {
		items = append(items, p.convertItem(item))
	}

	return &FileListResult{
		Items:         items,
		NextPageToken: itemList.NextLink,
	}, nil
}

// SearchFiles searches for files in OneDrive
func (p *MicrosoftGraphProvider) SearchFiles(ctx context.Context, query string) (*FileListResult, error) {
	url := fmt.Sprintf("%s/me/drive/root/search(q='%s')?$top=50", graphAPIBase, query)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("microsoft graph search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("microsoft graph search returned %d: %s", resp.StatusCode, string(body))
	}

	var itemList graphDriveItemList
	if err := json.NewDecoder(resp.Body).Decode(&itemList); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	items := make([]*FileItem, 0, len(itemList.Value))
	for _, item := range itemList.Value {
		items = append(items, p.convertItem(item))
	}

	return &FileListResult{
		Items:         items,
		NextPageToken: itemList.NextLink,
	}, nil
}

// GetFileMetadata returns metadata for a single file
func (p *MicrosoftGraphProvider) GetFileMetadata(ctx context.Context, fileID string) (*FileItem, error) {
	url := fmt.Sprintf("%s/me/drive/items/%s", graphAPIBase, fileID)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get file metadata returned %d: %s", resp.StatusCode, string(body))
	}

	var item graphDriveItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode file metadata: %w", err)
	}

	return p.convertItem(item), nil
}

// DownloadFile downloads a file from OneDrive/SharePoint.
// Uses the @microsoft.graph.downloadUrl from metadata to avoid redirect auth issues.
// The Graph /content endpoint returns a 302 redirect to a CDN, and Go's oauth2 client
// sends the Bearer token on the redirect, causing 401 from the CDN.
func (p *MicrosoftGraphProvider) DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	// First get file metadata which includes a pre-authenticated download URL
	metaURL := fmt.Sprintf("%s/me/drive/items/%s", graphAPIBase, fileID)
	metaResp, err := p.httpClient.Get(metaURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get file metadata for download: %w", err)
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(metaResp.Body)
		return nil, "", fmt.Errorf("metadata request returned %d: %s", metaResp.StatusCode, string(body))
	}

	var item graphDriveItem
	if err := json.NewDecoder(metaResp.Body).Decode(&item); err != nil {
		return nil, "", fmt.Errorf("failed to decode metadata: %w", err)
	}

	// Use the pre-authenticated download URL (no auth header needed)
	downloadURL := item.DownloadURL
	if downloadURL == "" {
		return nil, "", fmt.Errorf("no download URL available for file %s", fileID)
	}

	p.logger.Info("Downloading file via pre-authenticated URL",
		zap.String("fileId", fileID),
		zap.String("fileName", item.Name),
	)

	// Use a plain HTTP client (no OAuth headers) for the pre-authenticated URL
	plainClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := plainClient.Get(downloadURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("download returned %d: %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file content: %w", err)
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" && item.File != nil {
		mimeType = item.File.MimeType
	}

	p.logger.Info("Downloaded file from Microsoft Graph",
		zap.String("fileId", fileID),
		zap.Int("size", len(content)),
	)

	return content, mimeType, nil
}

// ListSites lists SharePoint sites accessible to the user
func (p *MicrosoftGraphProvider) ListSites(ctx context.Context) ([]*SharePointSite, error) {
	url := fmt.Sprintf("%s/sites?search=*&$top=100", graphAPIBase)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list SharePoint sites: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sites returned %d: %s", resp.StatusCode, string(body))
	}

	var siteList graphSiteList
	if err := json.NewDecoder(resp.Body).Decode(&siteList); err != nil {
		return nil, fmt.Errorf("failed to decode sites response: %w", err)
	}

	sites := make([]*SharePointSite, 0, len(siteList.Value))
	for _, s := range siteList.Value {
		name := s.DisplayName
		if name == "" {
			name = s.Name
		}
		sites = append(sites, &SharePointSite{
			ID:          s.ID,
			Name:        name,
			Description: s.Description,
			WebURL:      s.WebURL,
		})
	}

	return sites, nil
}

// ListDocumentLibraries lists document libraries for a SharePoint site
func (p *MicrosoftGraphProvider) ListDocumentLibraries(ctx context.Context, siteID string) ([]*DocumentLibrary, error) {
	url := fmt.Sprintf("%s/sites/%s/drives", graphAPIBase, siteID)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list document libraries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list libraries returned %d: %s", resp.StatusCode, string(body))
	}

	var driveList graphDriveList
	if err := json.NewDecoder(resp.Body).Decode(&driveList); err != nil {
		return nil, fmt.Errorf("failed to decode libraries response: %w", err)
	}

	libraries := make([]*DocumentLibrary, 0, len(driveList.Value))
	for _, d := range driveList.Value {
		libraries = append(libraries, &DocumentLibrary{
			ID:   d.ID,
			Name: d.Name,
		})
	}

	return libraries, nil
}

// convertItem converts a Microsoft Graph drive item to our FileItem type
func (p *MicrosoftGraphProvider) convertItem(item graphDriveItem) *FileItem {
	itemType := "file"
	mimeType := ""

	if item.Folder != nil {
		itemType = "folder"
	} else if item.File != nil {
		mimeType = item.File.MimeType
	}

	var modifiedAt time.Time
	if item.LastModifiedDateTime != "" {
		modifiedAt, _ = time.Parse(time.RFC3339, item.LastModifiedDateTime)
	}

	owner := ""
	if item.LastModifiedBy != nil {
		owner = item.LastModifiedBy.User.DisplayName
	}

	path := ""
	if item.ParentReference != nil {
		path = item.ParentReference.Path
	}

	return &FileItem{
		ID:         item.ID,
		Name:       item.Name,
		Type:       itemType,
		MimeType:   mimeType,
		Size:       item.Size,
		ModifiedAt: modifiedAt,
		Owner:      owner,
		Path:       path,
		WebURL:     item.WebURL,
	}
}
