# Comprehensive Test Plan for Aether-BE Document Processing Pipeline

## Overview

This test plan provides comprehensive coverage for the aether-be document processing pipeline, including upload, text extraction, chunking strategies, ML insights, and storage verification in both MinIO and DeepLake. The plan is based on proven CI/CD patterns from the AudiModal project and ensures robust testing from development through production deployment.

## 🎯 Test Objectives

### Primary Goals
1. **Document Processing Pipeline Validation**: Test complete workflow from upload to storage
2. **Multi-Format Support**: Validate all supported document formats with initial PDF focus
3. **Chunking Strategy Testing**: Comprehensive testing of all chunking strategies
4. **Storage Integration**: Verify MinIO file storage and DeepLake vector storage
5. **ML Pipeline Validation**: Test text extraction, embeddings, and insights
6. **CI/CD Pipeline Reliability**: Ensure robust automated testing and deployment

### Success Criteria
- **Upload Success Rate**: >99% for all supported formats
- **Processing Completion**: >95% within 30 seconds
- **Storage Verification**: 100% file integrity in MinIO and DeepLake
- **Chunking Accuracy**: Strategy recommendations >90% accuracy
- **API Response Times**: <2s for upload, <30s for processing
- **CI/CD Success Rate**: >99% build and deployment success

## 📋 Test Scope

### Document Formats (Priority Order)
1. **PDF (Primary Focus)**
   - Simple text PDFs
   - Multi-page PDFs with images
   - PDFs with tables and complex layouts
   - Password-protected PDFs
   - Scanned PDFs (OCR testing)

2. **Microsoft Office Formats**
   - Word documents (.docx)
   - Excel spreadsheets (.xlsx)
   - PowerPoint presentations (.pptx)

3. **Text Formats**
   - Plain text (.txt)
   - CSV files (.csv)
   - JSON documents (.json)
   - XML documents (.xml)

4. **Media Formats**
   - Images (.jpg, .png)
   - Audio files (.mp3)
   - Video files (.mp4)

### Chunking Strategies
- **Semantic**: Natural language documents, articles
- **Fixed**: Consistent chunk sizes with overlap
- **Adaptive**: Mixed content types, JSON documents
- **Row-based**: Structured data (CSV, tables)

### API Endpoints
- `POST /api/v1/documents/upload` - Multipart file upload
- `POST /api/v1/documents/upload-base64` - Base64 encoded upload
- `GET /api/v1/files/:file_id/chunks` - Retrieve file chunks
- `GET /api/v1/files/:file_id/chunks/:chunk_id` - Get specific chunk
- `POST /api/v1/files/:file_id/reprocess` - Reprocess with different strategy
- `POST /api/v1/chunks/search` - Search chunks
- `GET /api/v1/strategies` - Available chunking strategies
- `POST /api/v1/strategies/recommend` - Strategy recommendations

## 🏗️ Test Architecture

### Test Environment Structure
```
tests/
├── integration/           # Integration test suites
│   ├── document_processing_test.go
│   ├── chunk_processing_test.go
│   ├── storage_integration_test.go
│   ├── api_endpoints_test.go
│   └── ml_pipeline_test.go
├── fixtures/             # Test documents and data
│   ├── documents/        # Sample files for each format
│   ├── expected_results/ # Expected processing outcomes
│   └── mock_responses/   # Mock API responses
├── utils/               # Test utilities and helpers
│   ├── api_client.go    # Aether-BE API client
│   ├── auth_helper.go   # Authentication utilities
│   ├── storage_verifier.go # MinIO/DeepLake verification
│   └── test_helpers.go  # Common test utilities
├── config/              # Test configuration
│   ├── test_config.yaml # Test environment config
│   └── environments/    # Environment-specific configs
└── performance/         # Performance and load tests
    ├── load_test.js     # k6 load testing script
    └── benchmark_test.go # Go benchmark tests
```

### CI/CD Pipeline Structure
```
.github/workflows/
├── test.yml            # Main test pipeline (unit, integration)
├── ci-cd.yml          # Complete CI/CD with deployment
├── security.yml       # Security scanning workflow
└── performance.yml    # Performance testing workflow
```

## 🧪 Test Implementation Strategy

