//go:build ignore
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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func main() {
	// Configuration
	endpoint := "http://tas-minio-shared:9000"
	region := "us-east-1"
	accessKey := "minioadmin"
	secretKey := "minioadmin123"
	bucketName := "aether-1756217701"

	// Create AWS config
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

	// Create S3 client
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	// Test data
	testData := []byte("Test file content")
	testKey := fmt.Sprintf("test/test-file-%d.txt", time.Now().Unix())

	// Upload test
	ctx := context.Background()
	input := &s3.PutObjectInput{
		Bucket:               aws.String(bucketName),
		Key:                  aws.String(testKey),
		Body:                 bytes.NewReader(testData),
		ContentType:          aws.String("text/plain"),
		ContentLength:        aws.Int64(int64(len(testData))),
		ServerSideEncryption: types.ServerSideEncryptionAes256,
	}

	log.Printf("Attempting to upload to bucket: %s, key: %s", bucketName, testKey)
	
	_, err = client.PutObject(ctx, input)
	if err != nil {
		log.Printf("Upload failed: %v", err)
		log.Printf("Error type: %T", err)
	} else {
		log.Printf("Upload successful!")
	}
}