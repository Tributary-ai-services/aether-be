#!/bin/bash

# Run data migrations for space-based tenant model

set -e

echo "Running Aether Backend Data Migrations"
echo "======================================"

# Load environment variables
if [ -f ../.env ]; then
    source ../.env
    echo "✓ Loaded environment variables"
else
    echo "✗ .env file not found, using defaults"
fi

# Set Neo4j connection variables
export NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}"
export NEO4J_USERNAME="${NEO4J_USERNAME:-neo4j}"
export NEO4J_PASSWORD="${NEO4J_PASSWORD:-password}"

echo ""
echo "Neo4j Connection:"
echo "  URI: $NEO4J_URI"
echo "  Username: $NEO4J_USERNAME"
echo ""

# Check if Neo4j is accessible
echo -n "Checking Neo4j connection... "
if timeout 5 cypher-shell -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -a "$NEO4J_URI" "RETURN 1" >/dev/null 2>&1; then
    echo "✓ Connected"
else
    echo "✗ Failed"
    echo "Error: Cannot connect to Neo4j. Please check your connection settings."
    exit 1
fi

# Run migrations
echo ""
echo "Running migrations..."
echo ""

# Migration 001: Migrate to space model
if [ -f "001_migrate_to_space_model.go" ]; then
    echo "Running migration 001: Migrate to space model"
    go run 001_migrate_to_space_model.go
    if [ $? -eq 0 ]; then
        echo "✓ Migration 001 completed successfully"
    else
        echo "✗ Migration 001 failed"
        exit 1
    fi
fi

# Migration 002: Add chunk relationships
if [ -f "002_add_chunk_relationships.go" ]; then
    echo "Running migration 002: Add chunk relationships"
    go run 002_add_chunk_relationships.go
    if [ $? -eq 0 ]; then
        echo "✓ Migration 002 completed successfully"
    else
        echo "✗ Migration 002 failed"
        exit 1
    fi
fi

echo ""
echo "All migrations completed successfully!"
echo ""

# Show summary statistics
echo "Database Summary:"
cypher-shell -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" -a "$NEO4J_URI" --format plain <<EOF
MATCH (n:Notebook) 
WHERE n.tenant_id IS NOT NULL AND n.space_id IS NOT NULL
RETURN "Notebooks with space info: " + count(n) as summary
UNION
MATCH (n:Notebook) 
WHERE n.tenant_id IS NULL OR n.space_id IS NULL
RETURN "Notebooks without space info: " + count(n) as summary
UNION
MATCH (d:Document) 
WHERE d.tenant_id IS NOT NULL AND d.space_id IS NOT NULL
RETURN "Documents with space info: " + count(d) as summary
UNION
MATCH (d:Document) 
WHERE d.tenant_id IS NULL OR d.space_id IS NULL
RETURN "Documents without space info: " + count(d) as summary
UNION
MATCH (d:Document) 
WHERE d.chunking_strategy IS NOT NULL
RETURN "Documents with chunk metadata: " + count(d) as summary
UNION
MATCH (c:Chunk)
RETURN "Total chunks: " + count(c) as summary
UNION
MATCH (d:Document)-[:CONTAINS]->(c:Chunk)
RETURN "Documents with chunks: " + count(DISTINCT d) as summary
UNION
MATCH (u:User)
WHERE u.personal_space_id IS NOT NULL
RETURN "Users with personal_space_id: " + count(u) as summary
UNION
MATCH (u:User)
WHERE u.personal_space_id IS NULL
RETURN "Users without personal_space_id: " + count(u) as summary;
EOF

echo ""
echo "Migration complete!"