### Phase 1: Foundation Setup
1. **GitHub Actions Infrastructure**
   - Multi-job pipeline with service dependencies
   - Matrix testing across Go versions (1.21, 1.22, 1.23)
   - Service containers: Neo4j, Redis, Mock AudiModal

2. **Test Environment Configuration**
   - Docker Compose test environment
   - Test database setup and migrations
   - Mock service configuration

### Phase 2: Core Document Processing Tests

#### A. Basic PDF Processing (Initial Focus)
```go
func TestPDFProcessingPipeline(t *testing.T) {
    testCases := []struct {
        name          string
        pdfFile       string
        chunkStrategy string
        expectedChunks int
        expectedQuality float64
    }{
        {
            name:           "Simple text PDF with semantic chunking",
            pdfFile:        "fixtures/documents/simple_text.pdf",
            chunkStrategy:  "semantic",
            expectedChunks: 5,
            expectedQuality: 0.8,
        },
        // Additional test cases...
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // 1. Upload PDF document
            // 2. Monitor processing status
            // 3. Verify chunks created with specified strategy
            // 4. Validate chunk quality metrics
            // 5. Verify storage in MinIO and Neo4j
            // 6. Test chunk retrieval and search
        })
    }
}
```

#### B. Multi-Format Testing
```go
func TestMultiFormatProcessing(t *testing.T) {
    formats := []struct {
        name        string
        file        string
        mimeType    string
        strategy    string
    }{
        {"Word Document", "sample.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "semantic"},
        {"Excel Spreadsheet", "sample.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "row_based"},
        {"PowerPoint", "sample.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "semantic"},
        {"Text File", "sample.txt", "text/plain", "fixed"},
        {"CSV File", "sample.csv", "text/csv", "row_based"},
        {"JSON Document", "sample.json", "application/json", "adaptive"},
    }
    
    for _, format := range formats {
        t.Run(format.name, func(t *testing.T) {
            // Test format-specific processing logic
        })
    }
}
```

#### C. Chunking Strategy Validation
```go
func TestChunkingStrategies(t *testing.T) {
    strategies := []string{"semantic", "fixed", "adaptive", "row_based"}
    
    for _, strategy := range strategies {
        t.Run(fmt.Sprintf("Strategy_%s", strategy), func(t *testing.T) {
            // 1. Get strategy recommendation for test document
            // 2. Process document with specified strategy
            // 3. Validate chunk boundaries and quality
            // 4. Verify chunk relationships in Neo4j
            // 5. Test chunk search and retrieval
        })
    }
}
```

### Phase 3: Storage and ML Pipeline Testing

#### A. Storage Integration Tests
```go
func TestStorageIntegration(t *testing.T) {
    t.Run("MinIO File Storage", func(t *testing.T) {
        // 1. Upload document and verify file stored in MinIO
        // 2. Validate file integrity and metadata
        // 3. Test file retrieval and download URLs
        // 4. Verify proper bucket organization
    })
    
    t.Run("DeepLake Vector Storage", func(t *testing.T) {
        // 1. Process document and generate embeddings
        // 2. Verify vectors stored in DeepLake
        // 3. Test vector similarity search
        // 4. Validate embedding metadata
    })
}
```

#### B. ML Insights Validation
```go
func TestMLInsights(t *testing.T) {
    t.Run("Text Extraction Quality", func(t *testing.T) {
        // Test extraction accuracy for different formats
        // Validate OCR quality for scanned documents
        // Test table extraction from PDFs
    })
    
    t.Run("Embedding Generation", func(t *testing.T) {
        // Test vector generation quality
        // Validate embedding dimensions
        // Test similarity search accuracy
    })
    
    t.Run("Content Analysis", func(t *testing.T) {
        // Test content categorization
        // Validate language detection
        // Test entity recognition (if available)
    })
}
```

### Phase 4: Advanced Scenarios and Error Handling

#### A. Error Handling Tests
```go
func TestErrorHandling(t *testing.T) {
    t.Run("Invalid File Formats", func(t *testing.T) {
        // Test unsupported file types
        // Test corrupted files
        // Test empty files
    })
    
    t.Run("Processing Failures", func(t *testing.T) {
        // Test timeout scenarios
        // Test storage failures
        // Test service unavailability
    })
    
    t.Run("Security Validation", func(t *testing.T) {
        // Test tenant isolation
        // Test unauthorized access
        // Test malicious file uploads
    })
}
```

