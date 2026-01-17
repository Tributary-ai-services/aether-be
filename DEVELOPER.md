# Developer Guide - Aether AI Platform Backend

This guide provides comprehensive instructions for setting up the development environment, understanding the codebase, and contributing to the Aether AI Platform backend.

## 📋 Table of Contents

- [Prerequisites](#prerequisites)
- [Development Environment Setup](#development-environment-setup)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Testing](#testing)
- [Code Standards](#code-standards)
- [Debugging](#debugging)
- [Deployment](#deployment)
- [Contributing](#contributing)
- [Troubleshooting](#troubleshooting)

## 🔧 Prerequisites

### Required Software

- **Go 1.21+** - [Download](https://golang.org/dl/)
- **Docker & Docker Compose** - [Docker Desktop](https://www.docker.com/products/docker-desktop)
- **Git** - Version control
- **Make** - Build automation (usually pre-installed on macOS/Linux)

### Recommended Tools

- **VSCode** with Go extension or **GoLand**
- **Postman** or **curl** for API testing
- **kubectl** for Kubernetes development
- **golangci-lint** for code linting
- **Air** for hot reloading during development

### System Requirements

- **Memory**: 8GB+ RAM (due to Neo4j, Kafka, and other services)
- **Storage**: 10GB+ available disk space
- **Network**: Internet access for downloading dependencies

## 🚀 Development Environment Setup

### 1. Clone the Repository

```bash
git clone https://github.com/Tributary-ai-services/aether-be.git
cd aether-be
```

### 2. Install Development Tools

```bash
# Install all development tools using Make
make install-tools

# Or install individually:
go install github.com/cosmtrek/air@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### 3. Install Go Dependencies

```bash
make deps
```

### 4. Environment Configuration

Create a `.env` file in the root directory:

```bash
# Copy example environment file
cp .env.example .env

# Edit the file with your preferred editor
vim .env
```

Example `.env` configuration:

```env
# Server Configuration
PORT=8081
HOST=0.0.0.0
GIN_MODE=debug

# Database Configuration
NEO4J_URI=bolt://localhost:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=password
NEO4J_DATABASE=aether

# Redis Configuration
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# Keycloak Configuration
KEYCLOAK_URL=http://localhost:8081
KEYCLOAK_REALM=aether
KEYCLOAK_CLIENT_ID=aether-backend
KEYCLOAK_CLIENT_SECRET=dev-secret

# AWS/MinIO Configuration
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=minioadmin
AWS_SECRET_ACCESS_KEY=minioadmin
S3_BUCKET=aether-storage
S3_ENDPOINT=http://localhost:9000

# Kafka Configuration
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC_PREFIX=aether

# Logging
LOG_LEVEL=debug
LOG_FORMAT=json
```

### 5. Start Development Services

```bash
# Start all services with Docker Compose
make docker-compose-up

# Wait for services to be healthy (usually 1-2 minutes)
docker-compose ps
```

### 6. Verify Service Health

```bash
# Check all services are running
make health-check-local

# Or manually check individual services:
curl http://localhost:8081/health      # API Server
curl http://localhost:7474             # Neo4j Browser
curl http://localhost:6379             # Redis (should connection refused if healthy)
curl http://localhost:8081             # Keycloak
curl http://localhost:9001             # MinIO Console
```

### 7. Run the Application

```bash
# Option 1: Run with hot reloading (recommended for development)
make dev

# Option 2: Run normally
make run

# Option 3: Run with specific configurations
go run cmd/server/main.go
```

## 🏗️ Project Structure

```
aether-be/
├── cmd/                    # Application entry points
│   └── server/
│       └── main.go        # Main server entry point
├── internal/              # Private application code
│   ├── auth/              # Authentication logic
│   ├── config/            # Configuration management
│   ├── database/          # Database connections (Neo4j, Redis)
│   ├── handlers/          # HTTP handlers (controllers)
│   ├── middleware/        # HTTP middleware
│   ├── models/            # Data models and structs
│   ├── services/          # Business logic services
│   └── validation/        # Input validation and sanitization
├── pkg/                   # Public/shared packages
│   ├── errors/            # Custom error types
│   └── utils/             # Utility functions
├── deployments/           # Kubernetes manifests
│   ├── base/              # Base configurations
│   └── overlays/          # Environment-specific configs
├── docs/                  # Documentation
├── examples/              # Code examples
├── scripts/               # Build and deployment scripts
├── monitoring/            # Monitoring configurations
├── docker-compose.yml     # Development services
├── Dockerfile            # Container image definition
├── Makefile              # Build automation
├── go.mod & go.sum       # Go module definitions
└── skaffold.yaml         # Skaffold configuration
```

### Key Directories Explained

- **`cmd/`**: Contains the main applications. Each subdirectory represents a different executable.
- **`internal/`**: Private application code that shouldn't be imported by other applications.
- **`pkg/`**: Library code that can be used by external applications.
- **`deployments/`**: Kubernetes manifests organized with Kustomize.
- **`scripts/`**: Bash scripts for deployment, validation, and utilities.

## 🔄 Development Workflow

### Daily Development

1. **Start your development session:**
   ```bash
   # Pull latest changes
   git pull origin main
   
   # Start services
   make docker-compose-up
   
   # Start the application with hot reload
   make dev
   ```

2. **Make changes to the code**

3. **Test your changes:**
   ```bash
   # Run tests
   make test
   
   # Run with coverage
   make test-coverage
   ```

4. **Code quality checks:**
   ```bash
   # Format code
   make fmt
   
   # Run linter
   make lint
   
   # Security scan
   make security
   ```

5. **Commit your changes:**
   ```bash
   # Run pre-commit checks
   make pre-commit
   
   # Commit changes
   git add .
   git commit -m "Your commit message"
   ```

### Available Make Commands

```bash
# Development
make dev                    # Run with hot reload
make run                    # Run normally
make build                  # Build the binary

# Testing
make test                   # Run all tests
make test-coverage         # Run tests with coverage
make test-unit             # Run only unit tests
make integration-test      # Run integration tests
make benchmark             # Run benchmark tests

# Code Quality
make fmt                   # Format code
make vet                   # Run go vet
make lint                  # Run golangci-lint
make security              # Security scan
make pre-commit           # All pre-commit checks

# Docker & Compose
make docker-build         # Build Docker image
make docker-compose-up    # Start all services
make docker-compose-down  # Stop all services

# Dependencies
make deps                 # Download dependencies
make deps-upgrade         # Upgrade dependencies

# Deployment
make deploy-dev           # Deploy to dev environment
make k8s-apply-dev       # Apply Kubernetes manifests

# Utilities
make clean               # Clean build artifacts
make help                # Show all available commands
```

## 🧪 Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage report
make test-coverage

# Run specific test package
go test -v ./internal/validation/...

# Run tests with race detection
go test -race ./...

# Run tests with short flag (skip long-running tests)
go test -short ./...
```

### Test Organization

- **Unit Tests**: Located alongside source code files with `_test.go` suffix
- **Integration Tests**: Use build tag `integration`
- **Benchmarks**: Functions starting with `Benchmark`

```bash
# Run only unit tests
make test-unit

# Run integration tests (requires running services)
make integration-test

# Run benchmark tests
make benchmark
```

### Writing Tests

Follow Go testing conventions:

```go
// internal/services/user_test.go
package services

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestUserService_CreateUser(t *testing.T) {
    // Setup
    service := NewUserService(mockDB)
    
    // Test cases
    tests := []struct {
        name     string
        input    CreateUserRequest
        expected User
        wantErr  bool
    }{
        {
            name: "valid user creation",
            input: CreateUserRequest{Email: "test@example.com"},
            expected: User{Email: "test@example.com"},
            wantErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := service.CreateUser(tt.input)
            
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            
            require.NoError(t, err)
            assert.Equal(t, tt.expected.Email, result.Email)
        })
    }
}
```

## 📏 Code Standards

### Go Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `gofmt` for formatting (automatically applied by `make fmt`)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions small and focused

### Project-Specific Standards

1. **Package Organization**:
   - Use `internal/` for private code
   - Use `pkg/` for reusable libraries
   - Group related functionality in packages

2. **Error Handling**:
   ```go
   // Use custom error types from pkg/errors
   if err != nil {
       return nil, errors.Wrap(err, "failed to create user")
   }
   ```

3. **Configuration**:
   - Use environment variables for configuration
   - Provide sensible defaults
   - Validate configuration on startup

4. **Logging**:
   ```go
   // Use structured logging with zap
   logger.Info("user created",
       zap.String("user_id", user.ID),
       zap.String("email", user.Email),
   )
   ```

5. **Database Operations**:
   - Use transactions for multi-step operations
   - Handle connection timeouts and retries
   - Log query performance in development

### Code Quality Tools

```bash
# Automatic formatting
make fmt

# Linting with golangci-lint
make lint

# Security scanning with gosec
make security

# Dependency vulnerability check
make audit

# Run all quality checks
make check-all
```

## 🐛 Debugging

### Local Debugging

1. **Using VS Code**:
   - Install the Go extension
   - Set breakpoints in your code
   - Use the debug configuration in `.vscode/launch.json`

2. **Using Delve (command line)**:
   ```bash
   # Install delve
   go install github.com/go-delve/delve/cmd/dlv@latest
   
   # Debug the main application
   dlv debug cmd/server/main.go
   ```

3. **Logging-based debugging**:
   ```bash
   # Set debug log level
   export LOG_LEVEL=debug
   make run
   ```

### Common Issues

1. **Neo4j Connection Issues**:
   ```bash
   # Check if Neo4j is running
   docker-compose ps neo4j
   
   # Check Neo4j logs
   docker-compose logs neo4j
   
   # Access Neo4j browser
   open http://localhost:7474
   ```

2. **Redis Connection Issues**:
   ```bash
   # Test Redis connection
   docker-compose exec redis redis-cli ping
   ```

3. **Kafka Issues**:
   ```bash
   # Check Kafka topics
   docker-compose exec kafka kafka-topics --bootstrap-server localhost:9092 --list
   ```

### Performance Profiling

```bash
# Enable profiling in development
export ENABLE_PPROF=true
make run

# Access profiling endpoints
curl http://localhost:8081/debug/pprof/
```

## 🚀 Deployment

### Development Environment

```bash
# Deploy to local Kubernetes (if available)
make k8s-apply-dev

# Or use Skaffold for continuous deployment
make skaffold-dev
```

### Environment-Specific Deployments

```bash
# Deploy to staging
make deploy-staging

# Deploy to production (requires proper credentials)
make deploy-prod

# Validate deployment
make validate-deployment
```

### Docker Images

```bash
# Build Docker image
make docker-build

# Run containerized version locally
make docker-run
```

## 🤝 Contributing

### Before Contributing

1. **Read the Documentation**:
   - [README.md](README.md) - Project overview
   - [ROADMAP.md](ROADMAP.md) - Development phases
   - [docs/BACKEND-DESIGN.md](docs/BACKEND-DESIGN.md) - Architecture details

2. **Set up Development Environment**:
   - Follow this guide completely
   - Ensure all tests pass locally
   - Verify code quality checks pass

### Contribution Process

1. **Create a Feature Branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make Your Changes**:
   - Write code following our standards
   - Add tests for new functionality
   - Update documentation if needed

3. **Test Your Changes**:
   ```bash
   # Run comprehensive tests
   make check-all
   ```

4. **Submit a Pull Request**:
   - Provide clear description of changes
   - Reference any related issues
   - Ensure CI passes

### Pull Request Guidelines

- **Title**: Use conventional commit format (`feat:`, `fix:`, `docs:`, etc.)
- **Description**: Explain what and why, not just how
- **Tests**: Include tests for new functionality
- **Documentation**: Update relevant documentation
- **Breaking Changes**: Clearly mark and explain any breaking changes

### Code Review Process

1. Automated checks must pass (CI, tests, security scans)
2. At least one maintainer review required
3. All conversations must be resolved
4. Final approval from code owner for significant changes

## 🆘 Troubleshooting

### Common Development Issues

1. **Port Already in Use**:
   ```bash
   # Find process using port 8080
   lsof -i :8081
   
   # Kill the process
   kill -9 <PID>
   ```

2. **Docker Out of Space**:
   ```bash
   # Clean up Docker resources
   docker system prune -a
   
   # Remove unused volumes
   docker volume prune
   ```

3. **Go Module Issues**:
   ```bash
   # Clean module cache
   go clean -modcache
   
   # Re-download dependencies
   go mod download
   ```

4. **Neo4j Memory Issues**:
   ```bash
   # Increase Docker memory allocation to 4GB+
   # Or modify docker-compose.yml to add memory limits
   ```

### Getting Help

1. **Check Documentation**:
   - [Backend Design Document](docs/BACKEND-DESIGN.md)
   - API documentation (when available)

2. **Check Logs**:
   ```bash
   # Application logs
   make run
   
   # Service logs
   docker-compose logs -f <service-name>
   ```

3. **Create an Issue**:
   - Use GitHub Issues for bugs and feature requests
   - Provide detailed reproduction steps
   - Include environment information

### Performance Issues

1. **Database Performance**:
   - Check Neo4j query performance in browser
   - Ensure proper indexes are created
   - Monitor connection pool usage

2. **Memory Usage**:
   ```bash
   # Monitor memory usage
   docker stats
   
   # Profile Go application
   go tool pprof http://localhost:8081/debug/pprof/heap
   ```

3. **API Performance**:
   - Use middleware logging to identify slow endpoints
   - Monitor Prometheus metrics
   - Run load tests with `hey` or similar tools

---

## 📞 Support

- **Issues**: [GitHub Issues](https://github.com/Tributary-ai-services/aether-be/issues)
- **Architecture Questions**: See [docs/BACKEND-DESIGN.md](docs/BACKEND-DESIGN.md)
- **Development Questions**: Create a GitHub discussion

---

**Happy coding! 🚀**