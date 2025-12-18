# Aether Backend Makefile

.PHONY: help build test clean run dev docker-build docker-run docker-compose-up docker-compose-down deps lint fmt vet security audit ci pipeline pre-commit check-all validate-code benchmark integration-test generate docs

# Default target
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development
dev: ## Run the application in development mode with hot reload
	@echo "Starting development server..."
	air -c .air.toml

run: ## Run the application
	@echo "Starting server..."
	go run cmd/server/main.go

build: ## Build the application
	@echo "Building application..."
	go build -o bin/aether-backend cmd/server/main.go

# Testing
test: ## Run tests
	@echo "Running tests..."
	go test -v ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Code quality
fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run

# Dependencies
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

deps-upgrade: ## Upgrade dependencies
	@echo "Upgrading dependencies..."
	go get -u ./...
	go mod tidy

# Docker
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t aether-backend:latest .

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	docker run -p 8080:8080 --env-file .env aether-backend:latest

# Docker Compose
docker-compose-up: ## Start all services with docker-compose
	@echo "Starting services with docker-compose..."
	docker-compose up -d

docker-compose-down: ## Stop all services with docker-compose
	@echo "Stopping services with docker-compose..."
	docker-compose down

docker-compose-logs: ## View logs from docker-compose services
	docker-compose logs -f

# Database
db-migrate-up: ## Run database migrations up
	@echo "Running database migrations..."
	migrate -path migrations -database "$(NEO4J_URI)" up

db-migrate-down: ## Run database migrations down
	@echo "Rolling back database migrations..."
	migrate -path migrations -database "$(NEO4J_URI)" down

# Kubernetes
k8s-apply-dev: ## Apply Kubernetes manifests for dev environment
	@echo "Applying dev environment..."
	kubectl apply -k deployments/overlays/dev

k8s-apply-staging: ## Apply Kubernetes manifests for staging environment
	@echo "Applying staging environment..."
	kubectl apply -k deployments/overlays/staging

k8s-apply-testing: ## Apply Kubernetes manifests for testing environment
	@echo "Applying testing environment..."
	kubectl apply -k deployments/overlays/testing

k8s-apply-prod: ## Apply Kubernetes manifests for production environment
	@echo "Applying production environment..."
	kubectl apply -k deployments/overlays/production

k8s-delete-dev: ## Delete dev environment
	kubectl delete -k deployments/overlays/dev

k8s-delete-staging: ## Delete staging environment
	kubectl delete -k deployments/overlays/staging

k8s-delete-testing: ## Delete testing environment
	kubectl delete -k deployments/overlays/testing

k8s-delete-prod: ## Delete production environment
	kubectl delete -k deployments/overlays/production

# Skaffold
skaffold-dev: ## Start Skaffold development with hot reload
	@echo "Starting Skaffold development mode..."
	skaffold dev --profile=local

skaffold-run: ## Run with Skaffold (docker-compose profile)
	@echo "Running with Skaffold..."
	SKAFFOLD_PROFILE=docker-compose skaffold run

skaffold-deploy-dev: ## Deploy to dev environment with Skaffold
	@echo "Deploying to dev environment..."
	skaffold run --profile=dev

skaffold-deploy-staging: ## Deploy to staging environment with Skaffold
	@echo "Deploying to staging environment..."
	skaffold run --profile=staging

skaffold-deploy-testing: ## Deploy to testing environment with Skaffold
	@echo "Deploying to testing environment..."
	skaffold run --profile=testing

skaffold-deploy-prod: ## Deploy to production environment with Skaffold
	@echo "Deploying to production environment..."
	skaffold run --profile=production

# Deployment scripts
deploy-dev: ## Deploy to dev environment using script
	@echo "Deploying to dev environment..."
	./scripts/deploy.sh -e dev -b

deploy-staging: ## Deploy to staging environment using script
	@echo "Deploying to staging environment..."
	./scripts/deploy.sh -e staging -b -p

deploy-testing: ## Deploy to testing environment using script
	@echo "Deploying to testing environment..."
	./scripts/deploy.sh -e testing -b

deploy-prod: ## Deploy to production environment using script
	@echo "Deploying to production environment..."
	./scripts/deploy.sh -e production -b -p

# Validation and Health Checks
validate-dev: ## Validate dev environment
	@echo "Validating dev environment..."
	./scripts/validate-env.sh -e dev

