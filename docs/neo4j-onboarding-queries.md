# Neo4j Onboarding Verification Queries

This document contains Cypher queries for verifying the onboarding/tutorial tracking feature in the Aether platform.

## Table of Contents
1. [Check User Onboarding Status](#check-user-onboarding-status)
2. [Statistics Queries](#statistics-queries)
3. [Migration Queries](#migration-queries)
4. [Testing Queries](#testing-queries)

---

## Check User Onboarding Status

### View All Users' Onboarding Status
```cypher
MATCH (u:User)
RETURN u.id AS user_id,
       u.email AS email,
       u.username AS username,
       u.tutorial_completed AS completed,
       u.tutorial_completed_at AS completed_at,
       u.created_at AS created_at
ORDER BY u.created_at DESC
LIMIT 50;
```

### View Specific User's Onboarding Status
```cypher
MATCH (u:User {email: 'john@scharber.com'})
RETURN u.id AS user_id,
       u.email AS email,
       u.tutorial_completed AS completed,
       u.tutorial_completed_at AS completed_at,
       u.created_at AS created_at,
       u.last_login_at AS last_login;
```

### Find Users by Tutorial Completion Status
```cypher
// Find users who completed the tutorial
MATCH (u:User)
WHERE u.tutorial_completed = true
RETURN u.id, u.email, u.tutorial_completed_at
ORDER BY u.tutorial_completed_at DESC
LIMIT 20;

// Find users who haven't completed the tutorial
MATCH (u:User)
WHERE u.tutorial_completed = false OR u.tutorial_completed IS NULL
RETURN u.id, u.email, u.created_at
ORDER BY u.created_at DESC
LIMIT 20;
```

---

## Statistics Queries

### Count Users by Completion Status
```cypher
MATCH (u:User)
RETURN u.tutorial_completed AS status,
       count(*) AS count
ORDER BY status;
```

### Completion Rate
```cypher
MATCH (u:User)
WITH count(u) AS total,
     sum(CASE WHEN u.tutorial_completed = true THEN 1 ELSE 0 END) AS completed
RETURN total AS total_users,
       completed AS completed_users,
       total - completed AS incomplete_users,
       round(100.0 * completed / total, 2) AS completion_rate_percent;
```

### Average Time to Complete Tutorial
```cypher
MATCH (u:User)
WHERE u.tutorial_completed = true
  AND u.tutorial_completed_at IS NOT NULL
  AND u.created_at IS NOT NULL
WITH duration.between(u.created_at, u.tutorial_completed_at) AS time_to_complete
RETURN avg(time_to_complete.seconds) / 3600.0 AS avg_hours_to_complete,
       min(time_to_complete.seconds) / 60.0 AS min_minutes_to_complete,
       max(time_to_complete.seconds) / 3600.0 AS max_hours_to_complete;
```

### Recently Completed Tutorials
```cypher
MATCH (u:User)
WHERE u.tutorial_completed = true
  AND u.tutorial_completed_at > datetime() - duration('P7D')
RETURN u.id,
       u.email,
       u.tutorial_completed_at,
       duration.between(u.tutorial_completed_at, datetime()).days AS days_ago
ORDER BY u.tutorial_completed_at DESC;
```

---

## Migration Queries

### Find Users with NULL Tutorial Status (Need Migration)
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
RETURN u.id, u.email, u.created_at, u.last_login_at
ORDER BY u.created_at ASC
LIMIT 100;
```

### Count Users Needing Migration
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
RETURN count(*) AS users_needing_migration;
```

### Set Default Tutorial Status for Existing Users

**Option 1: Default to FALSE (show tutorial to existing users)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
SET u.tutorial_completed = false,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

**Option 2: Default to TRUE (skip tutorial for existing users)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
SET u.tutorial_completed = true,
    u.tutorial_completed_at = u.created_at,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

**Option 3: Set based on account age (users older than 7 days skip tutorial)**
```cypher
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
WITH u,
     CASE
       WHEN duration.between(u.created_at, datetime()).days > 7
       THEN true
       ELSE false
     END AS should_skip_tutorial
SET u.tutorial_completed = should_skip_tutorial,
    u.tutorial_completed_at = CASE
      WHEN should_skip_tutorial THEN u.created_at
      ELSE null
    END,
    u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

---

## Testing Queries

### Manually Set Tutorial Status for Testing User
```cypher
// Mark tutorial as incomplete
MATCH (u:User {email: 'test@example.com'})
SET u.tutorial_completed = false,
    u.tutorial_completed_at = null,
    u.updated_at = datetime()
RETURN u.id, u.email, u.tutorial_completed;

// Mark tutorial as complete
MATCH (u:User {email: 'test@example.com'})
SET u.tutorial_completed = true,
    u.tutorial_completed_at = datetime(),
    u.updated_at = datetime()
RETURN u.id, u.email, u.tutorial_completed, u.tutorial_completed_at;
```

### Verify Tutorial Status Update
```cypher
MATCH (u:User {id: $user_id})
RETURN u.id,
       u.email,
       u.tutorial_completed,
       u.tutorial_completed_at,
       u.updated_at;
```

### Check if Tutorial Fields Exist
```cypher
MATCH (u:User)
RETURN count(u) AS total_users,
       count(u.tutorial_completed) AS users_with_tutorial_field,
       count(u.tutorial_completed_at) AS users_with_timestamp_field;
```

### Find Users with Inconsistent State
```cypher
// Find users marked as completed but missing timestamp
MATCH (u:User)
WHERE u.tutorial_completed = true
  AND u.tutorial_completed_at IS NULL
RETURN u.id, u.email, u.tutorial_completed, u.tutorial_completed_at;

// Find users with timestamp but marked as incomplete
MATCH (u:User)
WHERE u.tutorial_completed = false
  AND u.tutorial_completed_at IS NOT NULL
RETURN u.id, u.email, u.tutorial_completed, u.tutorial_completed_at;
```

---

## Performance Queries

### Check Indexes on User Nodes
```cypher
SHOW INDEXES
WHERE entityType = 'NODE'
  AND labelsOrTypes = ['User'];
```

### Query Performance Test
```cypher
PROFILE
MATCH (u:User)
WHERE u.tutorial_completed = false
RETURN count(u);
```

---

## Cleanup Queries (Use with Caution!)

### Reset All Tutorial Status (DANGEROUS - Testing Only!)
```cypher
// DO NOT run in production without backup!
MATCH (u:User)
SET u.tutorial_completed = false,
    u.tutorial_completed_at = null,
    u.updated_at = datetime()
RETURN count(u) AS users_reset;
```

### Delete Tutorial Completion Data (DANGEROUS - Testing Only!)
```cypher
// DO NOT run in production without backup!
MATCH (u:User)
WHERE u.tutorial_completed IS NOT NULL
REMOVE u.tutorial_completed, u.tutorial_completed_at
SET u.updated_at = datetime()
RETURN count(u) AS users_updated;
```

---

## Access Instructions

### Via Neo4j Browser (Port Forward)
```bash
# Port forward Neo4j browser
kubectl port-forward -n aether-be svc/neo4j 7474:7474

# Access in browser
# URL: http://localhost:7474
# Username: neo4j
# Password: password
# Database: neo4j
```

### Via cypher-shell (Direct Connection with Port Forward)
```bash
# Port forward Bolt protocol
kubectl port-forward -n aether-be neo4j-0 7687:7687

# Connect via cypher-shell (requires TLS)
cypher-shell -a bolt+s://localhost:7687 -u neo4j -p password -d neo4j

# Run query
neo4j@neo4j> MATCH (u:User) RETURN count(u);
```

**Note:** Neo4j requires TLS encryption (`bolt+s://` protocol). The plain `bolt://` protocol will fail.

### Via kubectl exec (Pod-Internal)
```bash
# Execute query directly in pod (uses bolt+s://localhost with self-signed cert)
kubectl exec -n aether-be neo4j-0 -- \
  cypher-shell -a bolt+s://localhost:7687 -u neo4j -p password -d neo4j \
  "MATCH (u:User) RETURN u.email, u.tutorial_completed LIMIT 5;"
```

### Via HTTP Transactional API (Recommended for Scripts)
```bash
# Get a backend pod for running queries
BACKEND_POD=$(kubectl get pods -n aether-be -l app=aether-backend -o jsonpath='{.items[0].metadata.name}')

# Execute query via HTTP API (no TLS certificate issues)
kubectl exec -n aether-be $BACKEND_POD -- \
  wget -q -O- \
  --header="Content-Type: application/json" \
  --header="Authorization: Basic $(echo -n 'neo4j:password' | base64)" \
  --post-data='{"statements":[{"statement":"MATCH (u:User) RETURN count(u) AS count"}]}' \
  "http://neo4j.aether-be.svc.cluster.local:7474/db/neo4j/tx/commit"
```

**Recommended:** Use HTTP API for automated testing and scripts as it bypasses TLS certificate validation issues.

---

## Notes

- **Production Safety**: Always test queries in a development environment first
- **Backups**: Create a backup before running any UPDATE/SET queries
- **Timestamps**: All timestamps are stored in UTC using Neo4j's `datetime()` function
- **Null Handling**: Check for both `false` and `IS NULL` when querying incomplete tutorials
- **Indexes**: Ensure indexes exist on frequently queried fields for performance

---

## Troubleshooting

### Connection Issues
If you can't connect to Neo4j:
1. Check pod is running: `kubectl get pods -n aether-be -l app=neo4j`
2. Check service: `kubectl get svc -n aether-be neo4j`
3. View logs: `kubectl logs -n aether-be neo4j-0`
4. Test from backend pod: `kubectl exec -n aether-be deployment/aether-backend -- curl http://neo4j:7474`

### Query Timeout
If queries timeout:
1. Add `LIMIT` clause to large result sets
2. Check database size: `CALL apoc.meta.stats()`
3. Review query plan: `EXPLAIN MATCH (u:User) RETURN u`
4. Consider adding indexes on frequently queried properties
