# Aether AI Platform - Backend

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Pending-yellow.svg)](https://github.com/Tributary-ai-services/aether-be/actions)

The backend service for the Aether AI Platform - an AI-powered document processing and analysis platform designed to help users organize, process, and gain insights from their documents through advanced AI capabilities.

## 🚀 Overview

Aether is a comprehensive AI platform that enables users to:
- **Process Documents**: Upload and process documents in multiple formats using AudiModal API
- **Organize Content**: Create hierarchical notebook structures with advanced permissions
- **Search & Discover**: Leverage graph-based search with full-text and semantic capabilities
- **Collaborate**: Share notebooks and collaborate with team members
- **Automate Workflows**: Set up AI-powered automation workflows
- **Generate Insights**: Extract analytics and insights from document collections

## 🏗️ Architecture

### Current State
- **Status**: Early development phase
- **Frontend**: React 19 + TypeScript + Vite (separate repository)
- **Data Storage**: Currently localStorage (temporary)
- **Authentication**: Local development setup

### Production Target
- **Runtime**: Go 1.21+
- **Database**: Neo4j (graph database) + Redis (cache/sessions)
- **Authentication**: Keycloak (OIDC/OAuth2/SAML)
- **File Storage**: AWS S3/MinIO
- **Message Queue**: Kafka
- **Monitoring**: Prometheus + OpenTelemetry
- **AI Services**: AudiModal API + DeepLake vector storage

## 🛠️ Core Services

The platform is built around these microservices:

1. **Authentication Service** - Keycloak integration with JWT validation
2. **User Management Service** - User profiles and preferences
3. **Notebook Management Service** - Hierarchical document organization
4. **Document Processing Service** - File upload and AI processing
5. **Search Service** - Graph-based search and discovery
6. **Analytics Service** - Usage tracking and insights
7. **Notification Service** - Real-time notifications via WebSocket
8. **Community Service** - Sharing and collaboration features

## 🚦 Quick Start

### Prerequisites

- Go 1.21 or higher
- Docker & Docker Compose
- Neo4j (via Docker)
- Redis (via Docker)

### Development Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/Tributary-ai-services/aether-be.git
   cd aether-be
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Start development services**
   ```bash
   docker-compose up -d
   ```

4. **Run the application**
   ```bash
   go run cmd/server/main.go
   ```

5. **Access the services**
   - API Server: http://localhost:8081
   - Neo4j Browser: http://localhost:7474
   - Redis: localhost:6379

### Development Commands

```bash
# Build the application
go build ./...

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Format code
go fmt ./...

# Run linter (requires golangci-lint)
golangci-lint run

# Tidy dependencies
go mod tidy
```

## 📊 Database Schema

The platform uses Neo4j as the primary database with a graph schema designed for:
- **Users** - User profiles synced with Keycloak
- **Notebooks** - Hierarchical document collections
- **Documents** - Files with extracted content and metadata
- **Entities** - Extracted entities (people, organizations, concepts)
- **Relationships** - Complex connections between all node types

For detailed schema information, see [docs/BACKEND-DESIGN.md](docs/BACKEND-DESIGN.md).

## 🔗 Integration Points

### External Services
- **Keycloak**: Authentication and user management
- **AudiModal API**: Document processing and AI analysis
- **DeepLake**: Vector storage for semantic search
- **LLM Router**: AI model routing and load balancing (via proxy)
- **Kafka**: Event streaming and service communication
- **AWS S3**: File storage with multipart upload support

### Production Infrastructure
The platform integrates with existing production infrastructure:
- Prometheus monitoring with custom metrics
- Kafka cluster for event streaming
- Redis for caching and session management
- PostgreSQL for specific use cases

## 🤖 LLM Router Integration

The Aether Backend provides a comprehensive proxy integration with the LLM Router service, enabling AI model access through a unified API interface.

### Features

- **Dual Authentication Modes**: Supports both user authentication and service-to-service authentication
- **Public Endpoints**: No authentication required for informational endpoints (providers, health, capabilities)
- **Authenticated Endpoints**: User token required for operational endpoints (chat completions, etc.)
- **Automatic Retry Logic**: Built-in retry mechanism with exponential backoff
- **Configurable Timeouts**: Customizable request and connection timeouts
- **Error Handling**: Standardized error responses with proper HTTP status codes

### Configuration

Configure the LLM Router proxy using environment variables:

```bash
# Basic Configuration
ROUTER_ENABLED=true                                    # Enable/disable router proxy
ROUTER_SERVICE_BASE_URL=http://llm-router:8080        # LLM Router service URL

# Authentication Configuration
ROUTER_USE_SERVICE_AUTH=false                          # Enable service-to-service auth
ROUTER_API_KEY=your-service-api-key                   # API key for service auth

# Connection Configuration
ROUTER_SERVICE_TIMEOUT=30s                            # Request timeout
ROUTER_SERVICE_MAX_RETRIES=3                          # Maximum retry attempts
ROUTER_SERVICE_CONNECT_TIMEOUT=10s                    # Connection timeout
```

### Endpoints

**Public Endpoints (No Authentication):**
- `GET /api/v1/router/providers` - List available AI providers
- `GET /api/v1/router/health` - LLM Router health status  
- `GET /api/v1/router/capabilities` - Supported features

**Authenticated Endpoints (Bearer Token Required):**
- `POST /api/v1/router/chat/completions` - Chat completions
- `POST /api/v1/router/completions` - Text completions
- `POST /api/v1/router/messages` - Message handling
- `GET /api/v1/router/providers/{name}` - Provider details

### Authentication Modes

**User Authentication (Default):**
```bash
# Default configuration - forwards user tokens
ROUTER_USE_SERVICE_AUTH=false
```

**Service-to-Service Authentication:**
```bash
# Service authentication - uses API keys
ROUTER_USE_SERVICE_AUTH=true
ROUTER_API_KEY=your-api-key
```

For detailed API documentation, see [API_DOCUMENTATION.md](API_DOCUMENTATION.md#llm-router-proxy).

## 📈 Current Status

This project is in **early development phase**. The repository currently contains:
- ✅ Comprehensive design documentation
- ✅ Architecture specifications
- ✅ Database schema design
- 🚧 Go project structure (in progress)
- 🚧 Core service implementations (planned)

See [ROADMAP.md](ROADMAP.md) for detailed development phases and milestones.

## 🤝 Contributing

We welcome contributions! Please see [DEVELOPER.md](DEVELOPER.md) for:
- Development environment setup
- Coding standards and conventions
- Testing requirements
- Contribution workflow

## 📝 Documentation

- [Backend Design Document](docs/BACKEND-DESIGN.md) - Comprehensive architecture and implementation details
- [Development Roadmap](ROADMAP.md) - Development phases and milestones
- [Developer Guide](DEVELOPER.md) - Setup and contribution guidelines

## 🔒 Security & Compliance

The platform is designed with security and compliance in mind:
- GDPR compliance for data governance
- Comprehensive audit logging
- Role-based access control (RBAC)
- Data encryption in transit and at rest
- Regular security assessments

## 📞 Support

- **Issues**: [GitHub Issues](https://github.com/Tributary-ai-services/aether-be/issues)
- **Documentation**: [docs/](docs/)
- **Design Document**: [docs/BACKEND-DESIGN.md](docs/BACKEND-DESIGN.md)

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

**Note**: This is the backend repository. The frontend React application is maintained in a separate repository.
