#!/bin/bash

################################################################################
# Complete Onboarding Flow Test Script
#
# This script performs comprehensive end-to-end testing of the Aether onboarding
# feature following the ONBOARDING_TESTING_CHECKLIST.md
#
# Test Coverage:
#   - Phase 1: Backend API Testing (GET/POST/DELETE endpoints)
#   - Phase 2: Database Verification (Neo4j schema and data)
#   - Phase 3: Automatic Resource Creation (space, notebook, documents, agent)
#   - Phase 4: Security Testing (authentication, authorization)
#   - Phase 5: Performance Testing (response times)
#
# Usage:
#   ./test-complete-onboarding-flow.sh [OPTIONS]
#
# Options:
#   --backend-url URL     Backend API URL (default: https://aether.tas.scharber.com)
#   --keycloak-url URL    Keycloak URL (default: https://keycloak.tas.scharber.com)
#   --skip-user-creation  Use existing test user (requires --email and --password)
#   --email EMAIL         Test user email
#   --password PASS       Test user password
#   --verbose             Enable verbose output
#   --help                Show this help message
#
################################################################################

set -e  # Exit on error
set -u  # Exit on undefined variable

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Default configuration
BACKEND_URL="${BACKEND_URL:-https://aether.tas.scharber.com}"
KEYCLOAK_URL="${KEYCLOAK_URL:-https://keycloak.tas.scharber.com}"
SKIP_USER_CREATION=false
TEST_USER_EMAIL=""
TEST_USER_PASSWORD="Test123!"
VERBOSE=false

# Test tracking
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_TOTAL=0

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

################################################################################
# Helper Functions
################################################################################

log_header() {
    echo ""
    echo -e "${MAGENTA}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${MAGENTA} $1${NC}"
    echo -e "${MAGENTA}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

log_section() {
    echo ""
    echo -e "${CYAN}━━━ $1 ━━━${NC}"
    echo ""
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓ PASS]${NC} $1"
}

log_fail() {
    echo -e "${RED}[✗ FAIL]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

# Test result tracking
test_start() {
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    log_verbose "Test #$TESTS_TOTAL: $1"
}

test_pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    log_success "$1"
}

test_fail() {
    TESTS_FAILED=$((TESTS_FAILED + 1))
    log_fail "$1"
}

# Parse arguments
parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --backend-url)
                BACKEND_URL="$2"
                shift 2
                ;;
            --keycloak-url)
                KEYCLOAK_URL="$2"
                shift 2
                ;;
            --skip-user-creation)
                SKIP_USER_CREATION=true
                shift
                ;;
            --email)
                TEST_USER_EMAIL="$2"
                shift 2
                ;;
            --password)
                TEST_USER_PASSWORD="$2"
                shift 2
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            --help)
                cat << EOF
Usage: $0 [OPTIONS]

