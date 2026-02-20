package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// GoogleDriveProvider implements CloudDriveProvider using Google Drive API v3
type GoogleDriveProvider struct {
	httpClient *http.Client
	logger     *logger.Logger
}

// NewGoogleDriveProvider creates a new Google Drive provider
func NewGoogleDriveProvider(tokenSource oauth2.TokenSource, log *logger.Logger) *GoogleDriveProvider {
	return &GoogleDriveProvider{
		httpClient: oauth2.NewClient(context.Background(), tokenSource),
		logger:     log.WithService("google_drive_provider"),
	}
}

// Google Drive API response types
type googleFileList struct {
	NextPageToken string        `json:"nextPageToken"`
	Files         []googleFile  `json:"files"`
}

type googleFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	Size         string    `json:"size"`
	ModifiedTime string    `json:"modifiedTime"`
	Owners       []struct {
		DisplayName string `json:"displayName"`
	} `json:"owners"`
	WebViewLink string `json:"webViewLink"`
	Parents     []string `json:"parents"`
}

const (
	googleDriveAPIBase = "https://www.googleapis.com/drive/v3"
	googleDriveFields  = "id,name,mimeType,size,modifiedTime,owners,webViewLink,parents"
)

// Google Workspace MIME type mappings for export
var googleExportMimeTypes = map[string]struct {
	ExportMime string
	Extension  string
}{
	"application/vnd.google-apps.document":     {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"},
	"application/vnd.google-apps.spreadsheet":  {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"},
	"application/vnd.google-apps.presentation": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"},
	"application/vnd.google-apps.drawing":      {"application/pdf", ".pdf"},
}

// ListFiles lists files in Google Drive
func (p *GoogleDriveProvider) ListFiles(ctx context.Context, path string, opts *ListFilesOptions) (*FileListResult, error) {
	// Build query - path is used as parent folder ID
	query := "trashed = false"
	if path != "" && path != "/" && path != "root" {
		query += fmt.Sprintf(" and '%s' in parents", path)
	} else {
		query += " and 'root' in parents"
	}

	url := fmt.Sprintf("%s/files?q=%s&fields=nextPageToken,files(%s)&orderBy=folder,name",
		googleDriveAPIBase, url.QueryEscape(query), googleDriveFields)

	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}
	url += fmt.Sprintf("&pageSize=%d", pageSize)

	if opts != nil && opts.PageToken != "" {
		url += fmt.Sprintf("&pageToken=%s", opts.PageToken)
	}

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("google drive API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google drive API returned %d: %s", resp.StatusCode, string(body))
	}

	var fileList googleFileList
	if err := json.NewDecoder(resp.Body).Decode(&fileList); err != nil {
		return nil, fmt.Errorf("failed to decode google drive response: %w", err)
	}

	items := make([]*FileItem, 0, len(fileList.Files))
	for _, f := range fileList.Files {
		items = append(items, p.convertFile(f))
	}

	return &FileListResult{
		Items:         items,
		NextPageToken: fileList.NextPageToken,
	}, nil
}

// SearchFiles searches for files in Google Drive
func (p *GoogleDriveProvider) SearchFiles(ctx context.Context, query string) (*FileListResult, error) {
	q := fmt.Sprintf("trashed = false and (name contains '%s' or fullText contains '%s')", query, query)

	url := fmt.Sprintf("%s/files?q=%s&fields=nextPageToken,files(%s)&pageSize=50",
		googleDriveAPIBase, url.QueryEscape(q), googleDriveFields)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("google drive search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google drive search returned %d: %s", resp.StatusCode, string(body))
	}

	var fileList googleFileList
	if err := json.NewDecoder(resp.Body).Decode(&fileList); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	items := make([]*FileItem, 0, len(fileList.Files))
	for _, f := range fileList.Files {
		items = append(items, p.convertFile(f))
	}

	return &FileListResult{
		Items:         items,
		NextPageToken: fileList.NextPageToken,
	}, nil
}

// GetFileMetadata returns metadata for a single file
func (p *GoogleDriveProvider) GetFileMetadata(ctx context.Context, fileID string) (*FileItem, error) {
	url := fmt.Sprintf("%s/files/%s?fields=%s", googleDriveAPIBase, fileID, googleDriveFields)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get file metadata returned %d: %s", resp.StatusCode, string(body))
	}

	var file googleFile
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("failed to decode file metadata: %w", err)
	}

	return p.convertFile(file), nil
}

// DownloadFile downloads a file from Google Drive
func (p *GoogleDriveProvider) DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	// First get file metadata to determine download method
	meta, err := p.GetFileMetadata(ctx, fileID)
	if err != nil {
		return nil, "", err
	}

	var downloadURL string

	// Check if it's a Google Workspace file that needs export
	if exportInfo, isWorkspace := googleExportMimeTypes[meta.MimeType]; isWorkspace {
		downloadURL = fmt.Sprintf("%s/files/%s/export?mimeType=%s",
			googleDriveAPIBase, fileID, url.QueryEscape(exportInfo.ExportMime))
		meta.MimeType = exportInfo.ExportMime
	} else {
		downloadURL = fmt.Sprintf("%s/files/%s?alt=media", googleDriveAPIBase, fileID)
	}

	resp, err := p.httpClient.Get(downloadURL)
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

	p.logger.Info("Downloaded file from Google Drive",
		zap.String("fileId", fileID),
		zap.String("fileName", meta.Name),
		zap.Int("size", len(content)),
	)

	return content, meta.MimeType, nil
}

// convertFile converts a Google Drive file to our FileItem type
func (p *GoogleDriveProvider) convertFile(f googleFile) *FileItem {
	itemType := "file"
	if f.MimeType == "application/vnd.google-apps.folder" {
		itemType = "folder"
	}

	var size int64
	if f.Size != "" {
		fmt.Sscanf(f.Size, "%d", &size)
	}

	var modifiedAt time.Time
	if f.ModifiedTime != "" {
		modifiedAt, _ = time.Parse(time.RFC3339, f.ModifiedTime)
	}

	owner := ""
	if len(f.Owners) > 0 {
		owner = f.Owners[0].DisplayName
	}

	return &FileItem{
		ID:         f.ID,
		Name:       f.Name,
		Type:       itemType,
		MimeType:   f.MimeType,
		Size:       size,
		ModifiedAt: modifiedAt,
		Owner:      owner,
		WebURL:     f.WebViewLink,
	}
}
