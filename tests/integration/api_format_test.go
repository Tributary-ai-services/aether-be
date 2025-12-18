package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/Tributary-ai-services/aether-be/tests/utils"
)

// APIFormatTestSuite tests API response formats without requiring full authentication
type APIFormatTestSuite struct {
	suite.Suite
	config    *utils.TestConfig
	apiClient *utils.APIClient
}

// SetupSuite prepares the test suite
func (suite *APIFormatTestSuite) SetupSuite() {
	suite.config = utils.SetupTestEnvironment(suite.T())
	suite.apiClient = utils.NewAPIClient(suite.config.ServerURL)
}

// TestServiceConnectivity verifies all services are reachable
func (suite *APIFormatTestSuite) TestServiceConnectivity() {
	ctx := context.Background()

	// Test Aether-BE health endpoint
	suite.Run("Aether-BE Health Check", func() {
		err := suite.apiClient.HealthCheck(ctx)
		require.NoError(suite.T(), err, "Aether-BE should be healthy")
	})

	// Test AudiModal connectivity
	suite.Run("AudiModal Health Check", func() {
		client := utils.NewAPIClient(suite.config.AudiModalURL)
		resp, err := client.MakeRequest(ctx, "GET", "/health", nil, nil)
		require.NoError(suite.T(), err, "Should connect to AudiModal")
		defer resp.Body.Close()
		
		// AudiModal returns 200 with JSON response on health endpoint
		assert.Equal(suite.T(), 200, resp.StatusCode, "AudiModal should respond with 200")
	})

	// Test DeepLake connectivity
	suite.Run("DeepLake Health Check", func() {
		client := utils.NewAPIClient(suite.config.DeepLakeURL)
		resp, err := client.MakeRequest(ctx, "GET", "/__admin/health", nil, nil)
		require.NoError(suite.T(), err, "Should connect to DeepLake")
		defer resp.Body.Close()
		
		assert.Equal(suite.T(), 200, resp.StatusCode, "DeepLake should respond with 200")
	})
}

// TestAetherBEEndpoints tests accessible endpoints on Aether-BE
func (suite *APIFormatTestSuite) TestAetherBEEndpoints() {
	ctx := context.Background()

	// Test metrics endpoint (should be accessible without auth)
	suite.Run("Metrics Endpoint", func() {
		resp, err := suite.apiClient.MakeRequest(ctx, "GET", "/metrics", nil, nil)
		require.NoError(suite.T(), err, "Should access metrics endpoint")
		defer resp.Body.Close()
		
		assert.Equal(suite.T(), 200, resp.StatusCode, "Metrics endpoint should be accessible")
	})

	// Test health endpoints
	suite.Run("Health Endpoints", func() {
		endpoints := []string{"/health", "/health/live", "/health/ready"}
		
		for _, endpoint := range endpoints {
			resp, err := suite.apiClient.MakeRequest(ctx, "GET", endpoint, nil, nil)
			require.NoError(suite.T(), err, "Should access %s", endpoint)
			defer resp.Body.Close()
			
			assert.Equal(suite.T(), 200, resp.StatusCode, "%s should return 200", endpoint)
		}
	})
}

// TestAPIErrorFormats tests that API error responses have correct format
func (suite *APIFormatTestSuite) TestAPIErrorFormats() {
	ctx := context.Background()

	// Test protected endpoint without auth - should return proper error format
	suite.Run("Unauthorized Access Error Format", func() {
		resp, err := suite.apiClient.MakeRequest(ctx, "GET", "/api/v1/users/me", nil, nil)
		require.NoError(suite.T(), err, "Should make request even if unauthorized")
		defer resp.Body.Close()
		
		// Should return 401 for unauthorized
		assert.Equal(suite.T(), 401, resp.StatusCode, "Should return 401 for unauthorized access")
		
		// Verify response content type is JSON
		contentType := resp.Header.Get("Content-Type")
		assert.Contains(suite.T(), contentType, "application/json", "Error response should be JSON")
	})

	// Test invalid endpoint - should return 404
	suite.Run("Not Found Error Format", func() {
		resp, err := suite.apiClient.MakeRequest(ctx, "GET", "/api/v1/nonexistent", nil, nil)
		require.NoError(suite.T(), err, "Should make request to nonexistent endpoint")
		defer resp.Body.Close()
		
		assert.Equal(suite.T(), 404, resp.StatusCode, "Should return 404 for nonexistent endpoint")
	})
}

