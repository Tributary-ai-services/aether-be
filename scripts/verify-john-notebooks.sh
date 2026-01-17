#!/bin/bash
# Script to verify john@scharber.com's notebooks and trigger onboarding if needed

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

BACKEND_URL="${BACKEND_URL:-https://aether.tas.scharber.com/api/v1}"
KEYCLOAK_URL="${KEYCLOAK_URL:-https://keycloak.tas.scharber.com}"
USERNAME="john@scharber.com"
PASSWORD="test123"

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Verifying john@scharber.com's Onboarding Resources${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Step 1: Get JWT token for john@scharber.com
echo -e "${BLUE}Step 1: Getting JWT token for john@scharber.com${NC}"
TOKEN_RESPONSE=$(curl -s -X POST "${KEYCLOAK_URL}/realms/master/protocol/openid-connect/token" \
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

echo -e "${GREEN}✓ Successfully obtained JWT token${NC}"
echo ""

# Step 2: Get user profile
echo -e "${BLUE}Step 2: Getting user profile${NC}"
USER_RESPONSE=$(curl -s -X GET "${BACKEND_URL}/users/me" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "Content-Type: application/json")

if [ $? -ne 0 ]; then
  echo -e "${RED}✗ Failed to get user profile${NC}"
  exit 1
fi

USER_ID=$(echo "$USER_RESPONSE" | jq -r '.id // empty')
PERSONAL_SPACE_ID=$(echo "$USER_RESPONSE" | jq -r '.personal_space_id // empty')

if [ -z "$USER_ID" ] || [ "$USER_ID" = "null" ]; then
  echo -e "${RED}✗ Failed to get user ID${NC}"
  echo "User Response: $USER_RESPONSE"
  exit 1
fi

echo -e "${GREEN}✓ User ID: ${USER_ID}${NC}"
echo -e "${GREEN}✓ Personal Space ID: ${PERSONAL_SPACE_ID}${NC}"
echo ""

# Step 3: Query notebooks
echo -e "${BLUE}Step 3: Querying notebooks for user${NC}"
NOTEBOOKS_RESPONSE=$(curl -s -X GET "${BACKEND_URL}/notebooks/search?query=Getting%20Started" \
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
  echo -e "${YELLOW}  This indicates onboarding was not completed or failed${NC}"
  GETTING_STARTED_ID=""
else
  GETTING_STARTED_ID=$(echo "$NOTEBOOKS_RESPONSE" | jq -r '.notebooks[0].id // empty')
  GETTING_STARTED_NAME=$(echo "$NOTEBOOKS_RESPONSE" | jq -r '.notebooks[0].name // empty')
  echo -e "${GREEN}✓ Getting Started Notebook: ${GETTING_STARTED_ID}${NC}"
  echo -e "${GREEN}  Name: ${GETTING_STARTED_NAME}${NC}"
fi
echo ""

# Step 4: If Getting Started notebook exists, check for documents
if [ -n "$GETTING_STARTED_ID" ] && [ "$GETTING_STARTED_ID" != "null" ]; then
  echo -e "${BLUE}Step 4: Checking documents in 'Getting Started' notebook${NC}"

  DOCS_RESPONSE=$(curl -s -X GET "${BACKEND_URL}/documents?notebook_id=${GETTING_STARTED_ID}" \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "X-Space-ID: ${PERSONAL_SPACE_ID}")

  if [ $? -ne 0 ]; then
    echo -e "${RED}✗ Failed to query documents${NC}"
  else
    DOC_COUNT=$(echo "$DOCS_RESPONSE" | jq -r '.total // 0')
    echo -e "${YELLOW}Found ${DOC_COUNT} document(s) in 'Getting Started' notebook${NC}"

    if [ "$DOC_COUNT" -eq 0 ]; then
      echo -e "${YELLOW}⚠ No documents found - onboarding incomplete${NC}"
    else
      echo -e "${GREEN}✓ Documents found:${NC}"
      echo "$DOCS_RESPONSE" | jq -r '.documents[]? | "  - \(.name) (\(.type))"'
    fi
  fi
  echo ""
fi

# Step 5: Summary
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Summary${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""
echo "User: ${USERNAME}"
echo "User ID: ${USER_ID}"
echo "Personal Space ID: ${PERSONAL_SPACE_ID}"
echo "Getting Started Notebook: $([ -n "$GETTING_STARTED_ID" ] && echo "Found ($GETTING_STARTED_ID)" || echo "NOT FOUND")"
echo "Documents Count: ${DOC_COUNT:-0}"
echo ""

# Recommendations
if [ -z "$GETTING_STARTED_ID" ] || [ "$GETTING_STARTED_ID" = "null" ]; then
  echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
  echo -e "${YELLOW}  Recommendation${NC}"
  echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
  echo ""
  echo -e "${YELLOW}The user john@scharber.com is missing onboarding resources.${NC}"
  echo -e "${YELLOW}Since onboarding is now synchronous, this means:${NC}"
  echo -e "${YELLOW}  1. The user was created before the fix${NC}"
  echo -e "${YELLOW}  2. The old async onboarding failed or didn't complete${NC}"
  echo ""
  echo -e "${YELLOW}Next steps:${NC}"
  echo -e "${YELLOW}  1. Create a manual onboarding endpoint${NC}"
  echo -e "${YELLOW}  2. OR create the resources manually for john${NC}"
  echo -e "${YELLOW}  3. OR delete john's account and recreate (new onboarding will run sync)${NC}"
  echo ""
elif [ "${DOC_COUNT:-0}" -eq 0 ]; then
  echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
  echo -e "${YELLOW}  Issue Found${NC}"
  echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
  echo ""
  echo -e "${YELLOW}The 'Getting Started' notebook exists but has no documents!${NC}"
  echo -e "${YELLOW}This indicates a partial onboarding failure.${NC}"
  echo ""
else
  echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
  echo -e "${GREEN}  All Checks Passed!${NC}"
  echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
  echo ""
  echo -e "${GREEN}john@scharber.com has a complete onboarding setup.${NC}"
  echo ""
fi