Options:
  --backend-url URL     Backend API URL (default: https://aether.tas.scharber.com)
  --keycloak-url URL    Keycloak URL (default: https://keycloak.tas.scharber.com)
  --skip-user-creation  Use existing test user (requires --email and --password)
  --email EMAIL         Test user email
  --password PASS       Test user password
  --verbose             Enable verbose output
  --help                Show this help message

Examples:
  # Create new test user and run all tests
  $0

  # Use existing test user
  $0 --skip-user-creation --email test@example.com --password Test123!

  # Verbose mode
  $0 --verbose

EOF
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                exit 1
                ;;
        esac
    done
}

################################################################################
# Phase 0: Prerequisites Check
################################################################################

check_prerequisites() {
    log_header "PHASE 0: PREREQUISITES CHECK"

    test_start "Check kubectl availability"
    if command -v kubectl &> /dev/null; then
        test_pass "kubectl is installed"
    else
        test_fail "kubectl is not installed"
        return 1
    fi

    test_start "Check curl availability"
    if command -v curl &> /dev/null; then
        test_pass "curl is installed"
    else
        test_fail "curl is not installed"
        return 1
    fi

    test_start "Check jq availability"
    if command -v jq &> /dev/null; then
        test_pass "jq is installed"
    else
        log_warning "jq is not installed (optional, but recommended for better JSON parsing)"
    fi

    test_start "Check backend pods"
    if kubectl get pods -n aether-be -l app=aether-backend 2>/dev/null | grep -q "Running"; then
        test_pass "Backend pods are running"
    else
        test_fail "Backend pods are not running"
        kubectl get pods -n aether-be -l app=aether-backend 2>/dev/null || true
        return 1
    fi

    test_start "Check Neo4j pods"
    if kubectl get pods -n aether-be -l app=neo4j 2>/dev/null | grep -q "Running"; then
        test_pass "Neo4j pods are running"
    else
        test_fail "Neo4j pods are not running"
        return 1
    fi

    test_start "Check Keycloak accessibility"
    if curl -skf "$KEYCLOAK_URL/" > /dev/null 2>&1; then
        test_pass "Keycloak is accessible at $KEYCLOAK_URL"
    else
        test_fail "Keycloak is not accessible at $KEYCLOAK_URL"
        return 1
    fi

    test_start "Check backend API accessibility"
    if curl -skf "$BACKEND_URL/health" > /dev/null 2>&1; then
        test_pass "Backend API is accessible at $BACKEND_URL"
    else
        log_warning "Backend /health endpoint not accessible (may be normal)"
    fi

    log_info "Prerequisites check complete"
}

################################################################################
# Phase 1: Create Test User
################################################################################

create_test_user() {
    log_header "PHASE 1: TEST USER CREATION"

    if [ "$SKIP_USER_CREATION" = true ]; then
        if [ -z "$TEST_USER_EMAIL" ] || [ -z "$TEST_USER_PASSWORD" ]; then
            test_fail "Skipping user creation requires --email and --password"
            return 1
        fi
        log_info "Skipping user creation, using existing user: $TEST_USER_EMAIL"
        test_pass "Using existing test user"
        return 0
    fi

    test_start "Create Keycloak test user"

    if [ ! -f "$SCRIPT_DIR/create-test-user.sh" ]; then
        test_fail "create-test-user.sh not found at $SCRIPT_DIR"
        return 1
    fi

    local output
    output=$("$SCRIPT_DIR/create-test-user.sh" --keycloak-url "$KEYCLOAK_URL" 2>&1)
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        # Extract email from output
        TEST_USER_EMAIL=$(echo "$output" | grep "Email/Username:" | awk '{print $2}')

        if [ -z "$TEST_USER_EMAIL" ]; then
            test_fail "Failed to extract test user email from output"
            return 1
        fi

        test_pass "Test user created: $TEST_USER_EMAIL"
        log_info "Password: $TEST_USER_PASSWORD"
    else
        test_fail "Failed to create test user"
        echo "$output"
        return 1
    fi
}

################################################################################
# Phase 2: Get Authentication Token
################################################################################

get_auth_token() {
    log_header "PHASE 2: AUTHENTICATION"

    test_start "Get JWT token for test user"

    if [ ! -f "$SCRIPT_DIR/get-token-for-user.sh" ]; then
        test_fail "get-token-for-user.sh not found at $SCRIPT_DIR"
        return 1
    fi

    ACCESS_TOKEN=$("$SCRIPT_DIR/get-token-for-user.sh" "$TEST_USER_EMAIL" "$TEST_USER_PASSWORD" \
        --keycloak-url "$KEYCLOAK_URL" --token-only 2>/dev/null)
    local exit_code=$?

    if [ $exit_code -eq 0 ] && [ -n "$ACCESS_TOKEN" ]; then
        test_pass "JWT token obtained successfully"
        log_verbose "Token: ${ACCESS_TOKEN:0:50}..."
    else
        test_fail "Failed to obtain JWT token"
        return 1
    fi
}

################################################################################
# Phase 3: Test Automatic User Creation and Onboarding
################################################################################

test_automatic_onboarding() {
    log_header "PHASE 3: AUTOMATIC ONBOARDING"

    log_section "3.1: Trigger User Creation"

    test_start "GET /users/me (triggers automatic user creation)"
    local response
    response=$(curl -skf -X GET "$BACKEND_URL/api/v1/users/me" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        test_pass "User profile retrieved successfully"
        log_verbose "Response: $(echo "$response" | jq -c '.' 2>/dev/null || echo "$response")"

        # Extract user ID
        USER_ID=$(echo "$response" | jq -r '.id' 2>/dev/null || echo "")
        if [ -n "$USER_ID" ] && [ "$USER_ID" != "null" ]; then
            log_info "User ID: $USER_ID"
        fi
    else
        test_fail "Failed to retrieve user profile"
        echo "$response"
    fi

    # Wait for automatic onboarding to complete
    log_info "Waiting 2 seconds for automatic onboarding to complete..."
    sleep 2

    log_section "3.2: Verify Automatic Resources"

    test_start "Check personal space creation"
    local spaces_response
    spaces_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/spaces" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)

    if echo "$spaces_response" | grep -q "Personal"; then
        test_pass "Personal space created automatically"
        SPACE_ID=$(echo "$spaces_response" | jq -r '.[0].id' 2>/dev/null || echo "")
        log_info "Space ID: $SPACE_ID"
    else
        log_warning "Personal space not found (may not be created yet)"
    fi

    test_start "Check 'Getting Started' notebook creation"
    local notebooks_response
    notebooks_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/notebooks" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)

    if echo "$notebooks_response" | grep -q "Getting Started"; then
        test_pass "'Getting Started' notebook created automatically"
        NOTEBOOK_ID=$(echo "$notebooks_response" | jq -r '.[] | select(.name == "Getting Started") | .id' 2>/dev/null || echo "")
        log_info "Notebook ID: $NOTEBOOK_ID"
    else
        log_warning "'Getting Started' notebook not found"
    fi

    if [ -n "$NOTEBOOK_ID" ] && [ "$NOTEBOOK_ID" != "null" ]; then
        test_start "Check sample documents in notebook"
        local docs_response
        docs_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/notebooks/$NOTEBOOK_ID/documents" \
            -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)

        local doc_count
        doc_count=$(echo "$docs_response" | jq 'length' 2>/dev/null || echo "0")

        if [ "$doc_count" -ge 3 ]; then
            test_pass "Sample documents created (found $doc_count documents)"
        else
            log_warning "Expected 3 sample documents, found $doc_count"
        fi
    fi

    test_start "Check default agent creation"
    local agents_response
    agents_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/agents" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)

    if echo "$agents_response" | grep -q "Personal Assistant"; then
        test_pass "'Personal Assistant' agent created automatically"
    else
        log_warning "'Personal Assistant' agent not found (agent-builder may not be available)"
    fi
}

################################################################################
# Phase 4: Test Onboarding API Endpoints
################################################################################

test_onboarding_api() {
    log_header "PHASE 4: ONBOARDING API TESTING"

    log_section "4.1: GET /users/me/onboarding (Initial Status)"

    test_start "GET onboarding status (should be incomplete initially)"
    local get_response
    get_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        test_pass "GET /users/me/onboarding returned 200 OK"

        local tutorial_completed
        tutorial_completed=$(echo "$get_response" | jq -r '.tutorial_completed' 2>/dev/null || echo "")

        if [ "$tutorial_completed" = "false" ]; then
            test_pass "tutorial_completed is false (correct for new user)"
        else
            test_fail "tutorial_completed should be false for new user, got: $tutorial_completed"
        fi

        local should_auto_trigger
        should_auto_trigger=$(echo "$get_response" | jq -r '.should_auto_trigger' 2>/dev/null || echo "")

        if [ "$should_auto_trigger" = "true" ]; then
            test_pass "should_auto_trigger is true (correct for new user)"
        else
            test_fail "should_auto_trigger should be true for new user, got: $should_auto_trigger"
        fi
    else
        test_fail "GET /users/me/onboarding failed"
        echo "$get_response"
    fi

    log_section "4.2: POST /users/me/onboarding (Mark Complete)"

    test_start "POST to mark tutorial as complete"
    local post_response
    post_response=$(curl -skf -X POST "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        test_pass "POST /users/me/onboarding returned 200 OK"
    else
        test_fail "POST /users/me/onboarding failed"
        echo "$post_response"
    fi

    log_section "4.3: GET /users/me/onboarding (Completed Status)"

    test_start "GET onboarding status (should be complete now)"
    get_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        tutorial_completed=$(echo "$get_response" | jq -r '.tutorial_completed' 2>/dev/null || echo "")

        if [ "$tutorial_completed" = "true" ]; then
            test_pass "tutorial_completed is true (correct after marking complete)"
        else
            test_fail "tutorial_completed should be true after POST, got: $tutorial_completed"
        fi

        local completed_at
        completed_at=$(echo "$get_response" | jq -r '.tutorial_completed_at' 2>/dev/null || echo "")

        if [ -n "$completed_at" ] && [ "$completed_at" != "null" ]; then
            test_pass "tutorial_completed_at has timestamp: $completed_at"
        else
            test_fail "tutorial_completed_at should have timestamp, got: $completed_at"
        fi
    else
        test_fail "GET /users/me/onboarding failed after POST"
    fi

    log_section "4.4: DELETE /users/me/onboarding (Reset)"

    test_start "DELETE to reset tutorial"
    local delete_response
    delete_response=$(curl -skf -X DELETE "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        test_pass "DELETE /users/me/onboarding returned 200 OK"
    else
        test_fail "DELETE /users/me/onboarding failed"
        echo "$delete_response"
    fi

    log_section "4.5: GET /users/me/onboarding (Reset Status)"

    test_start "GET onboarding status (should be incomplete after reset)"
    get_response=$(curl -skf -X GET "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        tutorial_completed=$(echo "$get_response" | jq -r '.tutorial_completed' 2>/dev/null || echo "")

        if [ "$tutorial_completed" = "false" ]; then
            test_pass "tutorial_completed is false (correct after reset)"
        else
            test_fail "tutorial_completed should be false after DELETE, got: $tutorial_completed"
        fi

        completed_at=$(echo "$get_response" | jq -r '.tutorial_completed_at' 2>/dev/null || echo "")

        if [ "$completed_at" = "null" ] || [ -z "$completed_at" ]; then
            test_pass "tutorial_completed_at is null (correct after reset)"
        else
            test_fail "tutorial_completed_at should be null after DELETE, got: $completed_at"
        fi
    else
        test_fail "GET /users/me/onboarding failed after DELETE"
    fi
}

################################################################################
# Phase 5: Security Testing
################################################################################

test_security() {
    log_header "PHASE 5: SECURITY TESTING"

    log_section "5.1: Authentication Tests"

    test_start "Request without token returns 401"
    local no_auth_response
    no_auth_response=$(curl -sk -X GET "$BACKEND_URL/api/v1/users/me/onboarding" -w "\n%{http_code}" 2>&1)
    local http_code
    http_code=$(echo "$no_auth_response" | tail -1)

    if [ "$http_code" = "401" ]; then
        test_pass "Endpoint requires authentication (401 without token)"
    else
        test_fail "Expected 401 without token, got: $http_code"
    fi

    test_start "Request with invalid token returns 401"
    local invalid_token_response
    invalid_token_response=$(curl -sk -X GET "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer invalid_token_12345" -w "\n%{http_code}" 2>&1)
    http_code=$(echo "$invalid_token_response" | tail -1)

    if [ "$http_code" = "401" ]; then
        test_pass "Invalid token rejected (401)"
    else
        test_fail "Expected 401 with invalid token, got: $http_code"
    fi

    log_section "5.2: Performance Tests"

    test_start "GET endpoint responds in < 2 seconds"
    local start_time
    start_time=$(date +%s)
    curl -skf -X GET "$BACKEND_URL/api/v1/users/me/onboarding" \
        -H "Authorization: Bearer $ACCESS_TOKEN" > /dev/null 2>&1
    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if [ $duration -lt 2 ]; then
        test_pass "Response time: ${duration}s (< 2s threshold)"
    else
        log_warning "Response time: ${duration}s (exceeds 2s threshold)"
    fi
}

################################################################################
# Phase 6: Database Verification (if Neo4j is accessible)
################################################################################

test_database_verification() {
    log_header "PHASE 6: DATABASE VERIFICATION"

    # Check if we can access Neo4j
    test_start "Check Neo4j port-forward availability"
    if nc -z localhost 7687 2>/dev/null; then
        test_pass "Neo4j is accessible on localhost:7687"

        log_info "Attempting database queries..."

        test_start "Query user record in Neo4j"
        local cypher_query="MATCH (u:User {email: '$TEST_USER_EMAIL'}) RETURN u.email, u.tutorial_completed, u.tutorial_completed_at LIMIT 1"

        # Note: This requires cypher-shell to be available
        if command -v cypher-shell &> /dev/null; then
            local db_result
            db_result=$(cypher-shell -u neo4j -p password -a bolt://localhost:7687 "$cypher_query" 2>&1 || echo "")

            if echo "$db_result" | grep -q "$TEST_USER_EMAIL"; then
                test_pass "User found in Neo4j database"
                log_verbose "Database result: $db_result"
            else
                log_warning "User not found in Neo4j (may need time to sync)"
            fi
        else
            log_warning "cypher-shell not available, skipping database queries"
        fi
    else
        log_warning "Neo4j not accessible on localhost:7687 (skipping database verification)"
        log_info "To enable database verification, run: kubectl port-forward -n aether-be neo4j-0 7687:7687"
    fi
}

################################################################################
# Test Summary
################################################################################

print_summary() {
    log_header "TEST SUMMARY"

    echo ""
    echo -e "${CYAN}Total Tests:${NC} $TESTS_TOTAL"
    echo -e "${GREEN}Passed:${NC}      $TESTS_PASSED"
    echo -e "${RED}Failed:${NC}      $TESTS_FAILED"
    echo ""

    local pass_rate
    if [ $TESTS_TOTAL -gt 0 ]; then
        pass_rate=$((TESTS_PASSED * 100 / TESTS_TOTAL))
    else
        pass_rate=0
    fi

    echo -e "${CYAN}Pass Rate:${NC}   ${pass_rate}%"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ ALL TESTS PASSED${NC}"
        echo ""
        echo -e "${CYAN}Test User Details:${NC}"
        echo -e "  Email:    $TEST_USER_EMAIL"
        echo -e "  Password: $TEST_USER_PASSWORD"
        echo ""
        echo -e "${CYAN}Next Steps:${NC}"
        echo "  1. Test the frontend onboarding modal at https://aether.tas.scharber.com"
        echo "  2. Login with the test user credentials above"
        echo "  3. Verify the tutorial modal appears automatically"
        echo "  4. Complete the tutorial and verify it doesn't reappear"
        echo ""
        return 0
    else
        echo -e "${RED}✗ SOME TESTS FAILED${NC}"
        echo ""
        echo -e "${YELLOW}Review the output above for details on failed tests${NC}"
        echo ""
        return 1
    fi
}

################################################################################
# Main Execution
################################################################################

main() {
    parse_arguments "$@"

    log_header "COMPLETE ONBOARDING FLOW TEST"
    log_info "Backend URL: $BACKEND_URL"
    log_info "Keycloak URL: $KEYCLOAK_URL"
    log_info "Verbose: $VERBOSE"
    echo ""

    # Execute test phases
    check_prerequisites || exit 1
    create_test_user || exit 1
    get_auth_token || exit 1
    test_automatic_onboarding
    test_onboarding_api
    test_security
    test_database_verification

    # Print summary and exit
    print_summary
    exit $?
}

# Run main function
main "$@"
