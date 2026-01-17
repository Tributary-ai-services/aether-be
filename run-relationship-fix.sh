#!/bin/bash

# Neo4j Relationship Fix Script
# This script connects to Neo4j and fixes missing MEMBER_OF relationships

# Default Neo4j connection parameters - update these as needed
NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}"
NEO4J_USERNAME="${NEO4J_USERNAME:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-password}"

echo "Connecting to Neo4j at $NEO4J_URI..."
echo "Running relationship fix queries..."

# Run the step-by-step fix using cypher-shell
if command -v cypher-shell &> /dev/null; then
    echo "Using cypher-shell to run fixes..."
    cypher-shell -a "$NEO4J_URI" -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" --file fix-relationships-step-by-step.cypher
else
    echo "cypher-shell not found. Please run the queries manually in Neo4j Browser."
    echo "Queries are in the file: fix-relationships-step-by-step.cypher"
    echo ""
    echo "Or you can use curl to run individual queries:"
    echo ""
    echo "# First, check current state:"
    echo "curl -X POST \\"
    echo "  http://localhost:7474/db/data/transaction/commit \\"
    echo "  -H 'Content-Type: application/json' \\"
    echo "  -H 'Authorization: Basic <base64-encoded-username:password>' \\"
    echo "  -d '{\"statements\":[{\"statement\":\"MATCH (u:User) RETURN count(u) as users\"}]}'"
fi

echo "Done! Check the output above for any errors."
echo ""
echo "If you're using Neo4j Browser, you can copy and paste the queries from:"
echo "- fix-missing-relationships.cypher (all-in-one)"  
echo "- fix-relationships-step-by-step.cypher (step-by-step debugging)"