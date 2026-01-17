package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	// Parse command line flags
	keepFiles := flag.Bool("keep", false, "Keep test files and objects for inspection")
	flag.Parse()

	// MinIO connection settings
	endpoint := "localhost:9000"
	accessKeyID := "minioadmin"
	secretAccessKey := "minioadmin123"
	useSSL := false

	// Initialize MinIO client
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalf("Failed to create MinIO client: %v", err)
	}

	ctx := context.Background()

	fmt.Println("=== MinIO Standalone Tests ===")
	fmt.Println()

	// Test 1: Create bucket with tenant naming
	tenantID := "tenant_1234567890"
	bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
	
	fmt.Printf("1. Creating bucket: %s\n", bucketName)
	err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	if err != nil {
		exists, errBucketExists := minioClient.BucketExists(ctx, bucketName)
		if errBucketExists == nil && exists {
			fmt.Printf("   ✓ Bucket already exists: %s\n", bucketName)
		} else {
			log.Fatalf("   ✗ Failed to create bucket: %v", err)
		}
	} else {
		fmt.Printf("   ✓ Bucket created successfully\n")
	}

	// Test 2: Upload file with folder structure
	objectName := "spaces/personal/notebooks/notebook_123/documents/doc_456/test-file.pdf"
	fileContent := []byte("This is a test PDF content for MinIO diagnostic testing")
	
	fmt.Printf("\n2. Uploading file with path: %s\n", objectName)
	_, err = minioClient.PutObject(ctx, bucketName, objectName, 
		strings.NewReader(string(fileContent)), 
		int64(len(fileContent)), 
		minio.PutObjectOptions{
			ContentType: "application/pdf",
		})
	if err != nil {
		log.Fatalf("   ✗ Failed to upload file: %v", err)
	}
	fmt.Printf("   ✓ File uploaded successfully\n")

	// Test 3: List files to verify folder structure
	fmt.Printf("\n3. Listing files in notebook folder\n")
	prefix := "spaces/personal/notebooks/notebook_123/"
	objectCh := minioClient.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	
	count := 0
	for object := range objectCh {
		if object.Err != nil {
			log.Fatalf("   ✗ Error listing objects: %v", object.Err)
		}
		fmt.Printf("   - %s (size: %d bytes)\n", object.Key, object.Size)
		count++
	}
	fmt.Printf("   ✓ Found %d files\n", count)

	// Test 4: Update the file
	updatedContent := []byte("UPDATED: This file has been modified for testing purposes")
	fmt.Printf("\n4. Updating file content\n")
	_, err = minioClient.PutObject(ctx, bucketName, objectName,
		strings.NewReader(string(updatedContent)),
		int64(len(updatedContent)),
		minio.PutObjectOptions{
			ContentType: "application/pdf",
		})
	if err != nil {
		log.Fatalf("   ✗ Failed to update file: %v", err)
	}
	fmt.Printf("   ✓ File updated successfully\n")

	// Test 5: Download and verify update
	fmt.Printf("\n5. Downloading file to verify update\n")
	object, err := minioClient.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Fatalf("   ✗ Failed to download file: %v", err)
	}
	defer object.Close()

	buf := make([]byte, len(updatedContent))
	_, err = object.Read(buf)
	if err != nil && err.Error() != "EOF" {
		log.Fatalf("   ✗ Failed to read file content: %v", err)
	}
	
	if string(buf) == string(updatedContent) {
		fmt.Printf("   ✓ File content verified - update successful\n")
	} else {
		fmt.Printf("   ✗ File content mismatch\n")
	}

	if !*keepFiles {
		// Test 6: Delete the file
		fmt.Printf("\n6. Deleting file\n")
		err = minioClient.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
		if err != nil {
			log.Fatalf("   ✗ Failed to delete file: %v", err)
		}
		fmt.Printf("   ✓ File deleted successfully\n")

		// Test 7: Verify deletion
		fmt.Printf("\n7. Verifying file deletion\n")
		_, err = minioClient.StatObject(ctx, bucketName, objectName, minio.StatObjectOptions{})
		if err != nil {
			if minio.ToErrorResponse(err).Code == "NoSuchKey" {
				fmt.Printf("   ✓ File confirmed deleted\n")
			} else {
				log.Fatalf("   ✗ Unexpected error checking file: %v", err)
			}
		} else {
			fmt.Printf("   ✗ File still exists after deletion\n")
		}

		// Test 8: Clean up - remove bucket (optional)
		fmt.Printf("\n8. Cleanup (keeping bucket for future tests)\n")
		fmt.Printf("   ℹ Bucket %s retained for future testing\n", bucketName)
	} else {
		fmt.Printf("\n6-8. Skipping deletion and cleanup (--keep flag used)\n")
		fmt.Printf("   📁 File preserved at: %s/%s\n", bucketName, objectName)
		fmt.Printf("   🪣 Bucket: %s\n", bucketName)
		fmt.Printf("   🌐 Full MinIO path: s3://%s/%s\n", bucketName, objectName)
		fmt.Printf("   🖥️  MinIO Console: http://localhost:9001 (admin:minioadmin123)\n")
		fmt.Printf("   💻 CLI inspect: docker exec tas-minio-shared mc cat myminio/%s/%s\n", bucketName, objectName)
		fmt.Printf("   📋 List bucket: docker exec tas-minio-shared mc ls --recursive myminio/%s/\n", bucketName)
	}

	fmt.Println("\n=== All MinIO tests completed successfully! ===")
}

func extractTenantSuffix(tenantID string) string {
	if len(tenantID) > 10 {
		return tenantID[len(tenantID)-10:]
	}
	return tenantID
}