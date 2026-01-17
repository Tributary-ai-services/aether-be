# Onboarding Refactor - Testing Documentation

## Overview

This directory contains comprehensive testing documentation and utilities for the onboarding/tutorial tracking feature refactor.

**Feature Summary:**
Moved tutorial completion tracking from browser localStorage to Neo4j database backend with REST API endpoints.

**Implementation Date:** 2025-12-25
**Version:** 1.0.0

---

## Quick Start

### 1. Run Backend API Tests
```bash
cd /home/jscharber/eng/TAS/aether-be

# Get JWT token from browser (see instructions below)
TOKEN="<paste-token-here>"

# Run automated test script
./scripts/test-onboarding-api.sh https://aether.tas.scharber.com/api/v1 "$TOKEN"
```

### 2. Verify Database State
```bash
# Port forward Neo4j browser
kubectl port-forward -n aether-be svc/neo4j 7474:7474

# Open browser: http://localhost:7474
# Username: neo4j
# Password: password

# Run queries from: docs/neo4j-onboarding-queries.md
```

### 3. Manual Frontend Testing
Follow the comprehensive checklist in `docs/ONBOARDING_TESTING_CHECKLIST.md`

---

## Documentation Files

| File | Purpose |
|------|---------|
| `TESTING_README.md` | This file - overview and quick start |
| `ONBOARDING_TESTING_CHECKLIST.md` | Comprehensive 63-point manual testing checklist |
| `neo4j-onboarding-queries.md` | Cypher queries for database verification and analysis |
| `../scripts/test-onboarding-api.sh` | Automated backend API test script |

---

## Testing Phases

### Phase 1: Backend API Testing ✅
**Status:** Test script created
**Location:** `/home/jscharber/eng/TAS/aether-be/scripts/test-onboarding-api.sh`

**Tests:**
- GET `/api/v1/users/me/onboarding` - Fetch status
- POST `/api/v1/users/me/onboarding` - Mark complete
- DELETE `/api/v1/users/me/onboarding` - Reset tutorial

**How to Run:**
1. Obtain JWT token (see "Getting a JWT Token" below)
2. Execute test script:
   ```bash
   ./scripts/test-onboarding-api.sh https://aether.tas.scharber.com/api/v1 "$TOKEN"
   ```
3. Review output - all 5 tests should pass

### Phase 2: Database Verification ✅
**Status:** Query document created
**Location:** `/home/jscharber/eng/TAS/aether-be/docs/neo4j-onboarding-queries.md`

**Key Queries:**
- Check all users' onboarding status
- Count users by completion status
- Find users needing migration (null values)
- Verify data consistency
- Performance profiling

**How to Run:**
1. Port forward Neo4j: `kubectl port-forward -n aether-be svc/neo4j 7474:7474`
2. Open browser: http://localhost:7474
3. Login: neo4j / password
4. Copy/paste queries from documentation

### Phase 3: Frontend Integration Testing ⏳
**Status:** Checklist created
**Location:** `/home/jscharber/eng/TAS/aether-be/docs/ONBOARDING_TESTING_CHECKLIST.md`

**Test Scenarios:**
- New user first-time experience (auto-trigger)
- Existing user with completed tutorial (no auto-trigger)
- Tutorial completion flow
- Tutorial reset functionality
- Multi-browser/device synchronization
- Error handling when backend unavailable

**How to Run:**
Follow step-by-step instructions in the checklist document.

### Phase 4: Performance Testing ⏳
**Covered in:** ONBOARDING_TESTING_CHECKLIST.md (Phase 4)

**Metrics to Verify:**
- API response times < 500ms
- Database query times < 100ms
- No UI flicker during loading
- Smooth auto-trigger (1-second delay)

### Phase 5: Security Testing ⏳
**Covered in:** ONBOARDING_TESTING_CHECKLIST.md (Phase 5)

**Tests:**
- Authentication required for all endpoints
- Users can only access their own data
- Token validation (expired, invalid, missing)
- Input validation and injection prevention

### Phase 6: Cross-Browser Testing ⏳
**Covered in:** ONBOARDING_TESTING_CHECKLIST.md (Phase 6)

**Browsers:**
- Chrome (latest)
- Firefox (latest)
- Safari (latest)
- Edge (latest)
- Mobile browsers (iOS Safari, Chrome Mobile)

---

## Getting a JWT Token

Since the automated token script is currently not working, use this manual process:

