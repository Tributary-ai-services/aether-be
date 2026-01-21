#!/bin/bash
# Run migration 003: Backfill Space Relationships
#
# This script executes the Cypher queries to backfill OWNS and BELONGS_TO
# relationships for existing data.
#
# Prerequisites:
# - Neo4j is running and accessible
# - cypher-shell is installed (or use Neo4j Browser)
#
# Usage:
#   ./run_003_backfill.sh [NEO4J_URI] [NEO4J_USER] [NEO4J_PASSWORD]
#
# Examples:
#   ./run_003_backfill.sh                                    # Uses defaults
#   ./run_003_backfill.sh bolt://localhost:7687 neo4j password

set -e

# Configuration
NEO4J_URI="${1:-bolt://localhost:7687}"
NEO4J_USER="${2:-neo4j}"
NEO4J_PASSWORD="${3:-password}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== Migration 003: Backfill Space Relationships ===${NC}"
echo ""

# Check if cypher-shell is available
if command -v cypher-shell &> /dev/null; then
    CYPHER_SHELL="cypher-shell"
elif command -v /usr/bin/cypher-shell &> /dev/null; then
    CYPHER_SHELL="/usr/bin/cypher-shell"
else
    echo -e "${RED}Error: cypher-shell not found.${NC}"
    echo "Please install cypher-shell or run the migration manually via Neo4j Browser."
    echo "Migration file: migrations/003_backfill_space_relationships.cypher"
    exit 1
fi

# Function to run a Cypher query
run_query() {
    local query="$1"
    local description="$2"

    echo -e "${YELLOW}$description${NC}"
    $CYPHER_SHELL -a "$NEO4J_URI" -u "$NEO4J_USER" -p "$NEO4J_PASSWORD" <<< "$query"
    echo ""
}

echo "Connecting to Neo4j at $NEO4J_URI..."
echo ""

# Step 1: Backfill OWNS relationships
echo -e "${GREEN}Step 1: Creating OWNS relationships for personal spaces...${NC}"
run_query "
MATCH (u:User)
WHERE u.personal_space_id IS NOT NULL
MATCH (s:Space {id: u.personal_space_id})
WHERE NOT EXISTS { (u)-[:OWNS]->(s) }
MERGE (u)-[r:OWNS]->(s)
ON CREATE SET r.created_at = datetime(), r.migrated = true
RETURN count(r) as owns_relationships_created;
" "Backfill OWNS"

# Step 2: Backfill BELONGS_TO relationships
echo -e "${GREEN}Step 2: Creating BELONGS_TO relationships for notebooks...${NC}"
run_query "
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL
MATCH (s:Space {id: n.space_id})
WHERE NOT EXISTS { (n)-[:BELONGS_TO]->(s) }
MERGE (n)-[r:BELONGS_TO]->(s)
ON CREATE SET r.created_at = datetime(), r.migrated = true
RETURN count(r) as belongs_to_relationships_created;
" "Backfill BELONGS_TO"

# Step 3: Verification
echo -e "${GREEN}Step 3: Verifying migration...${NC}"

echo -e "${YELLOW}OWNS relationships:${NC}"
run_query "
MATCH (u:User)-[r:OWNS]->(s:Space)
RETURN
    count(r) as total_owns,
    count(CASE WHEN r.migrated = true THEN 1 END) as migrated_owns;
" "Count OWNS"

echo -e "${YELLOW}BELONGS_TO relationships:${NC}"
run_query "
MATCH (n:Notebook)-[r:BELONGS_TO]->(s:Space)
RETURN
    count(r) as total_belongs_to,
    count(CASE WHEN r.migrated = true THEN 1 END) as migrated_belongs_to;
" "Count BELONGS_TO"

echo -e "${YELLOW}Checking for orphaned data:${NC}"
run_query "
MATCH (n:Notebook)
WHERE n.space_id IS NOT NULL AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) }
RETURN count(n) as orphaned_notebooks;
" "Orphaned notebooks"

run_query "
MATCH (s:Space {type: 'personal'})
WHERE NOT EXISTS { (:User)-[:OWNS]->(s) }
RETURN count(s) as orphaned_personal_spaces;
" "Orphaned personal spaces"

echo -e "${GREEN}=== Migration Complete ===${NC}"
echo ""
echo "If you need to rollback, run:"
echo "  MATCH ()-[r]->() WHERE r.migrated = true DELETE r;"