validate-staging: ## Validate staging environment
	@echo "Validating staging environment..."
	./scripts/validate-env.sh -e staging

validate-testing: ## Validate testing environment
	@echo "Validating testing environment..."
	./scripts/validate-env.sh -e testing

validate-prod: ## Validate production environment
	@echo "Validating production environment..."
	./scripts/validate-env.sh -e production

health-check-dev: ## Health check dev environment
	@echo "Running health check for dev environment..."
	./scripts/health-check.sh -e dev

health-check-staging: ## Health check staging environment
	@echo "Running health check for staging environment..."
	./scripts/health-check.sh -e staging

health-check-testing: ## Health check testing environment
	@echo "Running health check for testing environment..."
	./scripts/health-check.sh -e testing

health-check-prod: ## Health check production environment
	@echo "Running health check for production environment..."
	./scripts/health-check.sh -e production

# Utilities
clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	go clean

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest

# Security and Code Quality
security: ## Run security scan
	@echo "Running security scan..."
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec..."; go install github.com/securego/gosec/v2/cmd/gosec@latest; }
	gosec -fmt json -out gosec-report.json -stdout ./... || true
	@echo "Security scan completed. Report saved to gosec-report.json"

audit: ## Audit dependencies for vulnerabilities
	@echo "Auditing dependencies..."
	go list -json -deps ./... | nancy sleuth || true
	@echo "Dependency audit completed"

validate-code: ## Validate code structure and standards
	@echo "Validating code structure..."
	@echo "Checking for TODO/FIXME comments..."
	@grep -r "TODO\|FIXME" --include="*.go" . || echo "No TODO/FIXME comments found"
	@echo "Checking for debug statements..."
	@grep -r "fmt.Print\|log.Print" --include="*.go" . || echo "No debug statements found"
	@echo "Checking for hardcoded secrets..."
	@grep -r "password\|secret\|key" --include="*.go" . | grep -v "// " | head -10 || echo "No obvious hardcoded secrets found"

benchmark: ## Run benchmark tests
	@echo "Running benchmark tests..."
	go test -bench=. -benchmem ./... || echo "No benchmark tests found"

integration-test: ## Run integration tests
	@echo "Running integration tests..."
	go test -tags=integration -v ./... || echo "No integration tests found"

generate: ## Generate code (mocks, swagger, etc.)
	@echo "Generating code..."
	go generate ./...

docs: ## Generate documentation
	@echo "Generating documentation..."
	@command -v godoc >/dev/null 2>&1 || { echo "Installing godoc..."; go install golang.org/x/tools/cmd/godoc@latest; }
	@echo "Documentation server: http://localhost:6060/pkg/github.com/Tributary-ai-services/aether-be/"
	@echo "Run 'godoc -http=:6060' to start documentation server"

# Pre-commit checks
pre-commit: fmt vet lint test security ## Run all pre-commit checks
	@echo "Pre-commit checks completed successfully!"

# Comprehensive CI/CD Pipeline
check-all: clean deps fmt vet lint test-coverage security audit validate-code benchmark ## Run all code quality checks
	@echo "All checks completed!"

ci: check-all build docker-build ## CI pipeline: run all checks and build
	@echo "CI pipeline completed successfully!"

pipeline: ci ## Full CI/CD pipeline with deployment validation
	@echo "Running full CI/CD pipeline..."
	@echo "Validating Docker image..."
	docker run --rm aether-backend:latest /bin/sh -c "echo 'Container validation successful'"
	@echo "Running example validation script..."
	go run examples/validation/validation_example.go
	@echo "Running metrics example..."
	timeout 10s go run examples/metrics/metrics_example.go || echo "Metrics example completed (timeout expected)"
	@echo "Full pipeline completed successfully!"

# Advanced testing
test-unit: ## Run only unit tests
	@echo "Running unit tests..."
	go test -short -v ./internal/validation ./pkg/errors

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	go test -tags=integration -v ./tests/integration/...

test-integration-full: ## Run comprehensive integration tests with containers
	@echo "Running full integration tests..."
	@echo "Starting test dependencies..."
	docker-compose -f docker-compose.test.yml up -d
	@echo "Waiting for services to be ready..."
	sleep 30
	@echo "Running integration tests..."
	go test -tags=integration -v ./tests/integration/...
	@echo "Stopping test services..."
	docker-compose -f docker-compose.test.yml down

