// Migration: Reconcile Space/Notebook data drift
// Applied: 2026-05-30 against production Neo4j (aether-be/neo4j-0)
// Context: Verification (see NOTEBOOK-SPACE-RELATIONSHIP-PLAN.md, "Live Neo4j Verification")
//          found legacy data that the runtime app never reconciled because the
//          cypher migrations 002-004 had not been run. This migration captures the
//          exact, idempotent fixes that were applied.
//
// Safe to re-run: every statement is idempotent (IF NOT EXISTS / null-guarded).

// =============================================================================
// 1. Backfill space_type from legacy `type` property
// Older Space nodes stored the type under `type`; GetUserSpaces filters on
// `space_type`, so those spaces were invisible to their owners.
// =============================================================================
MATCH (s:Space)
WHERE s.space_type IS NULL AND s.type IS NOT NULL
SET s.space_type = s.type, s.updated_at = datetime()
RETURN count(s) AS space_type_backfilled;

// =============================================================================
// 2. Backfill status='active' on spaces created before the status field existed
// (excludes anything already soft-deleted).
// =============================================================================
MATCH (s:Space)
WHERE s.status IS NULL AND s.deleted_at IS NULL
SET s.status = 'active', s.updated_at = datetime()
RETURN count(s) AS status_backfilled;

// =============================================================================
// 3. Remove orphaned notebooks
// Notebooks whose owner no longer exists AND whose space_id points to a Space
// that was never created (no BELONGS_TO, no OWNED_BY, no content). Dead data.
// =============================================================================
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL
  AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) }
  AND NOT EXISTS { (n)-[:OWNED_BY]->(:User) }
  AND NOT EXISTS { (:Space {id: n.space_id}) }
DETACH DELETE n
RETURN count(n) AS orphaned_notebooks_deleted;

// =============================================================================
// 4. Verification
// =============================================================================
MATCH (n:Notebook)
RETURN count(n) AS total_notebooks,
       count(CASE WHEN n.space_id IS NOT NULL
                   AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) } THEN 1 END) AS orphaned_notebooks;

MATCH (s:Space)
RETURN count(s) AS total_spaces,
       count(CASE WHEN s.space_type IS NULL THEN 1 END) AS missing_space_type,
       count(CASE WHEN s.status IS NULL THEN 1 END) AS missing_status;
