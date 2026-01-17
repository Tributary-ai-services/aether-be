#!/bin/bash
# Script to run diagnostic tests for MinIO and document operations

set -e

echo "=================================="
echo "Running Aether Diagnostic Tests"
echo "=================================="

# Check if services are running
echo "Checking required services..."

# Check Neo4j
if nc -z localhost 7687; then
    echo "✓ Neo4j is running on port 7687"
else
    echo "✗ Neo4j is not running. Please start Neo4j first."
    exit 1
fi

# Check MinIO
if nc -z localhost 9000; then
    echo "✓ MinIO is running on port 9000"
else
    echo "✗ MinIO is not running. Please start MinIO first."
    exit 1
fi

# Check Redis
if nc -z localhost 6379; then
    echo "✓ Redis is running on port 6379"
else
    echo "✗ Redis is not running. Please start Redis first."
    exit 1
fi

echo ""
echo "Running tests..."
echo ""

# Run storage integration tests
echo "1. Running MinIO Storage Tests"
echo "------------------------------"
go test -v ./internal/services -run TestMinIOFolderOperations -count=1

echo ""
echo "2. Running Document Lifecycle Tests"
echo "-----------------------------------"
go test -v ./internal/services -run TestCompleteDocumentLifecycle -count=1

echo ""
echo "3. Running Document Count Accuracy Tests"
echo "----------------------------------------"
go test -v ./internal/services -run TestDocumentCountAccuracyWithNeo4j -count=1

echo ""
echo "4. Running Notebook Compliance Tests"
echo "------------------------------------"
go test -v ./internal/services -run TestNotebookComplianceOptions -count=1

echo ""
echo "=================================="
echo "All diagnostic tests completed!"
echo "=================================="