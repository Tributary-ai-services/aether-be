// Migration: Add Space constraints and indexes
// Purpose: Prepare database for Space relationships (OWNS, BELONGS_TO, MEMBER_OF)
// Run this BEFORE creating new relationships

// =============================================================================
// 1. SPACE NODE CONSTRAINTS
// =============================================================================

// Unique constraint on Space.id - ensures no duplicate Space IDs
CREATE CONSTRAINT space_id_unique IF NOT EXISTS FOR (s:Space) REQUIRE s.id IS UNIQUE;

// Unique constraint on Space.tenant_id - ensures cross-service isolation
CREATE CONSTRAINT space_tenant_id_unique IF NOT EXISTS FOR (s:Space) REQUIRE s.tenant_id IS UNIQUE;

// =============================================================================
// 2. SPACE NODE INDEXES
// =============================================================================

// Index on owner_id for finding spaces owned by a user/org
CREATE INDEX space_owner_id_idx IF NOT EXISTS FOR (s:Space) ON (s.owner_id);

// Index on type for filtering personal vs organization spaces
CREATE INDEX space_type_idx IF NOT EXISTS FOR (s:Space) ON (s.type);

// Index on status for filtering active/suspended/deleted spaces
CREATE INDEX space_status_idx IF NOT EXISTS FOR (s:Space) ON (s.status);

// Index on owner_type for filtering user vs organization owned spaces
CREATE INDEX space_owner_type_idx IF NOT EXISTS FOR (s:Space) ON (s.owner_type);

// Composite index for common queries (owner + type)
CREATE INDEX space_owner_type_composite_idx IF NOT EXISTS FOR (s:Space) ON (s.owner_id, s.type);

// Composite index for active spaces by type
CREATE INDEX space_status_type_composite_idx IF NOT EXISTS FOR (s:Space) ON (s.status, s.type);

// Full-text search index for Space name and description
CREATE FULLTEXT INDEX space_name_description_fulltext IF NOT EXISTS FOR (s:Space) ON EACH [s.name, s.description];

// =============================================================================
// 3. USER NODE INDEXES (for Space relationships)
// =============================================================================

// Index on personal_space_id for quick lookup of user's personal space
CREATE INDEX user_personal_space_id_idx IF NOT EXISTS FOR (u:User) ON (u.personal_space_id);

// Ensure User.id has an index (may already exist)
CREATE INDEX user_id_idx IF NOT EXISTS FOR (u:User) ON (u.id);

// =============================================================================
// 4. NOTEBOOK NODE INDEXES (for BELONGS_TO relationship)
// =============================================================================

// Index on space_id for finding notebooks in a space
CREATE INDEX notebook_space_id_idx IF NOT EXISTS FOR (n:Notebook) ON (n.space_id);

// Composite index for notebooks by space and status
CREATE INDEX notebook_space_status_composite_idx IF NOT EXISTS FOR (n:Notebook) ON (n.space_id, n.status);

// =============================================================================
// 5. VERIFICATION QUERIES
// =============================================================================

// Verify Space constraints exist
SHOW CONSTRAINTS YIELD name, type, entityType, properties
WHERE name CONTAINS 'space';

// Verify Space indexes exist
SHOW INDEXES YIELD name, type, entityType, properties
WHERE name CONTAINS 'space' OR name CONTAINS 'user_personal' OR name CONTAINS 'notebook_space';

// Count existing Space nodes (should be run after migration)
MATCH (s:Space)
RETURN
    s.type as space_type,
    s.status as status,
    count(s) as count
ORDER BY space_type, status;

// Count Users with personal spaces
MATCH (u:User)
RETURN
    count(u) as total_users,
    count(CASE WHEN u.personal_space_id IS NOT NULL THEN 1 END) as users_with_personal_space;
