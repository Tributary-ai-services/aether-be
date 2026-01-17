# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the backend for the Aether AI Platform, a Go-based API service designed to support AI-powered document processing and analysis. The project is currently in early development phase with a comprehensive design document (`BACKEND-DESIGN.md`) outlining the full architecture.

## Data Models & Schema Reference

### Service-Specific Data Models
This service's data models are comprehensively documented in the centralized data models repository:

**Location**: `../aether-shared/data-models/aether-be/`

#### Key Node Models:
- **User Node** (`user.md`) - User profiles synced with Keycloak, including preferences and metadata
- **Notebook Node** (`notebook.md`) - Hierarchical document collections with permissions
- **Document Node** (`document.md`) - Individual files with extracted content and metadata
- **Space Node** (`space.md`) - Top-level isolation boundaries for multi-tenancy

#### Key Relationship Models:
- **OWNED_BY** (`owned-by.md`) - Ownership relationships between users and resources
- **MEMBER_OF** (`member-of.md`) - User membership in spaces and teams
- **BELONGS_TO** (`belongs-to.md`) - Resource containment relationships

#### Cross-Service Integration:
- **User Onboarding Flow** (`../aether-shared/data-models/cross-service/flows/user-onboarding.md`) - Complete user registration and space setup
- **Document Upload Flow** (`../aether-shared/data-models/cross-service/flows/document-upload.md`) - Multi-service document processing pipeline
- **Platform ERD** (`../aether-shared/data-models/cross-service/diagrams/platform-erd.md`) - Complete entity relationship diagram

#### When to Reference Data Models:
1. Before making schema changes to Neo4j nodes or relationships
2. When implementing new API endpoints that interact with graph data
3. When debugging data-related issues or unexpected query behavior
4. When onboarding new developers to understand the graph structure
5. Before modifying any properties on existing nodes or relationships

**Main Documentation Hub**: `../aether-shared/data-models/README.md` - Complete navigation for all 38 data model files

## Architecture

### Current State
- **Language**: Go 1.21+
- **Status**: Early development - repository contains design documents only
- **Frontend Integration**: React 19 + TypeScript + Vite frontend (separate repo)
- **Data Persistence**: Currently localStorage (frontend), to be replaced with Neo4j

### Target Production Stack
- **Framework**: Gin/Fiber/Echo (HTTP framework TBD)
- **Primary Database**: Neo4j (graph database for complex relationships)
- **Cache/Sessions**: Redis
- **Authentication**: Keycloak (OIDC/OAuth2/SAML) via go-oidc
- **File Storage**: AWS S3/MinIO
- **Message Queue**: Kafka via Sarama/Confluent Go client
- **Monitoring**: Prometheus + OpenTelemetry Go SDK
- **Testing**: Testify + Ginkgo/Gomega
- **Documentation**: OpenAPI/Swagger via swaggo/swag

## Core Services Architecture

The system is designed around microservices:

1. **Authentication Service** - Keycloak integration with JWT validation
2. **User Management Service** - User profiles synced with Keycloak + local preferences  
3. **Notebook Management Service** - Hierarchical notebook structure with permissions
4. **Document Processing Service** - File upload, multi-format support, AudiModal API integration
5. **Search Service** - Neo4j full-text search + vector search via DeepLake
6. **Analytics Service** - User activity tracking and reporting
7. **Compliance Service** - Data governance and compliance management
8. **Notification Service** - Real-time notifications via WebSocket

## Database Design

### Neo4j Graph Schema
The system uses Neo4j as the primary database with the following key node types:
- `User` - User profiles (synced with Keycloak)
- `Notebook` - Document collections with hierarchical structure
- `Document` - Individual files with extracted content and metadata
- `Chunk` - Document fragments for AI processing
- `Entity` - Extracted entities (people, organizations, concepts)
- `ProcessingJob` - Async processing tasks
- `AuditLog` - System activity tracking

Relationships support complex queries for permissions, hierarchies, and content relationships.

## Development Commands

Since this is an early-stage Go project, standard Go commands apply:

```bash
# Build the application
go build ./...

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Format code
go fmt ./...

# Run linter (if golangci-lint is installed)
golangci-lint run

# Tidy dependencies
go mod tidy
```

