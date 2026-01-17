package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"go.uber.org/zap"

	appConfig "github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// S3StorageService implements StorageService for AWS S3/MinIO
type S3StorageService struct {
	client *s3.Client
	bucket string
	logger *logger.Logger
	config appConfig.StorageConfig
}

// NewS3StorageService creates a new S3 storage service
func NewS3StorageService(cfg appConfig.StorageConfig, log *logger.Logger) (*S3StorageService, error) {
	// Load AWS configuration
	awsConfig, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Override credentials if provided
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		awsConfig.Credentials = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     cfg.AccessKeyID,
				SecretAccessKey: cfg.SecretAccessKey,
			}, nil
		})
	}

	// Create S3 client
	s3Client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for MinIO
			log.Info("S3 client configured with custom endpoint",
				zap.String("endpoint", cfg.Endpoint),
				zap.Bool("use_path_style", true),
			)
		}
	})

	service := &S3StorageService{
		client: s3Client,
		bucket: cfg.Bucket,
		logger: log.WithService("s3_storage"),
		config: cfg,
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := service.testConnection(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to S3: %w", err)
	}

	service.logger.Info("S3 storage service initialized",
		zap.String("bucket", cfg.Bucket),
		zap.String("region", cfg.Region),
		zap.String("endpoint", cfg.Endpoint),
	)

	return service, nil
}

// UploadFile uploads a file to S3
func (s *S3StorageService) UploadFile(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	start := time.Now()

	s.logger.Warn("Server-side encryption disabled - MinIO KMS not configured")
	
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
		Metadata: map[string]string{
			"uploaded-by": "aether-backend",
			"upload-time": time.Now().Format(time.RFC3339),
		},
	}

	_, err := s.client.PutObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to upload file to S3",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Int("size_bytes", len(data)),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	s.logger.Info("File uploaded to S3 successfully",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Int("size_bytes", len(data)),
		zap.Float64("duration_ms", duration),
	)

	return key, nil
}

// DownloadFile downloads a file from S3
func (s *S3StorageService) DownloadFile(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		duration := time.Since(start).Seconds() * 1000
		s.logger.Error("Failed to download file from S3",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer result.Body.Close()

	// Read the body
	buf := bytes.NewBuffer(nil)
	_, err = buf.ReadFrom(result.Body)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to read file body",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to read file body: %w", err)
	}

	data := buf.Bytes()
	s.logger.Debug("File downloaded from S3 successfully",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Int("size_bytes", len(data)),
		zap.Float64("duration_ms", duration),
	)

	return data, nil
}

// DeleteFile deletes a file from S3
func (s *S3StorageService) DeleteFile(ctx context.Context, key string) error {
	start := time.Now()

	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	_, err := s.client.DeleteObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to delete file from S3",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete file: %w", err)
	}

	s.logger.Info("File deleted from S3 successfully",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Float64("duration_ms", duration),
	)

	return nil
}

// GetFileURL generates a presigned URL for file access
func (s *S3StorageService) GetFileURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	result, err := presignClient.PresignGetObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = expiration
	})

	if err != nil {
		s.logger.Error("Failed to generate presigned URL",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Duration("expiration", expiration),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	s.logger.Debug("Presigned URL generated successfully",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Duration("expiration", expiration),
	)

	return result.URL, nil
}

// GetFileInfo retrieves file metadata
func (s *S3StorageService) GetFileInfo(ctx context.Context, key string) (*FileMetadata, error) {
	start := time.Now()

	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	result, err := s.client.HeadObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to get file info from S3",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	metadata := &FileMetadata{
		Key:          key,
		Size:         aws.ToInt64(result.ContentLength),
		ContentType:  aws.ToString(result.ContentType),
		ETag:         aws.ToString(result.ETag),
		LastModified: aws.ToTime(result.LastModified),
		Metadata:     result.Metadata,
	}

	s.logger.Debug("File info retrieved successfully",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Int64("size", metadata.Size),
		zap.String("content_type", metadata.ContentType),
		zap.Float64("duration_ms", duration),
	)

	return metadata, nil
}

