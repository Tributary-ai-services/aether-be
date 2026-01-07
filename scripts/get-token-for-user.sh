#!/bin/bash

################################################################################
# Keycloak Token Retrieval Script for Any User
#
# This script retrieves JWT access tokens for any Keycloak user.
# Useful for testing with different user accounts.
#
# Usage:
#   ./get-token-for-user.sh <username> <password> [--keycloak-url URL] [--realm REALM]
#
# Examples:
#   ./get-token-for-user.sh test-user-1234567890@example.com Test123!
#   ./get-token-for-user.sh john@scharber.com test123 --realm aether
#
# Environment Variables:
#   KEYCLOAK_URL  - Keycloak server URL (default: https://keycloak.tas.scharber.com)
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
REALM_NAME="aether"
CLIENT_ID="aether-frontend"  # Public client for user authentication

# User credentials
USERNAME=""
PASSWORD=""
OUTPUT_FORMAT="json"  # json or token-only

################################################################################
# Helper Functions
################################################################################

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

show_usage() {
    echo "Usage: $0 <username> <password> [OPTIONS]"
    echo ""
    echo "Arguments:"
    echo "  username              Keycloak username/email"
    echo "  password              User password"
    echo ""
    echo "Options:"
    echo "  --keycloak-url URL    Keycloak URL (default: https://keycloak.tas.scharber.com)"
    echo "  --realm REALM         Realm name (default: aether)"
    echo "  --client-id ID        Client ID (default: aether-frontend)"
    echo "  --token-only          Output only the access token (no JSON)"
    echo "  --help                Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 test@example.com Test123!"
    echo "  $0 john@scharber.com test123 --token-only"
    echo "  TOKEN=\$($0 user@example.com pass123 --token-only)"
    exit 0
}

# Parse arguments
parse_arguments() {
    if [ $# -lt 2 ]; then
        log_error "Missing required arguments"
        echo ""
        show_usage
    fi

    USERNAME="$1"
    PASSWORD="$2"
    shift 2

    while [[ $# -gt 0 ]]; do
        case $1 in
            --keycloak-url)
                KEYCLOAK_URL="$2"
                shift 2
                ;;
            --realm)
                REALM_NAME="$2"
                shift 2
                ;;
            --client-id)
                CLIENT_ID="$2"
                shift 2
                ;;
            --token-only)
                OUTPUT_FORMAT="token-only"
                shift
                ;;
            --help)
                show_usage
                ;;
            *)
                log_error "Unknown option: $1"
                show_usage
                ;;
        esac
    done
}

# Get user access token
get_user_token() {
    log_info "Retrieving token for user: $USERNAME"
    log_info "Keycloak URL: $KEYCLOAK_URL"
    log_info "Realm: $REALM_NAME"
    log_info "Client ID: $CLIENT_ID"

    local response
    response=$(curl -skf -X POST "$KEYCLOAK_URL/realms/$REALM_NAME/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "username=$USERNAME" \
        -d "password=$PASSWORD" \
        -d "grant_type=password" \
        -d "client_id=$CLIENT_ID" 2>&1)

    if [ $? -ne 0 ]; then
        log_error "Failed to obtain token"
        log_error "Response: $response"
        log_error ""
        log_error "Possible reasons:"
        log_error "  - Invalid username or password"
        log_error "  - User does not exist in realm '$REALM_NAME'"
        log_error "  - Client '$CLIENT_ID' not configured for direct access grants"
        log_error "  - Keycloak server not accessible at $KEYCLOAK_URL"
        exit 1
    fi

    # Check if response contains access_token
    if ! echo "$response" | grep -q '"access_token"'; then
        log_error "Response does not contain access_token"
        log_error "Response: $response"
        exit 1
    fi

    log_success "Token retrieved successfully" >&2
    echo ""

    # Output based on format
    if [ "$OUTPUT_FORMAT" = "token-only" ]; then
        echo "$response" | grep -o '"access_token":"[^"]*' | sed 's/"access_token":"//'
    else
        echo "$response" | jq -r '.' 2>/dev/null || echo "$response"
    fi
}

################################################################################
# Main Execution
################################################################################

main() {
    parse_arguments "$@"
    get_user_token
}

# Run main function
main "$@"