### Option 1: Browser Developer Tools (Easiest)
1. Open https://aether.tas.scharber.com in browser
2. Log in with valid credentials
3. Open Developer Tools (F12)
4. Go to **Application** or **Storage** tab
5. Look under **Local Storage** or **Session Storage**
6. Find key that contains token (might be `token`, `access_token`, `auth_token`)
7. Copy the token value

### Option 2: Network Tab
1. Open https://aether.tas.scharber.com in browser
2. Open Developer Tools (F12) → **Network** tab
3. Log in
4. Find the login/auth request
5. Look in **Response** tab for `access_token`
6. Copy the token value

### Option 3: Console Inspection
1. Log in to https://aether.tas.scharber.com
2. Open Developer Tools (F12) → **Console** tab
3. Type: `localStorage` and press Enter
4. Look for token in the output
5. Or try: `sessionStorage`

### Option 4: Keycloak Direct Grant (Advanced)
```bash
curl -X POST \
  https://keycloak.tas.scharber.com/realms/aether/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password" \
  -d "client_id=aether-frontend" \
  -d "username=john@scharber.com" \
  -d "password=test123" | jq -r '.access_token'
```

**Note:** This requires the Keycloak client to allow direct access grants.

---

## Implementation Summary

### Backend Changes

**Files Modified:**
- `/home/jscharber/eng/TAS/aether-be/internal/models/user.go`
  - Added `TutorialCompleted` (boolean) field
  - Added `TutorialCompletedAt` (timestamp) field
  - Added `OnboardingStatusResponse` model

- `/home/jscharber/eng/TAS/aether-be/internal/services/user.go`
  - Added `GetOnboardingStatus()` method
  - Added `MarkTutorialComplete()` method
  - Added `ResetTutorial()` method

- `/home/jscharber/eng/TAS/aether-be/internal/handlers/user.go`
  - Added `GetOnboardingStatus` handler (GET)
  - Added `MarkTutorialComplete` handler (POST)
  - Added `ResetTutorial` handler (DELETE)

- `/home/jscharber/eng/TAS/aether-be/internal/handlers/routes.go`
  - Added routes for onboarding endpoints

### Frontend Changes

**Files Modified:**
- `/home/jscharber/eng/TAS/aether/src/services/api.js`
  - Added `onboarding.getStatus()` method
  - Added `onboarding.markTutorialComplete()` method
  - Added `onboarding.resetTutorial()` method

- `/home/jscharber/eng/TAS/aether/src/hooks/useOnboarding.js`
  - Removed all localStorage logic
  - Added API integration for status fetching
  - Added `isLoading` and `error` states
  - Made `markComplete` and `resetOnboarding` async with API calls

- `/home/jscharber/eng/TAS/aether/src/App.tsx`
  - Added loading state check before auto-triggering onboarding
  - Prevents premature modal display

### Database Schema

**Neo4j User Node - New Fields:**
```cypher
(:User {
  ...existing fields...
  tutorial_completed: boolean,
  tutorial_completed_at: datetime | null
})
```

**Default Values for New Users:**
- `tutorial_completed`: false
- `tutorial_completed_at`: null

---

## Common Testing Scenarios

### Scenario 1: Test Complete Workflow
```bash
# 1. Get initial status (should be incomplete for new user)
curl -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding

# 2. Mark tutorial complete
curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding

# 3. Verify completion
curl -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding

# 4. Reset tutorial
curl -X DELETE \
     -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding

# 5. Verify reset
curl -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding
```

### Scenario 2: Check Database for Specific User
```cypher
// In Neo4j Browser (http://localhost:7474)
MATCH (u:User {email: 'john@scharber.com'})
RETURN u.id,
       u.email,
       u.tutorial_completed,
       u.tutorial_completed_at,
       u.created_at;
```

### Scenario 3: Frontend Auto-Trigger Test
1. Clear all browser data (or use incognito mode)
2. Navigate to https://aether.tas.scharber.com
3. Log in with user who has `tutorial_completed: false`
4. Wait 1 second
5. Onboarding modal should automatically appear
6. Complete tutorial
7. Modal should close
8. Refresh page
9. Modal should NOT reappear

---

## Migration Considerations

### Existing Users

If there are users in the database created before this refactor, they may have `null` values for `tutorial_completed`.

