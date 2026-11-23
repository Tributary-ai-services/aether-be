package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// AIQG-88 LOW-1: every SQL console route needs read in the space, on the
// dbhub path and the proxy path alike. The handler refuses before it
// reaches the database service (nil here).
func TestDatabaseConsole_NeedsReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &DatabaseHandler{}
	noRead := &models.SpaceContext{SpaceType: models.SpaceTypeOrganization, SpaceID: "org-a", TenantID: "t"}
	for name, fn := range map[string]func(*gin.Context){
		"query":   h.ExecuteQuery,
		"test":    h.TestConnection,
		"schema":  h.GetSchema,
		"tables":  h.GetTables,
		"columns": h.GetTableColumns,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"query":"SELECT 1"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "db-1"}, {Key: "table", Value: "t"}}
		c.Set(middleware.SpaceContextKey, noRead)
		fn(c)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s without read = %d, want 403", name, w.Code)
		}
	}
}