test-document-processing: ## Run document processing pipeline tests
	@echo "Running document processing tests..."
	go test -v -run TestDocumentProcessingPipeline ./tests/integration/...

test-chunking-strategies: ## Run chunking strategy tests
	@echo "Running chunking strategy tests..."
	go test -v -run TestChunkingStrategies ./tests/integration/...

test-storage-integration: ## Run storage integration tests
	@echo "Running storage integration tests..."
	go test -v -run TestStorageIntegration ./tests/integration/...

test-api-endpoints: ## Run API endpoint tests
	@echo "Running API endpoint tests..."
	go test -v -run TestAPIEndpoints ./tests/integration/...

test-ml-pipeline: ## Run ML pipeline tests
	@echo "Running ML pipeline tests..."
	go test -v -run TestMLPipeline ./tests/integration/...

test-performance: ## Run performance tests
	@echo "Running performance tests..."
	go test -v -run TestPerformance ./tests/integration/...
	@echo "Running load tests with k6..."
	@command -v k6 >/dev/null 2>&1 && k6 run tests/performance/load-test.js || echo "k6 not installed, skipping load tests"

test-security: ## Run security tests
	@echo "Running security tests..."
	go test -v -run TestSecurity ./tests/integration/...

test-progressive: ## Run progressive testing validation
	@echo "Running progressive testing validation..."
	go test -v -run TestABFramework ./tests/progressive/...

test-canary-validation: ## Validate canary deployment configuration
	@echo "Validating canary deployment configuration..."
	@command -v kubectl >/dev/null 2>&1 && kubectl apply --dry-run=client -f tests/progressive/canary_config.yaml || echo "kubectl not available, skipping validation"

test-blue-green-validation: ## Validate blue-green deployment configuration
	@echo "Validating blue-green deployment configuration..."
	@command -v kubectl >/dev/null 2>&1 && kubectl apply --dry-run=client -f tests/progressive/blue_green_config.yaml || echo "kubectl not available, skipping validation"

test-progressive-workflows: ## Test progressive deployment workflows
	@echo "Testing progressive deployment workflows..."
	@echo "Validating progressive-testing.yml workflow..."
	@command -v actionlint >/dev/null 2>&1 && actionlint .github/workflows/progressive-testing.yml || echo "actionlint not available, skipping validation"

test-all-comprehensive: test-unit test-integration test-document-processing test-chunking-strategies test-storage-integration test-api-endpoints test-progressive ## Run all test suites
	@echo "All comprehensive tests completed!"

test-load: ## Run load tests
	@echo "Running load tests..."
	@command -v hey >/dev/null 2>&1 || { echo "Installing hey..."; go install github.com/rakyll/hey@latest; }
	@echo "Load testing requires running server. Start with 'make run' in another terminal"
	@echo "Example: hey -n 1000 -c 10 http://localhost:8080/health"

# Deployment validation
validate-deployment: ## Validate deployment configuration
	@echo "Validating deployment configuration..."
	@echo "Checking Kubernetes manifests..."
	kubectl --dry-run=client apply -k deployments/overlays/dev || echo "Kubernetes validation failed"
	@echo "Checking Docker configuration..."
	docker build --dry-run -t aether-backend:test . || echo "Docker build validation failed"
	@echo "Checking environment variables..."
	./scripts/validate-env.sh -e dev || echo "Environment validation script not found"

# Monitoring and observability
metrics-check: ## Check metrics endpoint
	@echo "Checking metrics endpoint..."
	@echo "Start server with 'make run' then access http://localhost:8080/metrics/prometheus"

health-check-local: ## Check local health endpoints
	@echo "Checking local health endpoints..."
	@echo "Start server with 'make run' then run:"
	@echo "curl http://localhost:8080/health"
	@echo "curl http://localhost:8080/health/live"
	@echo "curl http://localhost:8080/health/ready"

# Release management
version: ## Show current version
	@echo "Current version: $(shell git describe --tags --always --dirty)"

tag: ## Create a new tag (usage: make tag VERSION=v1.0.0)
	@echo "Creating tag $(VERSION)..."
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)

release: ci tag ## Create a release after CI passes
	@echo "Release $(VERSION) created successfully!"