#### B. Performance and Load Testing
```go
func TestPerformance(t *testing.T) {
    t.Run("Large File Processing", func(t *testing.T) {
        // Test files up to 10MB limit
        // Measure processing times
        // Validate memory usage
    })
    
    t.Run("Concurrent Uploads", func(t *testing.T) {
        // Test multiple simultaneous uploads
        // Validate queue processing
        // Test system stability under load
    })
}
```

## 🔧 CI/CD Pipeline Configuration

### Main Test Workflow (.github/workflows/test.yml)
```yaml
name: Tests
on:
  push:
    branches: [ main, develop ]
  pull_request:
    branches: [ main, develop ]

jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go-version: [1.21, 1.22, 1.23]
    
    services:
      neo4j:
        image: neo4j:5.15-community
        env:
          NEO4J_AUTH: neo4j/password
        ports:
          - 7687:7687
          - 7474:7474
      
      redis:
        image: redis:7
        ports:
          - 6379:6379
      
      minio:
        image: minio/minio:latest
        env:
          MINIO_ROOT_USER: minioadmin
          MINIO_ROOT_PASSWORD: minioadmin123
        ports:
          - 9000:9000
        options: --health-cmd "curl -f http://localhost:9000/minio/health/live" --health-interval 30s
      
      audimodal-mock:
        image: wiremock/wiremock:latest
        ports:
          - 8084:8080
        options: --health-cmd "curl -f http://localhost:8080/__admin/health"
    
    steps:
    - name: Checkout code
      uses: actions/checkout@v4
    
    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: ${{ matrix.go-version }}
    
    - name: Cache Go modules
      uses: actions/cache@v3
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    
    - name: Install dependencies
      run: go mod download
    
    - name: Run unit tests
      run: make test-unit
    
    - name: Run integration tests
      run: make test-integration
      env:
        NEO4J_URI: bolt://localhost:7687
        NEO4J_USERNAME: neo4j
        NEO4J_PASSWORD: password
        REDIS_ADDR: localhost:6379
        S3_ENDPOINT: http://localhost:9000
        AUDIMODAL_API_URL: http://localhost:8084
    
    - name: Run document processing tests
      run: make test-document-processing
    
    - name: Generate coverage report
      run: |
        go test -coverprofile=coverage.out ./...
        go tool cover -html=coverage.out -o coverage.html
    
    - name: Upload coverage
      uses: codecov/codecov-action@v3
      with:
        file: ./coverage.out
```

### Complete CI/CD Pipeline (.github/workflows/ci-cd.yml)
Following AudiModal's comprehensive pipeline with:
- Security scanning (Gosec, Trivy, govulncheck)
- Docker image building and pushing
- Multi-environment deployment
- Performance testing with k6
- Health checks and validation

## 📊 Test Metrics and Reporting

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

### Quality Gates
- All tests must pass before merge
- Security scans must complete without critical issues
- Performance benchmarks must not regress >10%
- Code coverage must maintain >90%

## 🚀 Implementation Roadmap

### Week 1: Foundation
- [x] Create test plan documentation
- [ ] Set up GitHub Actions workflows
- [ ] Create test infrastructure (Docker Compose)
- [ ] Implement basic API client and utilities

### Week 2: Core Testing
- [ ] Implement PDF processing tests
- [ ] Create chunking strategy tests
- [ ] Set up storage integration tests
- [ ] Add basic error handling tests

### Week 3: Comprehensive Coverage
- [ ] Add multi-format testing
- [ ] Implement ML pipeline tests
- [ ] Create performance tests
- [ ] Add security validation tests

### Week 4: CI/CD Integration
- [ ] Complete CI/CD pipeline setup
- [ ] Add deployment testing
- [ ] Implement monitoring and alerting
- [ ] Documentation and training

## 🔍 Monitoring and Maintenance

### Test Result Tracking
- Automated test result reporting
- Performance trend analysis
- Coverage tracking over time
- Failure rate monitoring

### Continuous Improvement
- Regular test suite reviews
- Performance benchmark updates
- New format support testing
- Security test updates

This comprehensive test plan ensures robust validation of the entire aether-be document processing pipeline while leveraging proven CI/CD patterns for reliable automated testing and deployment.