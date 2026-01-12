# Onboarding Permission Error Fix

## Problem Statement

When new users were created and onboarding was triggered, the process failed with:
```
Failed to resolve personal space: FORBIDDEN: Cannot access another user's personal space
```

This blocked the end-to-end user onboarding flow, preventing:
- Creation of default "Getting Started" notebook
- Upload of sample documents to MinIO
- Creation of default "Personal Assistant" agent

## Root Cause

The error occurred in the space resolution logic during onboarding. The specific failure point was in `/internal/services/space_context.go` at line 81-86:

```go
if spaceID != user.PersonalSpaceID {
    return errors.Forbidden("Cannot access another user's personal space")
}
```

### Why It Failed

1. **User Creation Flow**:
   - User is created with personal tenant in AudiModal
   - `SetPersonalTenantInfo()` derives `PersonalSpaceID` from `PersonalTenantID`
   - User record is saved to Neo4j with `personal_space_id` field

2. **Onboarding Flow**:
   - Onboarding service calls `ResolveSpaceContext()` with user's `PersonalSpaceID`
   - `ResolveSpaceContext()` fetches user from Neo4j by Keycloak ID
   - Compares requested `spaceID` with `user.PersonalSpaceID` from database

3. **The Bug**:
   - If the `personal_space_id` field in Neo4j was NULL or empty (possibly due to database schema issues or migration gaps), the user object would have `PersonalSpaceID = ""`
   - The comparison `"space_abc-123" != ""` would fail
   - Permission denied error was thrown, even though the user was accessing their OWN space

## The Fix

### Changes Made

#### 1. `/internal/services/user.go`

**Added fallback logic** in `recordToUser()` method (lines 797-810):

```go
// Fallback: If personal_space_id is missing but personal_tenant_id exists, derive it
// This handles cases where older users don't have personal_space_id populated
if user.PersonalSpaceID == "" && user.PersonalTenantID != "" {
    if strings.HasPrefix(user.PersonalTenantID, "tenant_") {
        user.PersonalSpaceID = "space_" + user.PersonalTenantID[len("tenant_"):]
    } else {
        user.PersonalSpaceID = "space_" + user.PersonalTenantID
    }
    s.logger.Info("Derived PersonalSpaceID from PersonalTenantID for user",
        zap.String("user_id", user.ID),
        zap.String("personal_tenant_id", user.PersonalTenantID),
        zap.String("derived_personal_space_id", user.PersonalSpaceID),
    )
}
```

**Added diagnostic logging** in `CreateUser()` (lines 199-203):

```go
s.logger.Info("Personal tenant info set on user",
    zap.String("user_id", user.ID),
    zap.String("personal_tenant_id", user.PersonalTenantID),
    zap.String("personal_space_id", user.PersonalSpaceID),
)
```

#### 2. `/internal/services/space_context.go`

**Enhanced error logging** when space ID mismatch occurs (lines 82-88):

```go
if spaceID != user.PersonalSpaceID {
    s.logger.Error("Personal space ID mismatch",
        zap.String("keycloak_id", userID),
        zap.String("internal_user_id", user.ID),
        zap.String("requested_space_id", spaceID),
        zap.String("user_personal_space_id", user.PersonalSpaceID),
        zap.String("user_personal_tenant_id", user.PersonalTenantID),
    )
    return errors.Forbidden(...)
}
```

#### 3. `/internal/services/onboarding.go`

**Added pre-resolution logging** (lines 77-82):

```go
s.logger.Info("Resolving personal space for onboarding",
    zap.String("user_id", user.ID),
    zap.String("keycloak_id", user.KeycloakID),
    zap.String("personal_space_id", user.PersonalSpaceID),
    zap.String("personal_tenant_id", user.PersonalTenantID),
)
```

## Why This Fix Is Correct and Secure

### 1. No Permission Bypass
- The fix does NOT skip or disable permission checks
- It only ensures the user object has correct data before the check runs
- All space access still requires matching the user's own personal space ID