// FileExists checks if a file exists in S3
func (s *S3StorageService) FileExists(ctx context.Context, key string) (bool, error) {
	start := time.Now()

	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	_, err := s.client.HeadObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		// Check if error is "not found"
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			s.logger.Debug("File does not exist",
				zap.String("key", key),
				zap.String("bucket", s.bucket),
				zap.Float64("duration_ms", duration),
			)
			return false, nil
		}

		s.logger.Error("Failed to check file existence",
			zap.String("key", key),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return false, fmt.Errorf("failed to check file existence: %w", err)
	}

	s.logger.Debug("File exists",
		zap.String("key", key),
		zap.String("bucket", s.bucket),
		zap.Float64("duration_ms", duration),
	)

	return true, nil
}

// ListFiles lists files with a given prefix
func (s *S3StorageService) ListFiles(ctx context.Context, prefix string, maxKeys int) ([]*FileMetadata, error) {
	start := time.Now()

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(maxKeys)),
	}

	result, err := s.client.ListObjectsV2(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to list files from S3",
			zap.String("prefix", prefix),
			zap.String("bucket", s.bucket),
			zap.Int("max_keys", maxKeys),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	files := make([]*FileMetadata, 0, len(result.Contents))
	for _, obj := range result.Contents {
		files = append(files, &FileMetadata{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			ETag:         aws.ToString(obj.ETag),
			LastModified: aws.ToTime(obj.LastModified),
		})
	}

	s.logger.Debug("Files listed successfully",
		zap.String("prefix", prefix),
		zap.String("bucket", s.bucket),
		zap.Int("count", len(files)),
		zap.Float64("duration_ms", duration),
	)

	return files, nil
}

// CopyFile copies a file within S3
func (s *S3StorageService) CopyFile(ctx context.Context, sourceKey, destKey string) error {
	start := time.Now()

	source := fmt.Sprintf("%s/%s", s.bucket, sourceKey)
	s.logger.Warn("Server-side encryption disabled for copy operation - MinIO KMS not configured")
	
	input := &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(destKey),
		CopySource:        aws.String(source),
		Metadata: map[string]string{
			"copied-by": "aether-backend",
			"copy-time": time.Now().Format(time.RFC3339),
		},
		MetadataDirective: types.MetadataDirectiveReplace,
	}

	_, err := s.client.CopyObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to copy file in S3",
			zap.String("source_key", sourceKey),
			zap.String("dest_key", destKey),
			zap.String("bucket", s.bucket),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return fmt.Errorf("failed to copy file: %w", err)
	}

	s.logger.Info("File copied in S3 successfully",
		zap.String("source_key", sourceKey),
		zap.String("dest_key", destKey),
		zap.String("bucket", s.bucket),
		zap.Float64("duration_ms", duration),
	)

	return nil
}

// HealthCheck performs a health check on the S3 service
func (s *S3StorageService) HealthCheck(ctx context.Context) error {
	return s.testConnection(ctx)
}

