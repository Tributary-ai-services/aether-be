# TODO: Aether Backend

## 🔧 Database Issues (Critical)

### High Priority (Blocking notebook creation)
- [ ] **Fix Neo4j Map{} error in notebook creation - serialize complex objects to JSON strings before storing**
  - File: `internal/services/notebook.go` (line ~86)
  - Error: `"Property values can only be of primitive types or arrays thereof. Encountered: Map{}."`
  - Action: Serialize `compliance_settings` to JSON string before Neo4j query
  - Alternative: Skip null/empty complex fields entirely in query

- [ ] **Update NotebookService.CreateNotebook to handle null compliance_settings properly**
  - File: `internal/services/notebook.go`
  - Issue: Backend hardcodes `compliance_settings` parameter even when null
  - Action: Add conditional logic to exclude null complex objects from Neo4j params
  - Query: Remove `compliance_settings: $compliance_settings` when value is null

- [ ] **Implement proper JSON serialization for complianceSettings field in notebook model**
  - Files: `internal/models/notebook.go`, `internal/services/notebook.go`
  - Action: Add JSON serialization for complex objects before database storage
  - Implementation: Use `json.Marshal()` to convert objects to strings

### Medium Priority (Stability & Validation)
- [ ] **Add validation to prevent storing complex objects as Neo4j properties**
  - File: `internal/database/neo4j.go`
  - Action: Add validation layer to ensure only primitive types are stored
  - Check: Validate all parameters before Neo4j query execution

- [ ] **Add backend validation to ensure all Neo4j properties are primitive types or arrays**
  - Files: `internal/handlers/notebook.go`, validation middleware
  - Action: Add request validation for incoming data structures
  - Validation: Reject requests with nested objects in database-bound fields

- [ ] **Add proper error handling for database constraint violations in notebook creation**
  - File: `internal/handlers/notebook.go`
  - Action: Better error messages for Neo4j constraint violations
  - UX: Return specific error codes and messages for different failure types

- [ ] **Add integration tests for notebook creation with various data structures**
  - File: `tests/integration/notebook_test.go` (new)
  - Action: Test notebook creation with different data types
  - Coverage: Valid data, invalid data, edge cases, null values

- [ ] **Review all other endpoints for similar Map{} issues with complex object storage**
  - Files: All service files in `internal/services/`
  - Action: Audit all Neo4j queries for complex object storage issues
  - Focus: Document, user, and other entity creation endpoints

### Low Priority (Architecture & Documentation)
- [ ] **Consider storing complex objects as separate Neo4j nodes with relationships instead of properties**
  - Files: `internal/models/`, `internal/services/`
  - Action: Architectural review for complex object storage strategy
  - Design: Create ComplianceSettings nodes with relationships to Notebooks

- [ ] **Update API documentation to clarify expected data types for all endpoints**
  - Files: API documentation, OpenAPI specs
  - Action: Document all field types and constraints clearly
  - Include: Valid data structures, serialization requirements

## Current Critical Error
```
Neo4jError: Neo.ClientError.Statement.TypeError 
(Property values can only be of primitive types or arrays thereof. Encountered: Map{}.)
```

## Root Cause Analysis
1. **Frontend sends**: `complianceSettings` as complex object or null
2. **Backend tries**: To store complex object directly in Neo4j property
3. **Neo4j rejects**: Complex objects (maps) as property values
4. **Result**: 500 Internal Server Error, notebook creation fails

## Implementation Priority
1. **Immediate fix**: Serialize `compliance_settings` to JSON string
2. **Validation**: Add type checking before Neo4j operations  
3. **Testing**: Add integration tests for various data structures
4. **Architecture**: Consider separate nodes for complex objects

## Files Requiring Changes
- `internal/services/notebook.go` - Main notebook creation logic
- `internal/handlers/notebook.go` - Request handling and error responses
- `internal/models/notebook.go` - Data model definitions
- `internal/database/neo4j.go` - Database query utilities

## Neo4j Query Location
File: `internal/services/notebook.go` (~line 86)
```cypher
CREATE (n:Notebook {
    id: $id,
    name: $name,
    description: $description,
    visibility: $visibility,
    status: $status,
    owner_id: $owner_id,
    compliance_settings: $compliance_settings,  // <- This line causes the error
    document_count: $document_count,
    total_size_bytes: $total_size_bytes,
    tags: $tags,
    search_text: $search_text,
    created_at: datetime($created_at),
    updated_at: datetime($updated_at)
})
RETURN n
```

## Quick Fix Options
**Option A**: Serialize to JSON
```go
if complianceSettings != nil {
    settingsJSON, _ := json.Marshal(complianceSettings)
    params["compliance_settings"] = string(settingsJSON)
} else {
    params["compliance_settings"] = "{}"
}
```

**Option B**: Skip null fields
```go
if complianceSettings != nil {
    // Only add to query if not null
    params["compliance_settings"] = complianceSettings
}
// Modify query to conditionally include field
```

## Success Criteria
✅ Notebook creation works without 500 errors
✅ Complex objects are properly serialized for Neo4j storage
✅ Null values don't cause database errors
✅ All endpoints handle complex object storage consistently