# Aether-BE Test Suite

This directory contains a comprehensive test suite for the Aether-BE document processing pipeline, following proven CI/CD patterns from the AudiModal project.

## 📋 Test Structure

```
tests/
├── integration/              # Integration test suites
│   ├── document_processing_test.go   # Document processing pipeline tests
│   └── storage_integration_test.go   # MinIO and DeepLake storage tests
├── utils/                   # Test utilities and helpers
│   ├── api_client.go       # Aether-BE API client
│   ├── auth_helper.go      # Authentication utilities
│   ├── storage_verifier.go # MinIO/DeepLake verification
│   └── test_helpers.go     # Common test utilities
├── config/                 # Test configuration
│   └── test_config.yaml    # Test environment configuration
└── fixtures/               # Test documents and data
    ├── documents/          # Sample files for testing
    ├── expected_results/   # Expected processing outcomes
    └── mock_responses/     # Mock API responses
```

## 🧪 Test Categories

### Document Processing Pipeline Tests
- **PDF Processing**: Complete workflow testing for various PDF types
- **Multi-Format Support**: Word, Excel, PowerPoint, text, CSV, JSON, XML
- **Chunking Strategies**: Semantic, fixed, adaptive, row-based testing
- **Reprocessing**: Testing document reprocessing with different strategies
- **Performance Validation**: Upload, processing, and chunking performance

### Storage Integration Tests
- **MinIO File Storage**: File upload, integrity, retrieval, and metadata verification
- **DeepLake Vector Storage**: Embedding generation, storage, and similarity search
- **Cross-Storage Consistency**: Ensuring consistency between file and vector storage
- **Failure Handling**: Large files, concurrent operations, error scenarios
- **Storage Cleanup**: Verification of proper data cleanup

## 🚀 Running Tests

### Quick Commands

```bash
# Run all tests
make test-all-comprehensive

# Run specific test suites
make test-document-processing
make test-chunking-strategies
make test-storage-integration

# Run with test environment
make test-integration-full
```

### Detailed Test Commands

```bash
# Unit tests only
make test-unit

# Integration tests
make test-integration

# Document processing pipeline
make test-document-processing

# Chunking strategies
make test-chunking-strategies

# Storage integration
make test-storage-integration

# Performance tests
make test-performance

# Security tests
make test-security
```

## 🔧 Test Environment Setup

### Prerequisites
- Docker and Docker Compose
- Go 1.21+
- Access to test services (Neo4j, Redis, MinIO, mock services)

### Environment Configuration
The test suite uses `tests/config/test_config.yaml` for configuration. Key settings:

```yaml
# Service endpoints
server:
  host: "localhost"
  port: 8080

database:
  neo4j:
    uri: "bolt://localhost:7687"

storage:
  s3:
    endpoint: "http://localhost:9000"
    bucket: "aether-test-storage"

audimodal:
  base_url: "http://localhost:8084"

deeplake:
  api_url: "http://localhost:8085"
```

### Docker Compose Test Environment
Start the complete test environment:

```bash
docker-compose -f docker-compose.test.yml up -d
```

This starts:
- Neo4j (graph database)
- Redis (caching)
- MinIO (S3-compatible storage)
- WireMock (AudiModal and DeepLake mocks)
- Keycloak (authentication - optional)

## 📊 Test Coverage and Quality Gates

### Coverage Requirements
- **Unit Tests**: >90% code coverage
- **Integration Tests**: >95% API endpoint coverage
- **Document Formats**: 100% supported format testing
- **Chunking Strategies**: 100% strategy testing
- **Error Scenarios**: >80% error path coverage

### Performance Benchmarks
- **Upload Response**: <2 seconds
- **Processing Time**: <30 seconds for documents <10MB
- **Chunk Generation**: <10 seconds for text documents
- **Storage Verification**: <5 seconds
- **Search Response**: <1 second

## 🔍 CI/CD Integration

The test suite integrates with GitHub Actions workflows:

### Workflows
- **`.github/workflows/test.yml`**: Main test pipeline with matrix testing
- **`.github/workflows/ci-cd.yml`**: Complete CI/CD with deployment
- **`.github/workflows/security.yml`**: Security scanning (Gosec, CodeQL, Semgrep)
- **`.github/workflows/performance.yml`**: Performance testing with k6

### Test Matrix
- Go versions: 1.21, 1.22, 1.23
- Test categories: unit, integration, document processing, storage
- Security scans: SAST, dependency vulnerabilities, secrets detection
- Performance tests: load testing, benchmarks, profiling

## 🛠️ Test Utilities

### API Client (`utils.APIClient`)
Provides methods to interact with the Aether-BE API:
- Document upload (multipart and base64)
- Processing status monitoring
- Chunk retrieval and search
- Strategy management

### Storage Verifier (`utils.StorageVerifier`)
Verifies storage operations:
- MinIO file integrity and metadata
- DeepLake embedding verification
- Cross-storage consistency checks
- Performance validation

### Auth Helper (`utils.AuthHelper`)
Authentication utilities for testing:
- Keycloak integration
- Test user management
- JWT token handling
- Permission testing

### Test Helpers (`utils.TestHelpers`)
Common testing utilities:
- Environment setup and teardown
- Performance measurement
- Test data generation
- Validation helpers

## 🧩 Test Data and Fixtures

### Document Types
The test suite includes fixtures for:
- **PDF**: Simple text, complex layouts, scanned documents, password-protected
- **Office**: Word (.docx), Excel (.xlsx), PowerPoint (.pptx)
- **Text**: Plain text (.txt), CSV (.csv), JSON (.json), XML (.xml)
- **Media**: Images (.jpg, .png), audio (.mp3), video (.mp4)

### Mock Services
WireMock configurations for:
- **AudiModal API**: Document processing simulation
- **DeepLake API**: Vector storage and search simulation
- **Keycloak**: Authentication service simulation

## 📈 Monitoring and Reporting

### Test Results
- Detailed test output with performance metrics
- Coverage reports (HTML and console)
- Security scan results (SARIF format)
- Performance benchmarks and trends

### Artifacts
CI/CD workflows generate:
- Test coverage reports
- Security scan results
- Performance profiles
- Docker images for deployment

## 🎯 Success Criteria

Tests validate:
- ✅ Complete document processing pipeline
- ✅ All supported file formats
- ✅ All chunking strategies
- ✅ Storage integrity (MinIO + DeepLake)
- ✅ Performance benchmarks
- ✅ Security compliance
- ✅ Error handling and recovery
- ✅ Concurrent operation handling

## 📝 Contributing

When adding new tests:

1. **Follow existing patterns**: Use the established test suite structure
2. **Add appropriate fixtures**: Include test data in `tests/fixtures/`
3. **Update configuration**: Modify `test_config.yaml` if needed
4. **Include performance validation**: Measure and validate performance
5. **Add to CI/CD**: Ensure new tests run in GitHub Actions
6. **Document changes**: Update this README and test plan documentation

## 🔗 Related Documentation

- [Test Plan Documentation](../docs/TEST_PLAN.md)
- [Test Implementation TODO](../TODO_TESTPLAN.md)
- [Makefile Targets](../Makefile)
- [Docker Compose Test Environment](../docker-compose.test.yml)
- [GitHub Actions Workflows](../.github/workflows/)

This comprehensive test suite ensures robust validation of the entire aether-be document processing pipeline while leveraging proven CI/CD patterns for reliable automated testing and deployment.