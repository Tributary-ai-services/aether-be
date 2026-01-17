# Onboarding Feature Testing Checklist

## Overview
This checklist provides a comprehensive testing procedure for the onboarding/tutorial refactor that moved tutorial tracking from localStorage to Neo4j database.

**Feature**: Tutorial completion tracking via backend API
**Endpoints**: GET/POST/DELETE `/api/v1/users/me/onboarding`
**Database**: Neo4j fields `tutorial_completed`, `tutorial_completed_at`

---

## Pre-Testing Setup

- [ ] Backend service running and healthy
  ```bash
  kubectl get pods -n aether-be -l app=aether-backend
  ```

- [ ] Frontend service running and healthy
  ```bash
  kubectl get pods -n aether-be -l app=aether-frontend
  ```

- [ ] Neo4j database accessible
  ```bash
  # Via Browser (HTTP interface)
  kubectl port-forward -n aether-be svc/neo4j 7474:7474
  # Browser: http://localhost:7474 (neo4j/password)

  # Via automated test script (recommended)
  bash /home/jscharber/eng/TAS/aether-be/scripts/test-onboarding-database.sh
  ```

- [ ] Valid JWT token available
  ```bash
  cd /home/jscharber/eng/TAS/aether
  ./get-token.sh
  ```

- [ ] Test environment backed up (if production)
  ```bash
  # Neo4j backup command here
  ```

---

## Phase 1: Backend API Testing

### 1.1 GET /users/me/onboarding

**Test: Fetch onboarding status for authenticated user**

- [ ] Request with valid token returns 200 OK
  ```bash
  curl -H "Authorization: Bearer $TOKEN" \
       https://aether.tas.scharber.com/api/v1/users/me/onboarding
  ```

- [ ] Response includes required fields:
  - [ ] `tutorial_completed` (boolean)
  - [ ] `tutorial_completed_at` (datetime or null)
  - [ ] `should_auto_trigger` (boolean)

- [ ] Request without token returns 401 Unauthorized
  ```bash
  curl https://aether.tas.scharber.com/api/v1/users/me/onboarding
  ```

- [ ] Request with invalid token returns 401 Unauthorized

- [ ] Response time < 500ms (performance check)

**Expected Results:**
- New user: `tutorial_completed: false`, `should_auto_trigger: true`
- Existing user with completed tutorial: `tutorial_completed: true`, `should_auto_trigger: false`

### 1.2 POST /users/me/onboarding

**Test: Mark tutorial as complete**

- [ ] Request with valid token returns 200 OK
  ```bash
  curl -X POST \
       -H "Authorization: Bearer $TOKEN" \
       https://aether.tas.scharber.com/api/v1/users/me/onboarding
  ```

- [ ] Response includes success message

- [ ] Subsequent GET request shows `tutorial_completed: true`

- [ ] Timestamp `tutorial_completed_at` is set to current time

- [ ] Idempotent: Calling POST multiple times doesn't cause errors

- [ ] Request without token returns 401 Unauthorized

**Expected Results:**
- Status changes from incomplete to complete
- Timestamp is recorded in Neo4j
- Can be called multiple times safely

### 1.3 DELETE /users/me/onboarding

**Test: Reset tutorial status**

- [ ] Request with valid token returns 200 OK
  ```bash
  curl -X DELETE \
       -H "Authorization: Bearer $TOKEN" \
       https://aether.tas.scharber.com/api/v1/users/me/onboarding
  ```

- [ ] Response includes success message

- [ ] Subsequent GET request shows `tutorial_completed: false`

- [ ] Timestamp `tutorial_completed_at` is null

- [ ] Idempotent: Calling DELETE on already-reset tutorial doesn't cause errors

- [ ] Request without token returns 401 Unauthorized

**Expected Results:**
- Status changes from complete to incomplete
- Timestamp is cleared
- Can be called multiple times safely

### 1.4 End-to-End API Flow

- [ ] Run automated test script
  ```bash
  cd /home/jscharber/eng/TAS/aether-be
  ./scripts/test-onboarding-api.sh
  ```

- [ ] All 5 tests pass:
  - [ ] GET initial status (incomplete)
  - [ ] POST mark complete
  - [ ] GET updated status (completed)
  - [ ] DELETE reset
  - [ ] GET final status (incomplete again)

---

## Phase 2: Database Verification

### 2.1 Neo4j Schema Check

- [ ] Tutorial fields exist on User nodes
  ```cypher
  MATCH (u:User) RETURN count(u.tutorial_completed) as with_field, count(u) as total;
  ```

- [ ] No users have null `tutorial_completed` (or document migration plan)
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed IS NULL RETURN count(u);
  ```

- [ ] Timestamps are stored correctly
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed = true
  RETURN u.email, u.tutorial_completed_at LIMIT 5;
  ```