## Logging Implementation

### Structured JSON Logging
The Aether backend uses structured JSON logging with the zap library, automatically collected by Loki/Alloy for centralized observability.

**Logger Package**: `internal/logger`

```go
import "github.com/Tributary-ai-services/aether-be/internal/logger"

// Initialize logger
log := logger.NewLogger(os.Getenv("LOG_LEVEL")) // INFO, DEBUG, WARN, ERROR

// Structured logging with fields
log.Info("Notebook created",
  zap.String("user_id", userID),
  zap.String("notebook_id", notebookID),
  zap.String("space_id", spaceID),
)

// Context logger for multiple related log entries
contextLogger := log.WithContext(
  zap.String("service", "notebook-service"),
  zap.String("request_id", requestID),
)
contextLogger.Info("Processing started")
contextLogger.Info("Processing completed")
```

### Frontend Logging Endpoint
The backend provides an endpoint to receive logs from the React frontend:

**Handler**: `internal/handlers/logging.go`
**Route**: `POST /api/v1/logs`

```go
// Request format
type LogBatchRequest struct {
  Logs []FrontendLogEntry `json:"logs"`
}

type FrontendLogEntry struct {
  Level      string                 `json:"level"`       // error, warn, info, debug
  Message    string                 `json:"message"`
  Timestamp  *time.Time             `json:"timestamp"`
  URL        string                 `json:"url"`
  UserAgent  string                 `json:"user_agent"`
  SessionID  string                 `json:"session_id"`
  StackTrace string                 `json:"stack_trace,omitempty"`
  Extra      map[string]interface{} `json:"extra,omitempty"`
}
```

Frontend logs are enriched with user context (user_id, tenant_id, space_id from JWT) and logged to stdout with `source="frontend"` label for Loki collection.

### Key Logging Fields
Include these fields for effective log filtering and analysis:
- `user_id` - User performing the action
- `tenant_id` - Tenant identifier for multi-tenant isolation
- `space_id` - Space identifier for space-based operations
- `request_id` - Unique request identifier for distributed tracing
- `trace_id` - OpenTelemetry trace ID (if using distributed tracing)
- `notebook_id`, `document_id` - Resource identifiers

### Viewing Logs
```bash
# Port-forward to Loki
kubectl port-forward -n tas-shared svc/loki-shared 3100:3100

# Query recent backend logs
curl 'http://localhost:3100/loki/api/v1/query?query={namespace="aether-be",container="aether-backend"}&limit=20'

# Query frontend logs
curl 'http://localhost:3100/loki/api/v1/query?query={namespace="aether-be",source="frontend"}&limit=20'
```

**Grafana Dashboard**: "TAS Applications Logs" - Pre-configured dashboard with log streams, error rates, and filtering by service

### Log Levels
- `ERROR` - Critical errors requiring immediate attention (e.g., database connection failures, panic recovery)
- `WARN` - Warning conditions that should be investigated (e.g., deprecated API usage, slow queries)
- `INFO` - General informational messages (default level, e.g., service started, user actions)
- `DEBUG` - Detailed debugging information (e.g., request/response payloads, detailed state changes)

## Key Integration Points

- **Keycloak**: Authentication and user management via go-oidc and gocloak admin client
- **Neo4j**: Primary data store using Neo4j Go Driver with connection pooling
- **AudiModal API**: Document processing service integration
- **DeepLake**: Vector storage for AI/ML operations
- **Kafka**: Event streaming for real-time features and service communication
- **AWS S3**: File storage with multipart upload support

## Project Structure (Planned)

Based on the design document, the codebase will likely follow:
- `/cmd` - Application entry points
- `/internal` - Internal packages (services, handlers, models)
- `/pkg` - Public/shared packages
- `/api` - OpenAPI/Swagger definitions
- `/migrations` - Database migration files
- `/docker` - Docker configurations
- `/docs` - Additional documentation

## Important Notes

- The project emphasizes GDPR compliance and data governance
- Real-time features use WebSocket connections with Go channels
- All services integrate with existing production infrastructure (Prometheus, Kafka, Keycloak)
- The frontend uses localStorage temporarily until this backend is implemented
- Comprehensive audit logging is built into the Neo4j schema design