package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	// Configuration - using localhost since we're outside Docker network
	endpoint := "http://localhost:9000"
	region := "us-east-1"
	accessKey := "minioadmin"
	secretKey := "minioadmin123"
	bucketName := "aether-1756217701" // The tenant bucket that's failing
	
	// Test data - simulate a document upload
	testData := []byte("Test document content for tenant bucket upload")
	testKey := fmt.Sprintf("spaces/organization/notebooks/test-notebook/documents/test-doc/test-file-%d.txt", time.Now().Unix())
	contentType := "text/plain"

	log.Printf("Testing upload to tenant bucket: %s", bucketName)
	log.Printf("Key: %s", testKey)
	log.Printf("Data size: %d bytes", len(testData))
	log.Printf("Endpoint: %s", endpoint)

	// Create AWS config - exactly like our service
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
			}, nil
		})),
	)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create S3 client - exactly like our service
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // Required for MinIO
	})

	// First, check if bucket exists
	log.Printf("Checking if bucket exists...")
	ctx := context.Background()
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		log.Printf("Bucket check failed: %v", err)
	} else {
		log.Printf("Bucket exists and is accessible")
	}

	// Create PutObject input - without server-side encryption for MinIO
	input := &s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String(testKey),
		Body:          bytes.NewReader(testData),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(testData))),
		// ServerSideEncryption removed - MinIO doesn't have KMS configured
		Metadata: map[string]string{
			"uploaded-by": "aether-backend",
			"upload-time": time.Now().Format(time.RFC3339),
			"tenant-id":   "tenant_1756217701",
		},
	}

	log.Printf("Attempting PutObject...")
	start := time.Now()
	
	_, err = client.PutObject(ctx, input)
	duration := time.Since(start)
	
	if err != nil {
		log.Printf("Upload FAILED after %v", duration)
		log.Printf("Error: %v", err)
		log.Printf("Error type: %T", err)
		
		// Try to get more error details
		log.Printf("Full error string: %s", err.Error())
	} else {
		log.Printf("Upload SUCCESSFUL after %v", duration)
		log.Printf("File uploaded to: %s/%s", bucketName, testKey)
	}
}