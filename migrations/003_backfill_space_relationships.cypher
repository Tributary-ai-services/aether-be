// Migration: Backfill Space Relationships
// Purpose: Create OWNS and BELONGS_TO relationships for existing data
// Run this AFTER migration 002_add_space_constraints.cypher
//
// This migration:
// 1. Creates OWNS relationships between Users and their personal Spaces
// 2. Creates BELONGS_TO relationships between Notebooks and their Spaces
//
// All relationships are marked with `migrated: true` for easy rollback

// =============================================================================
// 1. BACKFILL OWNS RELATIONSHIPS
// =============================================================================

// Create OWNS relationship for existing personal spaces
// Links User -> Space based on user.personal_space_id
MATCH (u:User)
WHERE u.personal_space_id IS NOT NULL
MATCH (s:Space {id: u.personal_space_id})
WHERE NOT EXISTS { (u)-[:OWNS]->(s) }
MERGE (u)-[r:OWNS]->(s)
ON CREATE SET r.created_at = datetime(), r.migrated = true
RETURN count(r) as owns_relationships_created;

// =============================================================================
// 2. BACKFILL BELONGS_TO RELATIONSHIPS
// =============================================================================

// Create BELONGS_TO relationship for existing notebooks
// Links Notebook -> Space based on notebook.space_id
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL
MATCH (s:Space {id: n.space_id})
WHERE NOT EXISTS { (n)-[:BELONGS_TO]->(s) }
MERGE (n)-[r:BELONGS_TO]->(s)
ON CREATE SET r.created_at = datetime(), r.migrated = true
RETURN count(r) as belongs_to_relationships_created;

// =============================================================================
// 3. VERIFICATION QUERIES
// =============================================================================

// Count OWNS relationships (should match users with personal spaces)
MATCH (u:User)-[r:OWNS]->(s:Space)
RETURN
    count(r) as total_owns,
    count(CASE WHEN r.migrated = true THEN 1 END) as migrated_owns;

// Count BELONGS_TO relationships (should match notebooks with space_id)
MATCH (n:Notebook)-[r:BELONGS_TO]->(s:Space)
RETURN
    count(r) as total_belongs_to,
    count(CASE WHEN r.migrated = true THEN 1 END) as migrated_belongs_to;

// Check for orphaned notebooks (have space_id but no BELONGS_TO relationship)
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) }
RETURN count(n) as orphaned_notebooks;

// Check for orphaned personal spaces (no OWNS relationship)
MATCH (s:Space {type: "personal"})
WHERE NOT EXISTS { (:User)-[:OWNS]->(s) }
RETURN count(s) as orphaned_personal_spaces;

// Check for users without personal space relationship
MATCH (u:User)
WHERE u.personal_space_id IS NOT NULL
  AND NOT EXISTS { (u)-[:OWNS]->(:Space {id: u.personal_space_id}) }
RETURN count(u) as users_without_owns_relationship;

// =============================================================================
// 4. SUMMARY STATISTICS
// =============================================================================

// Overall relationship statistics
MATCH (u:User)
OPTIONAL MATCH (u)-[:OWNS]->(owned:Space)
RETURN
    count(DISTINCT u) as total_users,
    count(DISTINCT CASE WHEN u.personal_space_id IS NOT NULL THEN u END) as users_with_personal_space_id,
    count(DISTINCT CASE WHEN owned IS NOT NULL THEN u END) as users_with_owns_relationship;

// Notebook relationship statistics
MATCH (n:Notebook)
OPTIONAL MATCH (n)-[:BELONGS_TO]->(s:Space)
RETURN
    count(DISTINCT n) as total_notebooks,
    count(DISTINCT CASE WHEN n.space_id IS NOT NULL THEN n END) as notebooks_with_space_id,
    count(DISTINCT CASE WHEN s IS NOT NULL THEN n END) as notebooks_with_belongs_to;

// =============================================================================
// ROLLBACK QUERIES (if needed)
// =============================================================================

// To rollback migrated OWNS relationships:
// MATCH ()-[r:OWNS]->() WHERE r.migrated = true DELETE r;

// To rollback migrated BELONGS_TO relationships:
// MATCH ()-[r:BELONGS_TO]->() WHERE r.migrated = true DELETE r;

// To rollback all migrated relationships:
// MATCH ()-[r]->() WHERE r.migrated = true DELETE r;