// testConnection tests the connection to S3
func (s *S3StorageService) testConnection(ctx context.Context) error {
	// Try to head the bucket
	input := &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	}

	_, err := s.client.HeadBucket(ctx, input)
	if err != nil {
		// Check if it's a "bucket not found" error
		var nfe *types.NoSuchBucket
		if errors.As(err, &nfe) {
			// Bucket doesn't exist, try to create it
			s.logger.Info("Bucket doesn't exist, creating it",
				zap.String("bucket", s.bucket),
			)
			
			createInput := &s3.CreateBucketInput{
				Bucket: aws.String(s.bucket),
			}
			
			_, createErr := s.client.CreateBucket(ctx, createInput)
			if createErr != nil {
				s.logger.Error("Failed to create bucket",
					zap.String("bucket", s.bucket),
					zap.Error(createErr),
				)
				return fmt.Errorf("failed to create bucket: %w", createErr)
			}
			
			s.logger.Info("Bucket created successfully",
				zap.String("bucket", s.bucket),
			)
			
			// Test connection again after creating bucket
			_, err = s.client.HeadBucket(ctx, input)
			if err != nil {
				s.logger.Error("S3 connection test failed after creating bucket",
					zap.String("bucket", s.bucket),
					zap.Error(err),
				)
				return fmt.Errorf("S3 connection test failed after creating bucket: %w", err)
			}
		} else {
			s.logger.Error("S3 connection test failed",
				zap.String("bucket", s.bucket),
				zap.Error(err),
			)
			return fmt.Errorf("S3 connection test failed: %w", err)
		}
	}

	s.logger.Debug("S3 connection test successful",
		zap.String("bucket", s.bucket),
	)

	return nil
}

