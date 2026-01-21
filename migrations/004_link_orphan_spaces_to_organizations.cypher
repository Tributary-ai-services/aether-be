// Migration: 004_link_orphan_spaces_to_organizations
// Purpose: Links orphan organization spaces to their organizations via HAS_SPACE relationship
//          Creates default spaces for organizations that don't have any spaces
//          Cleans up truly orphaned spaces that have no matching organization
//
// Run with: cypher-shell -a bolt://localhost:7687 -u neo4j -p password < 004_link_orphan_spaces_to_organizations.cypher

// ============================================================================
// STEP 1: Link orphan organization spaces to matching organizations
// These are spaces with owner_type='organization' and owner_id set but no HAS_SPACE relationship
// ============================================================================

// Find and link orphan organization spaces to their organizations
MATCH (sp:Space)
WHERE sp.owner_type = 'organization'
  AND sp.owner_id IS NOT NULL
  AND NOT EXISTS { (:Organization)-[:HAS_SPACE]->(sp) }
WITH sp
MATCH (o:Organization {id: sp.owner_id})
MERGE (o)-[:HAS_SPACE {
    created_at: datetime(),
    is_default: false,
    migrated: true,
    migration_run: '004_link_orphan_spaces_to_organizations'
}]->(sp)
RETURN count(*) as spaces_linked_to_orgs;

// ============================================================================
// STEP 2: Create default spaces for organizations without any spaces
// ============================================================================

// Create default spaces for organizations that don't have any spaces yet
MATCH (o:Organization)
WHERE NOT EXISTS { (o)-[:HAS_SPACE]->(:Space) }
WITH o, datetime() as now
CREATE (sp:Space {
    id: 'space_' + toString(timestamp()) + '_' + replace(o.id, 'org_', ''),
    tenant_id: 'tenant_' + toString(timestamp()) + '_' + replace(o.id, 'org_', ''),
    name: o.name + ' Default Space',
    description: 'Default workspace for ' + o.name,
    space_type: 'organization',
    visibility: 'private',
    owner_id: o.id,
    owner_type: 'organization',
    status: 'active',
    created_at: now,
    updated_at: now
})
CREATE (o)-[:HAS_SPACE {
    created_at: now,
    is_default: true,
    migrated: true,
    migration_run: '004_link_orphan_spaces_to_organizations'
}]->(sp)
RETURN count(sp) as default_spaces_created;

// ============================================================================
// STEP 3: Report orphan spaces that have no matching organization
// These spaces have owner_type='organization' but no organization with that ID exists
// We'll report them first, then the cleanup step can be run manually if desired
// ============================================================================

// Find orphan spaces with no matching organization (report only, don't delete automatically)
MATCH (sp:Space)
WHERE sp.owner_type = 'organization'
  AND sp.owner_id IS NOT NULL
  AND NOT EXISTS { (:Organization {id: sp.owner_id}) }
  AND NOT EXISTS { (:Organization)-[:HAS_SPACE]->(sp) }
RETURN sp.id as orphan_space_id,
       sp.name as space_name,
       sp.owner_id as claimed_org_id,
       sp.created_at as created_at,
       'NO_MATCHING_ORG' as issue
ORDER BY sp.created_at DESC;

// ============================================================================
// STEP 4: Verify migration results
// ============================================================================

// Count organization spaces with HAS_SPACE relationship
MATCH (o:Organization)-[:HAS_SPACE]->(sp:Space)
RETURN 'Org spaces with HAS_SPACE' as metric, count(sp) as count
UNION ALL
// Count organization spaces without HAS_SPACE relationship (should be 0 after migration)
MATCH (sp:Space)
WHERE sp.owner_type = 'organization'
  AND NOT EXISTS { (:Organization)-[:HAS_SPACE]->(sp) }
RETURN 'Org spaces without HAS_SPACE' as metric, count(sp) as count
UNION ALL
// Count organizations with at least one space
MATCH (o:Organization)-[:HAS_SPACE]->(:Space)
RETURN 'Orgs with spaces' as metric, count(DISTINCT o) as count
UNION ALL
// Count organizations without any spaces
MATCH (o:Organization)
WHERE NOT EXISTS { (o)-[:HAS_SPACE]->(:Space) }
RETURN 'Orgs without spaces (should be 0)' as metric, count(o) as count;

// ============================================================================
// OPTIONAL STEP: Delete truly orphan spaces (run manually after reviewing report)
// Uncomment the following to delete orphan spaces with no matching organization
// ============================================================================

// MATCH (sp:Space)
// WHERE sp.owner_type = 'organization'
//   AND sp.owner_id IS NOT NULL
//   AND NOT EXISTS { (:Organization {id: sp.owner_id}) }
//   AND NOT EXISTS { (:Organization)-[:HAS_SPACE]->(sp) }
// DETACH DELETE sp
// RETURN count(sp) as orphan_spaces_deleted;
