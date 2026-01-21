#!/bin/bash

# Migration: 004_link_orphan_spaces_to_organizations
# Purpose: Links orphan organization spaces to their organizations via HAS_SPACE relationship

set -e

# Configuration - override with environment variables if needed
NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-password}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATION_FILE="$SCRIPT_DIR/004_link_orphan_spaces_to_organizations.cypher"

echo "========================================"
echo "Running Migration: 004_link_orphan_spaces_to_organizations"
echo "========================================"
echo "Neo4j URI: $NEO4J_URI"
echo "Neo4j User: $NEO4J_USER"
echo "Migration File: $MIGRATION_FILE"
echo "========================================"

if [ ! -f "$MIGRATION_FILE" ]; then
    echo "ERROR: Migration file not found: $MIGRATION_FILE"
    exit 1
fi

# Check if cypher-shell is available
if ! command -v cypher-shell &> /dev/null; then
    echo "ERROR: cypher-shell not found. Please install Neo4j tools."
    echo "You can run the migration manually in Neo4j Browser by copying the queries from:"
    echo "  $MIGRATION_FILE"
    exit 1
fi

echo ""
echo "Pre-migration state:"
echo "-------------------"

# Check current state before migration
cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASSWORD" <<EOF
// Pre-migration stats
MATCH (sp:Space) WHERE sp.space_type = 'organization'
RETURN 'Total org spaces' as metric, count(sp) as count
UNION ALL
MATCH (sp:Space) WHERE sp.space_type = 'organization' AND NOT EXISTS { (:Organization)-[:HAS_SPACE]->(sp) }
RETURN 'Org spaces without HAS_SPACE' as metric, count(sp) as count
UNION ALL
MATCH (o:Organization)
RETURN 'Total organizations' as metric, count(o) as count
UNION ALL
MATCH (o:Organization) WHERE NOT EXISTS { (o)-[:HAS_SPACE]->(:Space) }
RETURN 'Orgs without any spaces' as metric, count(o) as count;
EOF

echo ""
echo "Running migration..."
echo "-------------------"

# Run the migration
cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASSWORD" < "$MIGRATION_FILE"

echo ""
echo "========================================"
echo "Migration Complete!"
echo "========================================"
echo ""
echo "If there are orphan spaces reported above (NO_MATCHING_ORG), you can"
echo "optionally delete them by uncommenting the cleanup step in the migration file"
echo "and running it manually."