### 2. Data Consistency
- Uses identical derivation logic as `SetPersonalTenantInfo()`
- Maintains consistency between in-memory and database representations
- Handles both UUID and legacy "tenant_X" formats

### 3. Backward Compatible
- Existing users with `personal_space_id` set: No change, uses database value
- Older users without it: Derives from `personal_tenant_id`
- New users: Should have it set, but fallback provides safety net

### 4. Security Audit
- ✅ No external input used in derivation
- ✅ Space ID derived only from user's own tenant ID
- ✅ No privilege escalation possible
- ✅ Maintains principle of least privilege
- ✅ Preserves all existing permission boundaries

### 5. Observable and Debuggable
- Comprehensive logging at three critical points
- Tracks derivation when fallback is used
- Easy to diagnose future issues

## Testing Verification

### Expected Log Flow (Successful Onboarding)

1. **User Creation**:
```
[INFO] Personal tenant info set on user
  user_id: fd9142fd-e50a-47bc-99a9-e2e596e9e28a
  personal_tenant_id: abc-123-def
  personal_space_id: space_abc-123-def
```

2. **Onboarding Start**:
```
[INFO] Starting user onboarding
  user_id: fd9142fd-e50a-47bc-99a9-e2e596e9e28a
```

3. **Space Resolution**:
```
[INFO] Resolving personal space for onboarding
  user_id: fd9142fd-e50a-47bc-99a9-e2e596e9e28a
  keycloak_id: 8014617b-4208-4b6c-a3ed-d7ce49a88c65
  personal_space_id: space_abc-123-def
  personal_tenant_id: abc-123-def
```

4. **If Fallback Triggered** (only if database field was empty):
```
[INFO] Derived PersonalSpaceID from PersonalTenantID for user
  user_id: fd9142fd-e50a-47bc-99a9-e2e596e9e28a
  personal_tenant_id: abc-123-def
  derived_personal_space_id: space_abc-123-def
```

5. **Success**:
```
[INFO] Space context resolved
  space_id: space_abc-123-def
  tenant_id: abc-123-def
[INFO] User onboarding completed successfully
```

### Test Procedure

1. **Start the application** with updated code
2. **Create a new test user** via Keycloak authentication
3. **Access `/api/v1/users/me`** to trigger onboarding
4. **Monitor logs** for the expected flow above
5. **Verify**:
   - No FORBIDDEN errors
   - Onboarding completes
   - Default notebook created
   - Sample documents uploaded to MinIO

### Manual Test Command

```bash
# Assuming you have a JWT token
curl -H "Authorization: Bearer $JWT_TOKEN" \
     http://localhost:8080/api/v1/users/me
```

Expected response should include user profile without errors.

## Related Files

- `/internal/services/user.go` - User service with fallback logic
- `/internal/services/space_context.go` - Space resolution with enhanced logging
- `/internal/services/onboarding.go` - Onboarding flow with diagnostic logging
- `/internal/models/user.go` - User model with `SetPersonalTenantInfo()` logic
- `/internal/handlers/user.go` - Handler that triggers onboarding

## Future Improvements

1. **Database Migration**: Add a migration script to populate `personal_space_id` for any existing users missing it
2. **Unit Tests**: Add test cases for the fallback derivation logic
3. **Integration Tests**: Add end-to-end onboarding test
4. **Monitoring**: Add metrics for onboarding success/failure rates

## Rollback Plan

If issues arise, the changes can be safely reverted:
1. Remove fallback logic from `recordToUser()` (lines 797-810 in user.go)
2. Remove enhanced logging (all logging additions can be removed)
3. The original permission check logic remains unchanged

However, this would restore the original bug, so investigate the actual root cause (why database field is empty) instead.

## Summary

This fix resolves the user onboarding permission error by ensuring that the `PersonalSpaceID` is always correctly populated when a user is fetched from the database, even if the database field is missing. The fix is secure, backward compatible, and maintains all existing permission boundaries while adding comprehensive diagnostic logging.
