#!/bin/bash
# Onboarding Database Test Script - Simplified HTTP API Testing
# Uses kubectl exec with backend pod to query Neo4j HTTP API
#
# Usage: ./test-onboarding-database.sh

set -e

# Configuration
NAMESPACE="aether-be"
USERNAME="neo4j"
PASSWORD="password"
DATABASE="neo4j"
SERVICE_NAME="neo4j.aether-be.svc.cluster.local"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions for colored output
print_status() {
    echo -e "${BLUE}===${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_header() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Get a backend pod name
BACKEND_POD=$(kubectl get pods -n "$NAMESPACE" -l app=aether-backend -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$BACKEND_POD" ]; then
    echo -e "${RED}ERROR: No backend pod found. Ensure aether-backend deployment is running.${NC}"
    exit 1
fi

# Function to execute cypher query via HTTP API from backend pod
exec_cypher() {
    local query="$1"
    kubectl exec -n "$NAMESPACE" "$BACKEND_POD" -- \
        wget -q -O- \
        --header="Content-Type: application/json" \
        --header="Authorization: Basic $(echo -n ${USERNAME}:${PASSWORD} | base64)" \
        --post-data="{\"statements\":[{\"statement\":\"$query\"}]}" \
        "http://${SERVICE_NAME}:7474/db/${DATABASE}/tx/commit" 2>/dev/null
}

# Function to extract single value from Neo4j HTTP API response
extract_value() {
    local json="$1"
    # Extract first row value: {"row":[VALUE]} -> VALUE
    echo "$json" | grep -o '"row":\[[^]]*\]' | head -1 | sed 's/"row":\[\([^]]*\)\]/\1/' | tr -d '"' | tr -d ' '
}

# Function to check if response has errors
has_errors() {
    local json="$1"
    echo "$json" | grep -q '"errors":\[\]' && return 1 || return 0
}

print_header "Onboarding Database Verification"
echo "  Namespace: $NAMESPACE"
echo "  Service: $SERVICE_NAME"
echo "  Testing Method: HTTP Transactional API via backend pod"
echo "  Backend Pod: $BACKEND_POD"
echo ""

##############################################################################
# Database Tests using HTTP API
##############################################################################

print_header "Database Tests (HTTP API via Service Discovery)"

# Test 1: Basic connectivity
print_status "Test 1: Neo4j HTTP API connectivity"
RESULT=$(exec_cypher "RETURN 1 AS test")
if echo "$RESULT" | grep -q '"errors":\[\]'; then
    print_success "HTTP API connection successful"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "HTTP API connection failed"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 2: User nodes exist
print_status "Test 2: User nodes exist in database"
RESULT=$(exec_cypher "MATCH (u:User) RETURN count(u) AS count")
COUNT=$(extract_value "$RESULT")
if [ -n "$COUNT" ] && [ "$COUNT" -gt 0 ] 2>/dev/null; then
    print_success "Found $COUNT user(s) in database"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "No users found or query failed"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 3: Tutorial fields exist
print_status "Test 3: Tutorial fields exist on User nodes"
RESULT=$(exec_cypher "MATCH (u:User) WHERE u.tutorial_completed IS NOT NULL RETURN count(u) AS count")
COUNT=$(extract_value "$RESULT")
if [ -n "$COUNT" ] 2>/dev/null; then
    print_success "$COUNT users have tutorial_completed field"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "Tutorial fields missing or query failed"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 4: All users migrated
print_status "Test 4: All users have been migrated"
RESULT=$(exec_cypher "MATCH (u:User) WHERE u.tutorial_completed IS NULL RETURN count(u) AS count")
COUNT=$(extract_value "$RESULT")
if [ "$COUNT" == "0" ]; then
    print_success "All users migrated (0 with NULL tutorial_completed)"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_warning "$COUNT users still need migration"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 5: Data consistency - completed users have timestamps
print_status "Test 5: Completed users have timestamps"
RESULT=$(exec_cypher "MATCH (u:User) WHERE u.tutorial_completed = true AND u.tutorial_completed_at IS NULL RETURN count(u) AS count")
COUNT=$(extract_value "$RESULT")
if [ "$COUNT" == "0" ]; then
    print_success "Data consistent: completed users have timestamps"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "$COUNT completed users missing timestamps"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 6: Data consistency - incomplete users lack timestamps
print_status "Test 6: Incomplete users lack timestamps"
RESULT=$(exec_cypher "MATCH (u:User) WHERE u.tutorial_completed = false AND u.tutorial_completed_at IS NOT NULL RETURN count(u) AS count")
COUNT=$(extract_value "$RESULT")
if [ "$COUNT" == "0" ]; then
    print_success "Data consistent: incomplete users lack timestamps"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "$COUNT incomplete users have unexpected timestamps"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

# Test 7: Verify actual user data structure
print_status "Test 7: Verify user data structure"
RESULT=$(exec_cypher "MATCH (u:User) RETURN u.email LIMIT 1")
if echo "$RESULT" | grep -q "@"; then
    print_success "User data structure verified"
    PASSED_TESTS=$((PASSED_TESTS + 1))
else
    print_error "User data structure check failed"
    echo "Response: $RESULT"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi
TOTAL_TESTS=$((TOTAL_TESTS + 1))

##############################################################################
# SUMMARY
##############################################################################

print_header "Test Summary"

echo ""
echo -e "Total Tests:   ${BLUE}$TOTAL_TESTS${NC}"
echo -e "Passed:        ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed:        ${RED}$FAILED_TESTS${NC}"
echo ""

PASS_RATE=$((100 * PASSED_TESTS / TOTAL_TESTS))
echo -e "Pass Rate:     ${BLUE}${PASS_RATE}%${NC}"

echo ""
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}  ✓ ALL TESTS PASSED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    EXIT_CODE=0
else
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}  ✗ SOME TESTS FAILED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    EXIT_CODE=1
fi

echo ""
echo "Testing Method Used:"
echo "  ✓ HTTP Transactional API: http://$SERVICE_NAME:7474/db/$DATABASE/tx/commit"
echo "  ✓ Service Discovery: Uses cluster-internal DNS resolution"
echo "  ✓ Backend Pod Exec: Queries from existing backend pod (has wget pre-installed)"
echo "  ✓ Bypasses TLS: HTTP API doesn't require certificate validation"
echo ""

exit $EXIT_CODE
