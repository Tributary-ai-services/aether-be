#!/bin/bash
# Onboarding API End-to-End Test Script
# Tests the three onboarding endpoints: GET, POST, DELETE
#
# Usage: ./test-onboarding-api.sh [API_URL] [TOKEN]
#   API_URL: Base API URL (default: https://aether.tas.scharber.com/api/v1)
#   TOKEN: JWT Bearer token (if not provided, will attempt to get from get-token.sh)

set -e

# Configuration
API_URL="${1:-https://aether.tas.scharber.com/api/v1}"
TOKEN="${2:-}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
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

# Function to make API request and pretty print response
api_request() {
    local method="$1"
    local endpoint="$2"
    local description="$3"

    print_status "$description"
    echo "  Method: $method"
    echo "  URL: $API_URL$endpoint"

    response=$(curl -s -w "\n%{http_code}" -X "$method" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        "$API_URL$endpoint")

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    echo "  Status: $http_code"
    echo "  Response:"
    echo "$body" | jq '.' 2>/dev/null || echo "$body"

    if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
        print_success "Request successful"
        return 0
    else
        print_error "Request failed with status $http_code"
        return 1
    fi
}

# Get token if not provided
if [ -z "$TOKEN" ]; then
    print_warning "No token provided, attempting to get token..."
    if [ -f "/home/jscharber/eng/TAS/aether/get-token.sh" ]; then
        TOKEN=$(bash /home/jscharber/eng/TAS/aether/get-token.sh 2>/dev/null | jq -r '.access_token' 2>/dev/null)
        if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
            print_error "Failed to get token automatically"
            echo "Please provide a valid JWT token as the second argument"
            echo "Usage: $0 [API_URL] [TOKEN]"
            exit 1
        fi
        print_success "Token obtained successfully"
    else
        print_error "get-token.sh not found and no token provided"
        echo "Please provide a valid JWT token as the second argument"
        exit 1
    fi
fi

echo ""
print_status "Onboarding API End-to-End Test"
echo "  API URL: $API_URL"
echo "  Token: ${TOKEN:0:20}..."
echo ""

# Test Flow
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 1: Get Initial Onboarding Status (should show incomplete)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
api_request "GET" "/users/me/onboarding" "Fetching onboarding status"
initial_status=$?
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 2: Mark Tutorial as Complete"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
api_request "POST" "/users/me/onboarding" "Marking tutorial as complete"
mark_complete_status=$?
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 3: Get Onboarding Status (should show completed)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
api_request "GET" "/users/me/onboarding" "Fetching onboarding status after completion"
completed_status=$?
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 4: Reset Tutorial"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
api_request "DELETE" "/users/me/onboarding" "Resetting tutorial status"
reset_status=$?
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST 5: Get Onboarding Status (should show incomplete again)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
api_request "GET" "/users/me/onboarding" "Fetching onboarding status after reset"
final_status=$?
echo ""

# Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "TEST SUMMARY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

total_tests=5
passed_tests=0

[ $initial_status -eq 0 ] && ((passed_tests++)) && print_success "Test 1: Get initial status" || print_error "Test 1: Get initial status"
[ $mark_complete_status -eq 0 ] && ((passed_tests++)) && print_success "Test 2: Mark complete" || print_error "Test 2: Mark complete"
[ $completed_status -eq 0 ] && ((passed_tests++)) && print_success "Test 3: Get completed status" || print_error "Test 3: Get completed status"
[ $reset_status -eq 0 ] && ((passed_tests++)) && print_success "Test 4: Reset tutorial" || print_error "Test 4: Reset tutorial"
[ $final_status -eq 0 ] && ((passed_tests++)) && print_success "Test 5: Get reset status" || print_error "Test 5: Get reset status"

echo ""
if [ $passed_tests -eq $total_tests ]; then
    print_success "All tests passed! ($passed_tests/$total_tests)"
    exit 0
else
    print_error "Some tests failed ($passed_tests/$total_tests passed)"
    exit 1
fi