// TestServiceResponseFormats validates that services return expected JSON structures
func (suite *APIFormatTestSuite) TestServiceResponseFormats() {
	ctx := context.Background()

	// Test AudiModal service response format
	suite.Run("AudiModal Response Format", func() {
		client := utils.NewAPIClient(suite.config.AudiModalURL)
		resp, err := client.MakeRequest(ctx, "GET", "/health", nil, nil)
		require.NoError(suite.T(), err, "Should connect to AudiModal")
		defer resp.Body.Close()

		// Parse JSON response
		var response map[string]interface{}
		err = utils.ParseJSONResponse(resp.Body, &response)
		require.NoError(suite.T(), err, "AudiModal response should be valid JSON")

		// Verify expected fields
		assert.Contains(suite.T(), response, "service", "Response should contain service field")
		assert.Contains(suite.T(), response, "status", "Response should contain status field")
		assert.Equal(suite.T(), "audimodal", response["service"], "Service should be audimodal")
		assert.Equal(suite.T(), "healthy", response["status"], "Status should be healthy")
	})

	// Test DeepLake service response format
	suite.Run("DeepLake Response Format", func() {
		client := utils.NewAPIClient(suite.config.DeepLakeURL)
		resp, err := client.MakeRequest(ctx, "GET", "/__admin/health", nil, nil)
		require.NoError(suite.T(), err, "Should connect to DeepLake")
		defer resp.Body.Close()

		// Parse JSON response
		var response map[string]interface{}
		err = utils.ParseJSONResponse(resp.Body, &response)
		require.NoError(suite.T(), err, "DeepLake response should be valid JSON")

		// Verify expected fields
		assert.Contains(suite.T(), response, "status", "Response should contain status field")
		assert.Equal(suite.T(), "healthy", response["status"], "Status should be healthy")
	})
}

// TestIntegrationReadiness validates that all components are ready for integration testing
func (suite *APIFormatTestSuite) TestIntegrationReadiness() {
	ctx := context.Background()

	// Test that all required services are operational
	suite.Run("All Services Operational", func() {
		services := map[string]string{
			"Aether-BE":  suite.config.ServerURL + "/health",
			"AudiModal":  suite.config.AudiModalURL + "/health",
			"DeepLake":   suite.config.DeepLakeURL + "/__admin/health",
		}

		for serviceName, endpoint := range services {
			client := utils.NewAPIClient(strings.Split(endpoint, "/")[0] + "//" + strings.Split(endpoint, "/")[2])
			path := "/" + strings.Join(strings.Split(endpoint, "/")[3:], "/")
			
			resp, err := client.MakeRequest(ctx, "GET", path, nil, nil)
			require.NoError(suite.T(), err, "%s should be accessible", serviceName)
			defer resp.Body.Close()
			
			assert.Equal(suite.T(), 200, resp.StatusCode, "%s should return 200 on health check", serviceName)
			suite.T().Logf("✅ %s is operational", serviceName)
		}
	})

	// Test that infrastructure services are accessible
	suite.Run("Infrastructure Services", func() {
		// These are basic connectivity tests for infrastructure
		infrastructureServices := []string{
			"Redis (6379)",
			"MinIO (9000)", 
			"Neo4j (7687)",
			"Keycloak (8081)",
		}

		for _, service := range infrastructureServices {
			suite.T().Logf("ℹ️  %s - Infrastructure service (not directly testable via HTTP)", service)
		}
		
		// At least verify we have connectivity info
		assert.NotEmpty(suite.T(), suite.config.RedisAddr, "Redis address should be configured")
		assert.NotEmpty(suite.T(), suite.config.MinioEndpoint, "MinIO endpoint should be configured")
		assert.NotEmpty(suite.T(), suite.config.Neo4jURI, "Neo4j URI should be configured")
	})
}

// TestAPIFormatCompatibility runs the API format compatibility test suite
func TestAPIFormatCompatibility(t *testing.T) {
	suite.Run(t, new(APIFormatTestSuite))
}