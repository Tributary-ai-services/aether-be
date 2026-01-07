# TODO: Aether Backend

## ✅ Completed Items

### Database & Neo4j (DONE)
- [x] **Fixed Neo4j Map{} error in notebook creation** - compliance_settings now serialized to JSON
  - File: `internal/services/notebook.go` (lines 88-99, 220-228)
  - Solution: JSON serialization implemented for `ComplianceSettings`
  - Status: ✅ Working correctly - notebooks can be created

### Multi-Tenancy (DONE)
- [x] **Implemented proper tenant ID passing throughout AudiModal integration**
  - Files: `internal/services/audimodal.go`, `internal/handlers/*.go`
  - All 7 AudiModal API methods now accept and pass tenantID properly
  - Tenant ID extracted from Space context and passed through entire stack
  - Status: ✅ Merged into init branch

## 🔧 Active Development

### High Priority (Current Sprint)
- [ ] **Test end-to-end document upload and processing flow**
  - Verify notebook creation works in production
  - Test document upload with real files
  - Validate AudiModal integration with proper tenant IDs
  - Ensure chunk extraction and storage works correctly

- [ ] **Add comprehensive error handling for document processing failures**
  - Better error messages for different failure types
  - Retry logic for transient failures
  - Cleanup on processing failures

### Medium Priority (Next Sprint)
- [ ] **Add integration tests for notebook and document operations**
  - File: `tests/integration/notebook_test.go` (new)
  - Test notebook creation with various data types
  - Test document processing end-to-end
  - Test error scenarios and edge cases

- [ ] **Implement proper monitoring and alerting for processing jobs**
  - Add metrics for job success/failure rates
  - Alert on high failure rates
  - Track processing time and throughput

- [ ] **Add validation layer for Neo4j property types**
  - File: `internal/database/neo4j.go`
  - Ensure only primitive types are stored as properties
  - Provide clear error messages for validation failures

### Low Priority (Backlog)
- [ ] **Consider storing complex objects as separate Neo4j nodes**
  - Files: `internal/models/`, `internal/services/`
  - Architectural review for complex object storage strategy
  - Design: Create ComplianceSettings nodes with relationships to Notebooks

- [ ] **Review all other endpoints for similar complex object issues**
  - Files: All service files in `internal/services/`
  - Audit all Neo4j queries for proper type handling
  - Focus: Document, user, and other entity creation endpoints

- [ ] **Update API documentation**
  - Files: API documentation, OpenAPI specs
  - Document all field types and constraints clearly
  - Include: Valid data structures, serialization requirements

## 📝 Notes

### Recent Fixes (January 2026)
1. **Compliance Settings Serialization**: Already implemented - complex objects are properly serialized to JSON strings before Neo4j storage
2. **Tenant ID Integration**: Completed - proper multi-tenancy support via Space context

### Architecture Decisions
- Complex objects (like `ComplianceSettings`) are stored as JSON strings in Neo4j properties
- Tenant ID format: `tenant_<UUID>` internally, plain UUID for AudiModal API calls
- Space-based multi-tenancy provides proper isolation between users/organizations

### Files Requiring Changes (For Future Work)
- None currently blocking - notebook creation and document processing should work

## 🎯 Success Metrics
- ✅ Notebook creation works without 500 errors
- ✅ Complex objects are properly serialized for Neo4j storage
- ✅ Null values don't cause database errors
- ✅ Multi-tenancy properly isolated via tenant IDs
- ⏳ End-to-end document processing validated (needs testing)
