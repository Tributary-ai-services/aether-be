# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the backend for the Aether AI Platform, a Go-based API service designed to support AI-powered document processing and analysis. The project is currently in early development phase with a comprehensive design document (`BACKEND-DESIGN.md`) outlining the full architecture.

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