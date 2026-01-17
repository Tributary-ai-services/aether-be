package handlers

import (
	"net/http"
	
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TestStorageHandler handles storage testing endpoints
type TestStorageHandler struct {
	logger          *zap.Logger
	storageService  StorageService
}

// NewTestStorageHandler creates a new test storage handler
func NewTestStorageHandler(logger *zap.Logger, storageService StorageService) *TestStorageHandler {
	return &TestStorageHandler{
		logger:         logger,
		storageService: storageService,
	}
}

// TestTenantBucket tests tenant bucket creation and upload
func (h *TestStorageHandler) TestTenantBucket(c *gin.Context) {
	tenantID := c.Query("tenant_id")
	if tenantID == "" {
		tenantID = "tenant_1756217701"
	}
	
	h.logger.Info("Testing tenant bucket",
		zap.String("tenant_id", tenantID),
	)
	
	// Test data
	testData := []byte("Test file content for tenant bucket")
	testKey := "test/test-file.txt"
	
	// Try to upload to tenant bucket
	result, err := h.storageService.UploadFileToTenantBucket(c.Request.Context(), tenantID, testKey, testData, "text/plain")
	if err != nil {
		h.logger.Error("Test failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
			"tenant_id": tenantID,
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result": result,
		"tenant_id": tenantID,
	})
}