// FileMetadata represents file metadata in storage
type FileMetadata struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type,omitempty"`
	ETag         string            `json:"etag,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// GetBucketName returns the configured bucket name
func (s *S3StorageService) GetBucketName() string {
	return s.bucket
}

// GetEndpoint returns the configured endpoint
func (s *S3StorageService) GetEndpoint() string {
	return s.config.Endpoint
}

// UploadFileToTenantBucket uploads a file to a tenant-specific S3 bucket
func (s *S3StorageService) UploadFileToTenantBucket(ctx context.Context, tenantID, key string, data []byte, contentType string) (string, error) {
	start := time.Now()

	s.logger.Info("Starting tenant bucket file upload",
		zap.String("tenant_id", tenantID),
		zap.String("key", key),
		zap.String("content_type", contentType),
		zap.Int("file_size", len(data)))

	// Extract tenant suffix from tenant_1756161631 -> 1756161631
	tenantSuffix := extractTenantSuffix(tenantID)
	bucketName := fmt.Sprintf("aether-%s", tenantSuffix)
	
	s.logger.Info("Using tenant bucket", 
		zap.String("bucket_name", bucketName),
		zap.String("tenant_suffix", tenantSuffix))

	// Ensure bucket exists
	if err := s.ensureBucketExists(ctx, bucketName); err != nil {
		s.logger.Error("Failed to ensure bucket exists", 
			zap.String("bucket_name", bucketName),
			zap.Error(err))
		return "", fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	s.logger.Warn("Server-side encryption disabled for tenant bucket - MinIO KMS not configured",
		zap.String("bucket", bucketName),
		zap.String("tenant_id", tenantID))
	
	input := &s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
		Metadata: map[string]string{
			"uploaded-by": "aether-backend",
			"upload-time": time.Now().Format(time.RFC3339),
			"tenant-id":   tenantID,
		},
	}

	s.logger.Info("About to call S3 PutObject", 
		zap.String("bucket", bucketName),
		zap.String("key", key),
		zap.Int("content_length", len(data)),
		zap.String("content_type", contentType))

	// Log detailed request information
	s.logger.Debug("S3 PutObject request details",
		zap.String("endpoint", s.config.Endpoint),
		zap.String("bucket", bucketName),
		zap.String("key", key),
		zap.String("region", s.config.Region),
		zap.Bool("path_style", true),
		zap.String("expected_url", fmt.Sprintf("%s/%s/%s", s.config.Endpoint, bucketName, key)),
	)

	_, err := s.client.PutObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		// Extract more error details
		errMsg := err.Error()
		s.logger.Error("Failed to upload file to tenant bucket",
			zap.String("key", key),
			zap.String("bucket", bucketName),
			zap.String("tenant_id", tenantID),
			zap.Int("size_bytes", len(data)),
			zap.Float64("duration_ms", duration),
			zap.String("error_message", errMsg),
			zap.String("endpoint", s.config.Endpoint),
			zap.Error(err),
		)
		
		// Log the full error chain
		if strings.Contains(errMsg, "https response error") || strings.Contains(errMsg, "http response error") {
			s.logger.Error("S3 HTTP response error detected",
				zap.String("full_error", errMsg),
			)
		}
		
		// Extract detailed error information
		var opErr *smithy.OperationError
		if errors.As(err, &opErr) {
			s.logger.Error("Operation error details",
				zap.String("service", opErr.Service()),
				zap.String("operation", opErr.Operation()),
			)
			
			// Check for S3 specific response error
			var s3Err interface {
				ServiceHostID() string
				Error() string
			}
			if errors.As(opErr.Err, &s3Err) {
				s.logger.Error("S3 service error details",
					zap.String("host_id", s3Err.ServiceHostID()),
					zap.String("s3_error", s3Err.Error()),
				)
			}
			
			// Try to get the underlying HTTP response error
			var respErr *awshttp.ResponseError
			if errors.As(opErr.Err, &respErr) {
				s.logger.Error("HTTP response error details",
					zap.Int("status_code", respErr.Response.StatusCode),
					zap.String("status", respErr.Response.Status),
					zap.String("request_id", respErr.Response.Header.Get("x-amz-request-id")),
					zap.String("host_id", respErr.Response.Header.Get("x-amz-id-2")),
				)
				
				// Log all response headers for debugging
				headers := make(map[string]string)
				for k, v := range respErr.Response.Header {
					if len(v) > 0 {
						headers[k] = v[0]
					}
				}
				s.logger.Error("Full response headers",
					zap.Any("headers", headers),
				)
			}
		}
		
		// Also try direct HTTP error extraction
		var httpErr *awshttp.ResponseError
		if errors.As(err, &httpErr) {
			s.logger.Error("Direct HTTP response error",
				zap.Int("status_code", httpErr.Response.StatusCode),
				zap.String("status", httpErr.Response.Status),
			)
		}
		
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	s.logger.Info("File uploaded to tenant bucket successfully",
		zap.String("key", key),
		zap.String("bucket", bucketName),
		zap.String("tenant_id", tenantID),
		zap.Int("size_bytes", len(data)),
		zap.Float64("duration_ms", duration),
	)

	return fmt.Sprintf("%s:%s", bucketName, key), nil
}

// DownloadFileFromTenantBucket downloads a file from a tenant-specific S3 bucket
func (s *S3StorageService) DownloadFileFromTenantBucket(ctx context.Context, tenantID, key string) ([]byte, error) {
	start := time.Now()
	
	s.logger.Info("Starting tenant bucket file download",
		zap.String("tenant_id", tenantID),
		zap.String("key", key),
	)

	// Extract tenant suffix from ID (e.g., tenant-1234567890 -> 1234567890)
	tenantSuffix := extractTenantSuffix(tenantID)
	bucketName := fmt.Sprintf("aether-%s", tenantSuffix)

	s.logger.Info("Using tenant bucket", 
		zap.String("bucket", bucketName),
		zap.String("tenant_suffix", tenantSuffix),
	)

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		duration := time.Since(start).Seconds() * 1000
		s.logger.Error("Failed to download file from tenant bucket",
			zap.String("key", key),
			zap.String("bucket", bucketName),
			zap.String("tenant_id", tenantID),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to download file from tenant bucket: %w", err)
	}
	defer result.Body.Close()

	// Read the body
	buf := bytes.NewBuffer(nil)
	_, err = buf.ReadFrom(result.Body)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to read file body from tenant bucket",
			zap.String("key", key),
			zap.String("bucket", bucketName),
			zap.String("tenant_id", tenantID),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to read file body: %w", err)
	}

	data := buf.Bytes()
	s.logger.Info("File downloaded from tenant bucket successfully",
		zap.String("key", key),
		zap.String("bucket", bucketName),
		zap.String("tenant_id", tenantID),
		zap.Int("size_bytes", len(data)),
		zap.Float64("duration_ms", duration),
	)

	return data, nil
}

// DeleteFileFromTenantBucket deletes a file from a tenant-specific S3 bucket
func (s *S3StorageService) DeleteFileFromTenantBucket(ctx context.Context, tenantID, key string) error {
	start := time.Now()

	s.logger.Info("Starting tenant bucket file deletion",
		zap.String("tenant_id", tenantID),
		zap.String("key", key),
	)

	// Extract tenant suffix from ID (e.g., tenant-1234567890 -> 1234567890)
	tenantSuffix := extractTenantSuffix(tenantID)
	bucketName := fmt.Sprintf("aether-%s", tenantSuffix)

	s.logger.Info("Using tenant bucket for deletion", 
		zap.String("bucket", bucketName),
		zap.String("tenant_suffix", tenantSuffix),
	)

	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	_, err := s.client.DeleteObject(ctx, input)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		s.logger.Error("Failed to delete file from tenant bucket",
			zap.String("key", key),
			zap.String("bucket", bucketName),
			zap.String("tenant_id", tenantID),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete file from tenant bucket: %w", err)
	}

	s.logger.Info("File deleted from tenant bucket successfully",
		zap.String("key", key),
		zap.String("bucket", bucketName),
		zap.String("tenant_id", tenantID),
		zap.Float64("duration_ms", duration),
	)

	return nil
}

// ensureBucketExists creates a bucket if it doesn't exist
func (s *S3StorageService) ensureBucketExists(ctx context.Context, bucketName string) error {
	// Log the bucket check
	s.logger.Debug("Checking if bucket exists",
		zap.String("bucket", bucketName),
		zap.String("endpoint", s.config.Endpoint),
	)

	// Check if bucket exists
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	
	if err == nil {
		// Bucket exists
		s.logger.Debug("Bucket already exists",
			zap.String("bucket", bucketName),
		)
		return nil
	}

	// Check if error is "not found"
	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		// Some other error occurred
		s.logger.Error("Failed to check bucket existence",
			zap.String("bucket", bucketName),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Error(err),
		)
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	// Bucket doesn't exist, create it
	s.logger.Info("Creating new tenant bucket",
		zap.String("bucket", bucketName),
	)

	createInput := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}

	// Add location constraint if not using default region
	if s.config.Region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.config.Region),
		}
	}

	_, err = s.client.CreateBucket(ctx, createInput)
	if err != nil {
		// Check if bucket already exists (race condition)
		var bucketAlreadyExists *types.BucketAlreadyExists
		var bucketAlreadyOwnedByYou *types.BucketAlreadyOwnedByYou
		if errors.As(err, &bucketAlreadyExists) || errors.As(err, &bucketAlreadyOwnedByYou) {
			s.logger.Info("Bucket already exists (race condition), continuing",
				zap.String("bucket", bucketName),
			)
			return nil
		}
		
		s.logger.Error("Failed to create tenant bucket",
			zap.String("bucket", bucketName),
			zap.Error(err),
		)
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	s.logger.Info("Tenant bucket created successfully",
		zap.String("bucket", bucketName),
	)

	return nil
}

// extractTenantSuffix extracts the numeric suffix from tenant ID
// tenant_1756161631 -> 1756161631
func extractTenantSuffix(tenantID string) string {
	if strings.HasPrefix(tenantID, "tenant_") {
		return tenantID[7:] // Remove "tenant_" prefix
	}
	// If not in expected format, use the whole string (fallback)
	return tenantID
}

// buildTenantStorageKey builds the storage key for tenant-scoped storage
// spaces/{space_type}/notebooks/{notebook_id}/documents/{document_id}/{original_filename}
func buildTenantStorageKey(spaceType, notebookID, documentID, originalFilename string) string {
	return fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
		spaceType, notebookID, documentID, originalFilename)
}
