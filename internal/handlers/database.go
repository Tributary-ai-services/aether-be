package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DatabaseHandler handles database connection HTTP requests
type DatabaseHandler struct {
	databaseService *services.DatabaseService
	userService     *services.UserService
	logger          *logger.Logger
}

// NewDatabaseHandler creates a new database handler
func NewDatabaseHandler(
	databaseService *services.DatabaseService,
	userService *services.UserService,
	log *logger.Logger,
) *DatabaseHandler {
	return &DatabaseHandler{
		databaseService: databaseService,
		userService:     userService,
		logger:          log.WithService("database_handler"),
	}
}

// CreateDatabase creates a new database connection
// @Summary Create database connection
// @Description Create a new database connection configuration
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param database body models.DatabaseCreateRequest true "Database connection data"
// @Success 201 {object} models.DatabaseResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases [post]
func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	// Get user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", map[string]any{
			"hint": "Set X-Space-ID header with your space ID",
		}))
		return
	}

	tenantID := spaceContext.TenantID
	spaceIDStr := spaceContext.SpaceID

	var req models.DatabaseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	h.logger.Info("Creating database connection",
		zap.String("user_id", userID),
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceIDStr),
		zap.String("name", req.Name),
		zap.String("type", string(req.Type)),
	)

	db, err := h.databaseService.CreateDatabase(c.Request.Context(), userID, tenantID, spaceIDStr, req)
	if err != nil {
		h.logger.Error("Failed to create database",
			zap.String("user_id", userID),
			zap.Error(err),
		)
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Database created successfully",
		zap.String("database_id", db.ID),
		zap.String("name", db.Name),
	)

	c.JSON(http.StatusCreated, db.ToResponse())
}

// GetDatabase gets a database connection by ID
// @Summary Get database connection
// @Description Get a database connection by ID
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Success 200 {object} models.DatabaseResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id} [get]
func (h *DatabaseHandler) GetDatabase(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	db, err := h.databaseService.GetDatabase(c.Request.Context(), databaseID, spaceContext.TenantID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, db.ToResponse())
}

// ListDatabases lists all databases for the tenant/space
// @Summary List database connections
// @Description List all database connections for the current tenant/space
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Success 200 {object} models.DatabaseListResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases [get]
func (h *DatabaseHandler) ListDatabases(c *gin.Context) {
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	tenantID := spaceContext.TenantID
	spaceIDStr := spaceContext.SpaceID

	// Parse pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	result, err := h.databaseService.ListDatabases(c.Request.Context(), tenantID, spaceIDStr, page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateDatabase updates a database connection
// @Summary Update database connection
// @Description Update a database connection configuration
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Param database body models.DatabaseUpdateRequest true "Database update data"
// @Success 200 {object} models.DatabaseResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id} [put]
func (h *DatabaseHandler) UpdateDatabase(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.DatabaseUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	db, err := h.databaseService.UpdateDatabase(c.Request.Context(), databaseID, spaceContext.TenantID, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, db.ToResponse())
}

// DeleteDatabase deletes a database connection
// @Summary Delete database connection
// @Description Delete a database connection configuration
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Success 204 "No Content"
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id} [delete]
func (h *DatabaseHandler) DeleteDatabase(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	if err := h.databaseService.DeleteDatabase(c.Request.Context(), databaseID, spaceContext.TenantID); err != nil {
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// TestConnection tests a database connection
// @Summary Test database connection
// @Description Test connectivity to a database
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id}/test [post]
func (h *DatabaseHandler) TestConnection(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	h.logger.Info("Testing database connection",
		zap.String("database_id", databaseID),
	)

	if err := h.databaseService.TestConnection(c.Request.Context(), databaseID, spaceContext.TenantID); err != nil {
		h.logger.Error("Database connection test failed",
			zap.String("database_id", databaseID),
			zap.Error(err),
		)
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, map[string]string{
		"status":  "connected",
		"message": "Connection successful",
	})
}

// ExecuteQuery executes a SQL query on a database
// @Summary Execute SQL query
// @Description Execute a SQL query on a database connection
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Param query body models.QueryRequest true "Query data"
// @Success 200 {object} models.QueryResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id}/query [post]
func (h *DatabaseHandler) ExecuteQuery(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req struct {
		Query      string `json:"query" binding:"required"`
		Parameters []any  `json:"parameters,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	h.logger.Info("Executing query",
		zap.String("database_id", databaseID),
		zap.String("query_preview", truncateForLog(req.Query, 50)),
	)

	result, err := h.databaseService.ExecuteQuery(c.Request.Context(), databaseID, spaceContext.TenantID, req.Query, req.Parameters)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetSchema retrieves schema information for a database
// @Summary Get database schema
// @Description Get schema information (databases, schemas, tables) for a database
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Param type query string false "Schema type: database, schema, table" default(table)
// @Success 200 {object} models.SchemaResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id}/schema [get]
func (h *DatabaseHandler) GetSchema(c *gin.Context) {
	databaseID := c.Param("id")
	schemaType := c.DefaultQuery("type", "table")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	result, err := h.databaseService.GetSchema(c.Request.Context(), databaseID, spaceContext.TenantID, schemaType)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTables retrieves table list for a database
// @Summary Get database tables
// @Description Get list of tables in a database
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Success 200 {array} models.TableInfo
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/databases/{id}/tables [get]
func (h *DatabaseHandler) GetTables(c *gin.Context) {
	databaseID := c.Param("id")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	tables, err := h.databaseService.GetTables(c.Request.Context(), databaseID, spaceContext.TenantID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, map[string]any{
		"tables": tables,
	})
}

// GetTableColumns retrieves column information for a table
// @Summary Get table columns
// @Description Get column information for a specific table
// @Tags databases
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Database ID"
// @Param table path string true "Table name"
// @Success 200 {array} models.ColumnInfo
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Param schema query string false "Schema name (default: public)"
// @Router /api/v1/databases/{id}/tables/{table}/columns [get]
func (h *DatabaseHandler) GetTableColumns(c *gin.Context) {
	databaseID := c.Param("id")
	tableName := c.Param("table")
	schemaName := c.DefaultQuery("schema", "")

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	// If schema is provided, prepend it to table name for the service
	fullTableName := tableName
	if schemaName != "" {
		fullTableName = schemaName + "." + tableName
	}

	columns, err := h.databaseService.GetTableColumns(c.Request.Context(), databaseID, spaceContext.TenantID, fullTableName)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, map[string]any{
		"table":   tableName,
		"schema":  schemaName,
		"columns": columns,
	})
}

// truncateForLog truncates a string for logging
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
