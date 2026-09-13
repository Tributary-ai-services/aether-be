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
// Existing Database nodes carry a legacy "db-<8 hex>" name that CreateDatabase
// used to mint for a CR it never created - verified against production: all 10
// nodes, e.g. db-335d056c and db-0d462710. So aether-be has been sending DBHub
// a source id it has never heard of. DBHub tolerated it because only one source
// is configured, which means the console has been working by luck: adding a
// second source would have silently routed every query to whichever source
// DBHub picked.
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

// Matches nodes with no source recorded AND nodes still carrying a legacy
// "db-<8 hex>" name. Every Database node in production carries a legacy name
// today, so restricting this to NULL/empty would match nothing and the whole
// migration would be a no-op that merely blanked the bogus values.
MATCH (d:Database)
WHERE d.type = 'postgres'
  AND d.host STARTS WITH 'postgres-shared'
  AND (d.crd_name IS NULL OR d.crd_name = '' OR d.crd_name =~ 'db-[0-9a-f]{8}')
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
//
// Runs after the backfill and skips anything the backfill claimed, so the two
// steps cannot undo each other: the 3 shared-postgres connections keep
// 'tas-postgres', and the remaining 7 (2 minio, 2 kafka, 2 grafana, 1 neo4j)
// simply lose their bogus names.

MATCH (d:Database)
WHERE d.crd_name IS NOT NULL
  AND d.crd_name =~ 'db-[0-9a-f]{8}'
  AND coalesce(d.dbhub_source_migrated, false) = false
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
