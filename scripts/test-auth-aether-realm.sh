#!/bin/bash
# Test authentication with aether realm after fixing realm mismatch

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

BACKEND_URL="${BACKEND_URL:-https://aether.tas.scharber.com/api/v1}"
KEYCLOAK_URL="${KEYCLOAK_URL:-https://keycloak.tas.scharber.com}"
USERNAME="john@scharber.com"
PASSWORD="test123"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Testing Authentication - Aether Realm${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Step 1: Get JWT token from aether realm
echo -e "${BLUE}Step 1: Getting JWT token for ${USERNAME} from aether realm${NC}"
TOKEN_RESPONSE=$(curl -s -k -X POST "${KEYCLOAK_URL}/realms/aether/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "client_id=admin-cli" \
  -d "username=${USERNAME}" \
  -d "password=${PASSWORD}" \
  -d "grant_type=password")

if [ $? -ne 0 ]; then
  echo -e "${RED}✗ Failed to get token from Keycloak${NC}"
  exit 1
fi

ACCESS_TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.access_token // empty')

if [ -z "$ACCESS_TOKEN" ] || [ "$ACCESS_TOKEN" = "null" ]; then
  echo -e "${RED}✗ Failed to extract access token${NC}"
  echo "Token Response: $TOKEN_RESPONSE"
  exit 1
fi

echo -e "${GREEN}✓ Successfully obtained JWT token from aether realm${NC}"
echo "Token (first 50 chars): ${ACCESS_TOKEN:0:50}..."

# Decode token to check issuer
ISSUER=$(echo "$ACCESS_TOKEN" | awk -F. '{print $2}' | base64 -d 2>/dev/null | jq -r '.iss // empty')
echo -e "${BLUE}Token Issuer: ${ISSUER}${NC}"

if [[ "$ISSUER" == *"/realms/aether" ]]; then
  echo -e "${GREEN}✓ Token issued by aether realm${NC}"
else
  echo -e "${RED}✗ WARNING: Token NOT from aether realm!${NC}"
  echo -e "${RED}  Issuer: ${ISSUER}${NC}"
fi
echo ""

# Step 2: Test /users/me endpoint
echo -e "${BLUE}Step 2: Testing /users/me endpoint${NC}"
USER_RESPONSE=$(curl -s -k -X GET "${BACKEND_URL}/users/me" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "Content-Type: application/json")

if [ $? -ne 0 ]; then
  echo -e "${RED}✗ Failed to get user profile${NC}"
  exit 1
fi

# Check if response contains error
if echo "$USER_RESPONSE" | jq -e '.code == "UNAUTHORIZED"' > /dev/null 2>&1; then
  echo -e "${RED}✗ Backend rejected token with UNAUTHORIZED${NC}"
  echo "Response: $USER_RESPONSE"
  exit 1
fi

USER_ID=$(echo "$USER_RESPONSE" | jq -r '.id // empty')
PERSONAL_SPACE_ID=$(echo "$USER_RESPONSE" | jq -r '.personal_space_id // empty')

if [ -z "$USER_ID" ] || [ "$USER_ID" = "null" ]; then
  echo -e "${RED}✗ Failed to get user ID${NC}"
  echo "User Response: $USER_RESPONSE"
  exit 1
fi

echo -e "${GREEN}✓ Successfully authenticated with backend${NC}"
echo -e "${GREEN}  User ID: ${USER_ID}${NC}"
echo -e "${GREEN}  Personal Space ID: ${PERSONAL_SPACE_ID}${NC}"
echo ""

# Step 3: Query notebooks
echo -e "${BLUE}Step 3: Querying notebooks for user${NC}"
NOTEBOOKS_RESPONSE=$(curl -s -k -X GET "${BACKEND_URL}/notebooks/search?query=Getting%20Started" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "X-Space-ID: ${PERSONAL_SPACE_ID}")

if [ $? -ne 0 ]; then
  echo -e "${RED}✗ Failed to query notebooks${NC}"
  exit 1
fi

NOTEBOOK_COUNT=$(echo "$NOTEBOOKS_RESPONSE" | jq -r '.total // 0')
echo -e "${YELLOW}Found ${NOTEBOOK_COUNT} notebook(s) matching 'Getting Started'${NC}"

if [ "$NOTEBOOK_COUNT" -eq 0 ]; then
  echo -e "${YELLOW}⚠ No 'Getting Started' notebook found${NC}"
  echo -e "${YELLOW}  User may need onboarding to be run${NC}"
  GETTING_STARTED_ID=""
else
  GETTING_STARTED_ID=$(echo "$NOTEBOOKS_RESPONSE" | jq -r '.notebooks[0].id // empty')
  GETTING_STARTED_NAME=$(echo "$NOTEBOOKS_RESPONSE" | jq -r '.notebooks[0].name // empty')
  echo -e "${GREEN}✓ Getting Started Notebook: ${GETTING_STARTED_ID}${NC}"
  echo -e "${GREEN}  Name: ${GETTING_STARTED_NAME}${NC}"
fi
echo ""

# Step 4: If notebook exists, check for documents
if [ -n "$GETTING_STARTED_ID" ] && [ "$GETTING_STARTED_ID" != "null" ]; then
  echo -e "${BLUE}Step 4: Checking documents in 'Getting Started' notebook${NC}"

  DOCS_RESPONSE=$(curl -s -k -X GET "${BACKEND_URL}/documents?notebook_id=${GETTING_STARTED_ID}" \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "X-Space-ID: ${PERSONAL_SPACE_ID}")

  if [ $? -ne 0 ]; then
    echo -e "${RED}✗ Failed to query documents${NC}"
  else
    DOC_COUNT=$(echo "$DOCS_RESPONSE" | jq -r '.total // 0')
    echo -e "${YELLOW}Found ${DOC_COUNT} document(s) in 'Getting Started' notebook${NC}"

    if [ "$DOC_COUNT" -eq 0 ]; then
      echo -e "${YELLOW}⚠ No documents found - onboarding may be incomplete${NC}"
    else
      echo -e "${GREEN}✓ Documents found:${NC}"
      echo "$DOCS_RESPONSE" | jq -r '.documents[]? | "  - \(.name) (\(.type))"'
    fi
  fi
  echo ""
fi

# Summary
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Realm: ${GREEN}aether${NC}"
echo "User: ${USERNAME}"
echo "User ID: ${USER_ID}"
echo "Personal Space ID: ${PERSONAL_SPACE_ID}"
echo "Token Issuer: ${ISSUER}"
echo "Getting Started Notebook: $([ -n "$GETTING_STARTED_ID" ] && echo "Found ($GETTING_STARTED_ID)" || echo "NOT FOUND")"
echo "Documents Count: ${DOC_COUNT:-0}"
echo ""

if [[ "$ISSUER" == *"/realms/aether" ]] && [ -n "$USER_ID" ] && [ "$USER_ID" != "null" ]; then
  echo -e "${GREEN}========================================${NC}"
  echo -e "${GREEN}✓ ALL TESTS PASSED${NC}"
  echo -e "${GREEN}========================================${NC}"
  echo ""
  echo -e "${GREEN}Authentication is working correctly with aether realm!${NC}"
  exit 0
else
  echo -e "${RED}========================================${NC}"
  echo -e "${RED}✗ TESTS FAILED${NC}"
  echo -e "${RED}========================================${NC}"
  exit 1
fi