### 2.2 Data Consistency

- [ ] Users marked complete have timestamps
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed = true AND u.tutorial_completed_at IS NULL
  RETURN count(u); // Should be 0
  ```

- [ ] Users marked incomplete have null timestamps
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed = false AND u.tutorial_completed_at IS NOT NULL
  RETURN count(u); // Should be 0
  ```

- [ ] Completion statistics make sense
  ```cypher
  MATCH (u:User)
  RETURN u.tutorial_completed AS status, count(*) AS count;
  ```

### 2.3 Migration (if needed)

- [ ] Identify users created before refactor
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed IS NULL
  RETURN count(u) AS users_needing_migration;
  ```

- [ ] Execute migration (if users found)
  ```cypher
  // Option chosen: [true/false/age-based]
  // Migration query from neo4j-onboarding-queries.md
  ```

- [ ] Verify migration success
  ```cypher
  MATCH (u:User) WHERE u.tutorial_completed IS NULL RETURN count(u); // Should be 0
  ```

---

## Phase 3: Frontend Integration Testing

### 3.1 New User Onboarding Flow

**Prerequisites:**
- Create new Keycloak user OR reset existing user's tutorial status

**Steps:**
1. - [ ] Open browser in incognito mode
2. - [ ] Navigate to https://aether.tas.scharber.com
3. - [ ] Log in with new/reset user credentials
4. - [ ] Wait 1 second after login completes
5. - [ ] Verify onboarding modal appears automatically
6. - [ ] Verify modal shows "Welcome to Aether" (Step 1/4)
7. - [ ] Click "Next" through all 4 steps:
   - [ ] Step 1: Welcome
   - [ ] Step 2: Create Your First Notebook
   - [ ] Step 3: Explore Key Features
   - [ ] Step 4: Complete
8. - [ ] Click "Get Started" on final step
9. - [ ] Verify modal closes
10. - [ ] Refresh page
11. - [ ] Verify modal does NOT auto-trigger again
12. - [ ] Check Neo4j database shows `tutorial_completed: true`

**Expected Results:**
- Modal auto-triggers for new users
- Completion is saved to backend
- Modal doesn't re-trigger after completion
- Tutorial state persists across browser sessions

### 3.2 Existing User Flow

**Prerequisites:**
- User with `tutorial_completed: true` in database

**Steps:**
1. - [ ] Log in with existing user (tutorial already completed)
2. - [ ] Wait for page to fully load
3. - [ ] Verify onboarding modal does NOT auto-trigger
4. - [ ] Navigate to different pages
5. - [ ] Confirm modal doesn't appear

**Expected Results:**
- No auto-trigger for users who completed tutorial
- Application behaves normally

### 3.3 Tutorial Reset Flow

**Steps:**
1. - [ ] Complete tutorial as new user
2. - [ ] Call DELETE endpoint to reset tutorial
   ```bash
   curl -X DELETE \
        -H "Authorization: Bearer $TOKEN" \
        https://aether.tas.scharber.com/api/v1/users/me/onboarding
   ```
3. - [ ] Refresh browser page
4. - [ ] Verify modal auto-triggers again
5. - [ ] Complete tutorial again
6. - [ ] Verify can be completed multiple times

**Expected Results:**
- Reset works correctly
- Tutorial can be re-triggered and completed again
- State changes persist

### 3.4 Multi-Session/Device Testing

**Steps:**
1. - [ ] Log in on Browser A
2. - [ ] Complete tutorial
3. - [ ] Log out
4. - [ ] Log in on Browser B (different browser/incognito)
5. - [ ] Verify tutorial does NOT auto-trigger
6. - [ ] Verify state is synchronized

**Expected Results:**
- Tutorial completion syncs across browsers/devices
- No reliance on localStorage

### 3.5 Error Handling

**Test: Backend API unavailable**

1. - [ ] Stop backend service temporarily
   ```bash
   kubectl scale deployment/aether-backend -n aether-be --replicas=0
   ```
2. - [ ] Open frontend in browser
3. - [ ] Log in
4. - [ ] Verify app doesn't crash
5. - [ ] Check browser console for errors (should be handled gracefully)
6. - [ ] Restart backend service
   ```bash
   kubectl scale deployment/aether-backend -n aether-be --replicas=2
   ```

**Expected Results:**
- Frontend handles API errors gracefully
- Defaults to showing tutorial (safe fallback)
- No application crashes

---

## Phase 4: Performance Testing

### 4.1 API Response Times

- [ ] GET endpoint responds < 500ms
- [ ] POST endpoint responds < 1s
- [ ] DELETE endpoint responds < 1s

**Measurement:**
```bash
time curl -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding
```

### 4.2 Database Query Performance

- [ ] User lookup query < 100ms
- [ ] Status update query < 200ms

**Measurement:**
```cypher
PROFILE
MATCH (u:User {id: $user_id})
RETURN u.tutorial_completed, u.tutorial_completed_at;
```

### 4.3 Frontend Loading

- [ ] Onboarding status loads before auto-trigger (no race condition)
- [ ] No visible UI flicker during loading
- [ ] Loading state shown if API is slow

---

## Phase 5: Security Testing

### 5.1 Authentication

- [ ] All endpoints require valid JWT token
- [ ] Expired tokens return 401 Unauthorized
- [ ] Missing tokens return 401 Unauthorized
- [ ] Invalid tokens return 401 Unauthorized

### 5.2 Authorization

- [ ] Users can only access their own onboarding status
- [ ] Cannot read other users' tutorial status
- [ ] Cannot modify other users' tutorial status

**Test:**
```bash
# Try to access with valid token but wrong user
# Should fail authorization
```

### 5.3 Input Validation

- [ ] Malformed requests return 400 Bad Request
- [ ] Invalid JSON returns appropriate error
- [ ] SQL/Cypher injection attempts are blocked

---

## Phase 6: Browser Compatibility

Test on multiple browsers:

- [ ] **Chrome (latest)**
  - [ ] Auto-trigger works
  - [ ] Completion works
  - [ ] No console errors

- [ ] **Firefox (latest)**
  - [ ] Auto-trigger works
  - [ ] Completion works
  - [ ] No console errors

- [ ] **Safari (latest)**
  - [ ] Auto-trigger works
  - [ ] Completion works
  - [ ] No console errors

- [ ] **Edge (latest)**
  - [ ] Auto-trigger works
  - [ ] Completion works
  - [ ] No console errors

- [ ] **Mobile browsers**
  - [ ] Responsive design works
  - [ ] Modal displays correctly
  - [ ] Touch interactions work

---

## Phase 7: Regression Testing

### 7.1 Ensure localStorage is NOT used

- [ ] Clear browser localStorage
- [ ] Log in
- [ ] Verify tutorial state loads from API (not localStorage)
- [ ] Inspect localStorage - should NOT contain `aether_onboarding_completed`

**Verification:**
```javascript
// In browser console
console.log(localStorage.getItem('aether_onboarding_completed')); // Should be null
```

### 7.2 Existing Features Still Work

- [ ] User login/logout works
- [ ] Notebook creation works
- [ ] Document upload works
- [ ] Navigation works
- [ ] Settings work

---

## Issue Tracking Template

If issues are found during testing, use this template:

```markdown
### Issue: [Brief Description]

