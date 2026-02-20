package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// CloudDriveProvider defines the interface for cloud drive operations
type CloudDriveProvider interface {
	// ListFiles lists files and folders at a given path
	ListFiles(ctx context.Context, path string, opts *ListFilesOptions) (*FileListResult, error)

	// SearchFiles searches for files matching a query
	SearchFiles(ctx context.Context, query string) (*FileListResult, error)

	// GetFileMetadata returns metadata for a single file
	GetFileMetadata(ctx context.Context, fileID string) (*FileItem, error)

	// DownloadFile downloads a file and returns its content
	DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) // content, mimeType, error
}

// ListFilesOptions provides options for listing files
type ListFilesOptions struct {
	SiteID    string
	LibraryID string
	PageSize  int
	PageToken string
}

// FileListResult represents the result of listing files
type FileListResult struct {
	Items         []*FileItem `json:"items"`
	NextPageToken string      `json:"next_page_token,omitempty"`
	TotalCount    int         `json:"total_count,omitempty"`
}

// FileItem represents a file or folder in a cloud drive
type FileItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`       // "file" or "folder"
	MimeType   string    `json:"mimeType"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Owner      string    `json:"owner,omitempty"`
	Path       string    `json:"path,omitempty"`
	WebURL     string    `json:"webUrl,omitempty"`
}

// SharePointSite represents a SharePoint site
type SharePointSite struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	WebURL      string `json:"webUrl,omitempty"`
}

// DocumentLibrary represents a SharePoint document library
type DocumentLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ImportResult represents the result of importing files
type ImportResult struct {
	Imported    int      `json:"imported"`
	Failed      int      `json:"failed"`
	FileIDs     []string `json:"file_ids,omitempty"`
	DocumentIDs []string `json:"document_ids,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

// CloudDriveService orchestrates cloud drive operations
type CloudDriveService struct {
	oauthService    *OAuthService
	storageService  *S3StorageService
	audiModalClient *AudiModalService
	documentService *DocumentService
	providers       map[string]func(ctx context.Context, credentialID, userID string) (CloudDriveProvider, error)
	logger          *logger.Logger
}

// NewCloudDriveService creates a new CloudDriveService
func NewCloudDriveService(
	oauthService *OAuthService,
	storageService *S3StorageService,
	audiModalClient *AudiModalService,
	documentService *DocumentService,
	log *logger.Logger,
) *CloudDriveService {
	svc := &CloudDriveService{
		oauthService:    oauthService,
		storageService:  storageService,
		audiModalClient: audiModalClient,
		documentService: documentService,
		providers:       make(map[string]func(ctx context.Context, credentialID, userID string) (CloudDriveProvider, error)),
		logger:          log.WithService("cloud_drive_service"),
	}

	// Register provider factories
	svc.providers["google"] = svc.createGoogleProvider
	svc.providers["onedrive"] = svc.createMicrosoftProvider
	svc.providers["sharepoint"] = svc.createMicrosoftProvider

	return svc
}

// ListFiles lists files from a cloud drive provider
func (s *CloudDriveService) ListFiles(ctx context.Context, provider, credentialID, userID, path string, opts *ListFilesOptions) (*FileListResult, error) {
	p, err := s.getProvider(ctx, provider, credentialID, userID)
	if err != nil {
		return nil, err
	}
	return p.ListFiles(ctx, path, opts)
}

// SearchFiles searches for files in a cloud drive
func (s *CloudDriveService) SearchFiles(ctx context.Context, provider, credentialID, userID, query string) (*FileListResult, error) {
	p, err := s.getProvider(ctx, provider, credentialID, userID)
	if err != nil {
		return nil, err
	}
	return p.SearchFiles(ctx, query)
}

// ImportFiles downloads files from the cloud drive and routes them through the document processing pipeline
func (s *CloudDriveService) ImportFiles(ctx context.Context, provider, credentialID, userID, notebookID string, fileIDs []string, spaceCtx *models.SpaceContext) (*ImportResult, error) {
	p, err := s.getProvider(ctx, provider, credentialID, userID)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{}

	for _, fileID := range fileIDs {
		// Get file metadata
		meta, err := p.GetFileMetadata(ctx, fileID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("metadata for %s: %v", fileID, err))
			continue
		}

		// Download file content
		content, mimeType, err := p.DownloadFile(ctx, fileID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", meta.Name, err))
			continue
		}

		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		// Route through the document processing pipeline (creates Neo4j Document node + AudiModal processing)
		if s.documentService != nil {
			uploadReq := models.DocumentUploadRequest{
				DocumentCreateRequest: models.DocumentCreateRequest{
					Name:       meta.Name,
					NotebookID: notebookID,
					SourceType: "cloud_drive",
					Metadata: map[string]interface{}{
						"cloud_drive_provider": provider,
						"cloud_drive_file_id":  fileID,
						"cloud_drive_web_url":  meta.WebURL,
					},
				},
				FileData: content,
			}

			fileInfo := models.FileInfo{
				OriginalName: meta.Name,
				MimeType:     mimeType,
				SizeBytes:    meta.Size,
			}

			doc, err := s.documentService.UploadDocument(ctx, uploadReq, userID, spaceCtx, fileInfo)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("process %s: %v", meta.Name, err))
				continue
			}

			result.Imported++
			result.FileIDs = append(result.FileIDs, fileID)
			result.DocumentIDs = append(result.DocumentIDs, doc.ID)

			s.logger.Info("Imported file from cloud drive via document pipeline",
				zap.String("provider", provider),
				zap.String("fileName", meta.Name),
				zap.String("fileId", fileID),
				zap.String("documentId", doc.ID),
				zap.Int64("size", meta.Size),
				zap.String("notebookId", notebookID),
			)
		} else {
			// Fallback: upload to MinIO directly (no AudiModal processing)
			objectKey := fmt.Sprintf("cloud-drive-imports/%s/%s/%s", userID, notebookID, meta.Name)
			if s.storageService != nil {
				_, err = s.storageService.UploadFile(ctx, objectKey, content, mimeType)
				if err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("storage upload %s: %v", meta.Name, err))
					continue
				}
			}

			result.Imported++
			result.FileIDs = append(result.FileIDs, fileID)

			s.logger.Warn("Imported file without document pipeline (no documentService)",
				zap.String("provider", provider),
				zap.String("fileName", meta.Name),
				zap.String("fileId", fileID),
			)
		}
	}

	return result, nil
}

// getProvider creates a cloud drive provider instance
func (s *CloudDriveService) getProvider(ctx context.Context, provider, credentialID, userID string) (CloudDriveProvider, error) {
	factory, ok := s.providers[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported cloud drive provider: %s", provider)
	}
	return factory(ctx, credentialID, userID)
}

func (s *CloudDriveService) createGoogleProvider(ctx context.Context, credentialID, userID string) (CloudDriveProvider, error) {
	tokenSource, err := s.oauthService.GetTokenSource(ctx, credentialID, userID, "google")
	if err != nil {
		return nil, fmt.Errorf("failed to get Google token: %w", err)
	}
	return NewGoogleDriveProvider(tokenSource, s.logger), nil
}

func (s *CloudDriveService) createMicrosoftProvider(ctx context.Context, credentialID, userID string) (CloudDriveProvider, error) {
	tokenSource, err := s.oauthService.GetTokenSource(ctx, credentialID, userID, "microsoft")
	if err != nil {
		return nil, fmt.Errorf("failed to get Microsoft token: %w", err)
	}
	return NewMicrosoftGraphProvider(tokenSource, s.logger), nil
}