**Check for Migration Need:**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
RETURN count(u) AS users_needing_migration;
```

**Migration Options:**

**Option A: Show tutorial to all existing users (conservative)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
SET u.tutorial_completed = false,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

**Option B: Skip tutorial for existing users (user-friendly)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
SET u.tutorial_completed = true,
    u.tutorial_completed_at = u.created_at,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

**Option C: Age-based (users > 7 days old skip tutorial)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
WITH u,
     CASE
       WHEN duration.between(u.created_at, datetime()).days > 7
       THEN true
       ELSE false
     END AS should_skip
SET u.tutorial_completed = should_skip,
    u.tutorial_completed_at = CASE
      WHEN should_skip THEN u.created_at
      ELSE null
    END,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

**Recommendation:** Use Option B or C for better user experience.

---

## Troubleshooting

### Issue: API Endpoint Returns 401 Unauthorized
**Cause:** Invalid or expired JWT token
**Solution:**
- Get fresh token from browser
- Verify token is not expired (JWT tokens typically expire after 5-15 minutes)
- Check token includes `Bearer ` prefix in Authorization header

### Issue: Frontend Doesn't Auto-Trigger Tutorial
**Possible Causes:**
1. User already completed tutorial (check database)
2. API request failed (check browser console)
3. Loading state race condition

**Debug Steps:**
1. Open browser console (F12)
2. Check for API errors
3. Look for network requests to `/api/v1/users/me/onboarding`
4. Verify response shows `should_auto_trigger: true`

### Issue: Tutorial Completion Not Persisting
**Possible Causes:**
1. POST request failing
2. Database update failing
3. Frontend not calling API

**Debug Steps:**
1. Open browser Developer Tools → Network tab
2. Complete tutorial
3. Look for POST request to `/users/me/onboarding`
4. Check response status (should be 200)
5. Query database to verify update

### Issue: Neo4j Connection Failed
**Solution:**
```bash
# Check Neo4j pod is running
kubectl get pods -n aether-be -l app=neo4j

# Check Neo4j logs
kubectl logs -n aether-be neo4j-0

# Restart Neo4j if needed
kubectl delete pod -n aether-be neo4j-0
```

---

## Performance Benchmarks

**Expected Performance (Target):**

| Operation | Target | Acceptable |
|-----------|--------|------------|
| GET onboarding status | < 200ms | < 500ms |
| POST mark complete | < 500ms | < 1s |
| DELETE reset tutorial | < 500ms | < 1s |
| Neo4j query (user lookup) | < 50ms | < 100ms |
| Neo4j query (status update) | < 100ms | < 200ms |
| Frontend API call | < 300ms | < 800ms |

**Measurement Commands:**
```bash
# API response time
time curl -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding

# Neo4j query performance
PROFILE
MATCH (u:User {id: $user_id})
RETURN u.tutorial_completed, u.tutorial_completed_at;
```

---

## Support and Questions

- **Backend Issues:** Check `/home/jscharber/eng/TAS/aether-be/internal/handlers/user.go`
- **Frontend Issues:** Check `/home/jscharber/eng/TAS/aether/src/hooks/useOnboarding.js`
- **Database Issues:** Check `/home/jscharber/eng/TAS/aether-be/docs/neo4j-onboarding-queries.md`
- **API Documentation:** Check `/home/jscharber/eng/TAS/aether-be/CLAUDE.md`

---

## Testing Completion Checklist

Before marking this feature as production-ready:

- [ ] Backend API test script passes all 5 tests
- [ ] Database verification queries show consistent data
- [ ] Manual frontend testing checklist 100% complete (63/63 tests passed)
- [ ] Performance benchmarks meet targets
- [ ] Security testing passed (authentication/authorization verified)
- [ ] Cross-browser testing passed (Chrome, Firefox, Safari, Edge)
- [ ] Migration plan executed (if needed for existing users)
- [ ] Documentation reviewed and updated
- [ ] Code reviewed by team member
- [ ] Deployed to production

**Current Status:** ⏳ In Testing

---

## Deployment Notes

**Backend Deployment:**
- Image: `registry-api.tas.scharber.com/aether-backend:latest`
- Namespace: `aether-be`
- Pods: 2/2 running
- Deployed: 2025-12-25

**Frontend Deployment:**
- Image: `registry-api.tas.scharber.com/aether-frontend:latest`
- Namespace: `aether-be`
- Pods: 1/1 running
- Deployed: 2025-12-25

**Rollback Plan:**
If critical issues are found:
1. Revert backend: `kubectl rollout undo deployment/aether-backend -n aether-be`
2. Revert frontend: `kubectl rollout undo deployment/aether-frontend -n aether-be`
3. Database changes are non-destructive - no rollback needed

---

*Last Updated: 2025-12-25*
*Version: 1.0.0*
*Feature: Onboarding Refactor - localStorage to Neo4j Migration*
