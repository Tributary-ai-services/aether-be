// Migration: Backfill the DBHub source id on Database nodes
// Purpose: give existing postgres Database connections a source id that the
//          DBHub MCP server actually recognises, so queries route explicitly
//          instead of relying on DBHub's single-source fallback.
//
// Background
// ----------
// aether-be sends `crd_name` to DBHub as the `source` argument of the
// execute_sql tool. DBHub's sources are declared by the dbhub-operator's
// Database CRs - today exactly one: `tas-postgres`, pointing at
// postgres-shared.tas-shared.svc.cluster.local / tas_shared.
//
// Existing Database nodes were created before that field was populated, so
// crd_name is NULL and an empty source was being sent. DBHub tolerated it
// because only one source is configured, which means the console has been
// working by luck: adding a second source would have silently routed every
// query to whichever source DBHub picked.
//
// This migration sets crd_name only where the node demonstrably matches the
// tas-postgres source. Nodes that do not match are left alone: a wrong source
// id is worse than an absent one, because resolution falls back to the
// configured DBHUB_DEFAULT_SOURCE instead of querying the wrong database.
//
// Nodes touched are marked `dbhub_source_migrated: true` for rollback.

// =============================================================================
// 1. BACKFILL - postgres connections pointing at the shared cluster
// =============================================================================

MATCH (d:Database)
WHERE d.type = 'postgres'
  AND d.host STARTS WITH 'postgres-shared'
  AND (d.crd_name IS NULL OR d.crd_name = '')
SET d.crd_name = 'tas-postgres',
    d.crd_namespace = 'tas-mcp-servers',
    d.dbhub_source_migrated = true,
    d.updated_at = datetime()
RETURN count(d) AS backfilled;

// =============================================================================
// 2. CLEAR LEGACY GENERATED NAMES
// =============================================================================
// CreateDatabase used to mint `db-<8 hex>` as the CR name. No such CR was ever
// created, and DBHub has never heard of those ids, so they are not sources.
// Clearing them lets source resolution fall through to the configured default
// rather than sending DBHub a name it cannot resolve.

MATCH (d:Database)
WHERE d.crd_name IS NOT NULL
  AND d.crd_name =~ 'db-[0-9a-f]{8}'
SET d.crd_name = '',
    d.dbhub_source_migrated = true,
    d.updated_at = datetime()
RETURN count(d) AS cleared;

// =============================================================================
// 3. VERIFY
// =============================================================================

MATCH (d:Database)
RETURN d.type AS type,
       d.name AS name,
       coalesce(d.crd_name, '<none>') AS dbhub_source
ORDER BY type, name;

// =============================================================================
// ROLLBACK
// =============================================================================
// MATCH (d:Database) WHERE d.dbhub_source_migrated = true
// REMOVE d.dbhub_source_migrated
// SET d.crd_name = NULL;