**Severity**: [Critical/High/Medium/Low]
**Component**: [Backend API/Frontend/Database/Integration]

**Steps to Reproduce:**
1.
2.
3.

**Expected Behavior:**


**Actual Behavior:**


**Screenshots/Logs:**


**Environment:**
- Browser:
- Backend version:
- Frontend version:
- Date/Time:

**Possible Fix:**

```

---

## Test Results Summary

**Test Date**: _____________
**Tester**: _____________
**Environment**: [ ] Development [ ] Staging [ ] Production

### Results

| Phase | Tests Passed | Tests Failed | Notes |
|-------|--------------|--------------|-------|
| Backend API | __/14 | __ | |
| Database | __/8 | __ | |
| Frontend | __/15 | __ | |
| Performance | __/7 | __ | |
| Security | __/7 | __ | |
| Browsers | __/5 | __ | |
| Regression | __/7 | __ | |
| **TOTAL** | __/63 | __ | |

### Overall Status

- [ ] **PASS** - All tests passed, ready for production
- [ ] **PASS WITH NOTES** - Minor issues found, documented, not blocking
- [ ] **FAIL** - Critical issues found, needs fixes before deployment

### Sign-off

**Tested by**: _______________________
**Date**: _____________
**Signature**: _______________________

---

## Appendix: Quick Commands

### Get JWT Token
```bash
cd /home/jscharber/eng/TAS/aether
./get-token.sh | jq -r '.access_token'
```

### Test All API Endpoints
```bash
cd /home/jscharber/eng/TAS/aether-be
./scripts/test-onboarding-api.sh
```

### Query Neo4j
```bash
kubectl port-forward -n aether-be svc/neo4j 7474:7474
# Open http://localhost:7474 in browser
```

### Check Backend Logs
```bash
kubectl logs -n aether-be deployment/aether-backend --tail=100 | grep -i onboard
```

### Check Frontend Logs
```bash
kubectl logs -n aether-be deployment/aether-frontend --tail=100
```

### Reset Test User Tutorial
```bash
TOKEN=$(./get-token.sh | jq -r '.access_token')
curl -X DELETE \
     -H "Authorization: Bearer $TOKEN" \
     https://aether.tas.scharber.com/api/v1/users/me/onboarding
```
