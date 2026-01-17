#!/bin/bash

################################################################################
# Keycloak Test User Creation Script
#
# This script creates test users in Keycloak for onboarding flow testing.
# Users are created in the "aether" realm with the "user" role assigned.
#
# Usage:
#   ./create-test-user.sh [--email EMAIL] [--password PASSWORD] [--keycloak-url URL]
#
# Environment Variables:
#   KEYCLOAK_URL      - Keycloak server URL (default: https://keycloak.tas.scharber.com)
#   KEYCLOAK_ADMIN    - Admin username (default: admin)
#   KEYCLOAK_PASSWORD - Admin password (default: admin123)
################################################################################

set -e  # Exit on error
set -u  # Exit on undefined variable

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
KEYCLOAK_URL="${KEYCLOAK_URL:-https://keycloak.tas.scharber.com}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-admin123}"
REALM_NAME="aether"

# Default test user configuration
TEST_USER_EMAIL=""
TEST_USER_PASSWORD="Test123!"
TEST_USER_FIRST_NAME="Test"
TEST_USER_LAST_NAME="User"
AUTO_GENERATE=true

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --email)
            TEST_USER_EMAIL="$2"
            AUTO_GENERATE=false
            shift 2
            ;;
        --password)
            TEST_USER_PASSWORD="$2"
            shift 2
            ;;
        --keycloak-url)
            KEYCLOAK_URL="$2"
            shift 2
            ;;
        --first-name)
            TEST_USER_FIRST_NAME="$2"
            shift 2
            ;;
        --last-name)
            TEST_USER_LAST_NAME="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--email EMAIL] [--password PASSWORD] [--keycloak-url URL]"
            echo ""
            echo "Options:"
            echo "  --email EMAIL         Test user email (auto-generated if not provided)"
            echo "  --password PASSWORD   Test user password (default: Test123!)"
            echo "  --first-name NAME     Test user first name (default: Test)"
            echo "  --last-name NAME      Test user last name (default: User)"
            echo "  --keycloak-url URL    Keycloak URL (default: https://keycloak.tas.scharber.com)"
            echo "  --help                Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

################################################################################
# Helper Functions
################################################################################

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Generate auto email if not provided
generate_email() {
    if [ -z "$TEST_USER_EMAIL" ]; then
        local timestamp=$(date +%s)
        TEST_USER_EMAIL="test-user-${timestamp}@example.com"
        log_info "Auto-generated email: $TEST_USER_EMAIL"
    fi
}

# Wait for Keycloak to be ready
wait_for_keycloak() {
    log_info "Waiting for Keycloak to be ready at $KEYCLOAK_URL..."
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -skf "$KEYCLOAK_URL/health/ready" > /dev/null 2>&1 || curl -skf "$KEYCLOAK_URL/" > /dev/null 2>&1; then
            log_success "Keycloak is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        echo -n "."
        sleep 2
    done

    log_error "Keycloak did not become ready after $max_attempts attempts"
    return 1
}

# Get admin access token
get_admin_token() {
    log_info "Obtaining admin access token..."

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "username=$KEYCLOAK_ADMIN" \
        -d "password=$KEYCLOAK_PASSWORD" \
        -d "grant_type=password" \
        -d "client_id=admin-cli" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to obtain admin token"
        log_error "Response: $response"
        return 1
    fi

    ADMIN_TOKEN=$(echo "$response" | grep -o '"access_token":"[^"]*' | sed 's/"access_token":"//')

    if [ -z "$ADMIN_TOKEN" ]; then
        log_error "Failed to extract access token from response"
        return 1
    fi

    log_success "Admin token obtained"
}

# Create test user
create_test_user() {
    log_info "Creating test user: $TEST_USER_EMAIL..."

    # Check if user already exists
    local user_check
    user_check=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/users?email=$TEST_USER_EMAIL" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    if [ $? -eq 0 ] && echo "$user_check" | grep -q "\"email\":\"$TEST_USER_EMAIL\""; then
        log_warning "User $TEST_USER_EMAIL already exists in realm $REALM_NAME"
        log_info "Fetching existing user ID..."

        USER_ID=$(echo "$user_check" | grep -o '"id":"[^"]*' | head -1 | sed 's/"id":"//')

        if [ -z "$USER_ID" ]; then
            log_error "Failed to extract user ID from existing user"
            return 1
        fi

        log_info "Existing user ID: $USER_ID"
        return 0
    fi

    # Create user
    local user_config
    user_config=$(cat <<EOF
{
  "username": "$TEST_USER_EMAIL",
  "email": "$TEST_USER_EMAIL",
  "firstName": "$TEST_USER_FIRST_NAME",
  "lastName": "$TEST_USER_LAST_NAME",
  "enabled": true,
  "emailVerified": true,
  "credentials": [{
    "type": "password",
    "value": "$TEST_USER_PASSWORD",
    "temporary": false
  }]
}
EOF
    )

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/users" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$user_config" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to create user"
        log_error "Response: $response"
        return 1
    fi

    log_success "User $TEST_USER_EMAIL created successfully"

    # Get created user ID
    local user_data
    user_data=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/users?email=$TEST_USER_EMAIL" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    USER_ID=$(echo "$user_data" | grep -o '"id":"[^"]*' | head -1 | sed 's/"id":"//')

    if [ -z "$USER_ID" ]; then
        log_error "Failed to get created user ID"
        return 1
    fi

    log_info "Created user ID: $USER_ID"
}

# Assign user role
assign_user_role() {
    log_info "Assigning 'user' role to $TEST_USER_EMAIL..."

    # Get role representation
    local role_data
    role_data=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles/user" \
        -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)

    if [ $? -ne 0 ]; then
        log_warning "Failed to get 'user' role. It may not exist yet."
        log_info "Creating 'user' role..."

        local role_config
        role_config=$(cat <<EOF
{
  "name": "user",
  "description": "Default user role for Aether platform",
  "composite": false,
  "clientRole": false
}
EOF
        )

        curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles" \
            -H "Authorization: Bearer $ADMIN_TOKEN" \
            -H "Content-Type: application/json" \
            -d "$role_config" > /dev/null 2>&1

        # Get role data again
        role_data=$(curl -skf -X GET "$KEYCLOAK_URL/admin/realms/$REALM_NAME/roles/user" \
            -H "Authorization: Bearer $ADMIN_TOKEN" 2>&1)
    fi

    # Assign role to user
    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/users/$USER_ID/role-mappings/realm" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H "Content-Type: application/json" \
        -d "[$role_data]" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to assign role to user"
        log_error "Response: $response"
        return 1
    fi

    log_success "Role 'user' assigned to $TEST_USER_EMAIL"
}

################################################################################
# Main Execution
################################################################################

main() {
    log_info "=== Keycloak Test User Creation ==="
    log_info "Keycloak URL: $KEYCLOAK_URL"
    log_info "Realm: $REALM_NAME"
    echo ""

    # Generate email if needed
    generate_email

    # Execute steps
    wait_for_keycloak || exit 1
    get_admin_token || exit 1
    create_test_user || exit 1
    assign_user_role || exit 1

    echo ""
    log_success "=== Test User Created Successfully ==="
    echo ""
    echo -e "${GREEN}Email/Username:${NC} $TEST_USER_EMAIL"
    echo -e "${GREEN}Password:${NC} $TEST_USER_PASSWORD"
    echo -e "${GREEN}User ID:${NC} $USER_ID"
    echo -e "${GREEN}Realm:${NC} $REALM_NAME"
    echo ""
    log_info "You can now use these credentials to test the onboarding flow"
    echo ""
}

# Run main function
main
