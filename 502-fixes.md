# 502 Bad Gateway Fixes for Aether Backend

## Common Causes and Solutions

### 1. Agent-Builder Service Not Running

**Symptom:** 
- 502 errors when accessing `/api/v1/agents` endpoint
- Logs show: `Agent-builder service unavailable`

**Solution:**
```bash
# Start agent-builder service
cd /home/jscharber/eng/TAS/tas-agent-builder
DB_HOST=tas-postgres-shared \
DB_PORT=5432 \
DB_USER=tasuser \
DB_PASSWORD=taspassword \
DB_NAME=tas_shared \
JWT_SECRET=823xb9ZpXB+D4lur7MHA80/6uU60Dj/0njFAK4aD+m0= \
SERVER_PORT=8087 \
go run cmd/main.go
```

### 2. Incorrect AGENT_BUILDER_URL Configuration

**Symptom:**
- 502 errors even when agent-builder is running
- Logs show: `connection refused` to agent-builder

**Root Cause:**
- Docker containers can't connect to localhost services
- `host.docker.internal` doesn't work on Linux (resolves to wrong IP)
- Environment variables don't update with `docker-compose restart`

**Solution:**
```bash
# Stop and remove the backend container
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose stop aether-backend
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose rm -f aether-backend

# Start with correct agent-builder URL using Docker bridge gateway IP
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 \
AGENT_BUILDER_URL=http://172.17.0.1:8087/api/v1 \
docker-compose up -d aether-backend
```

### 3. Nil Pointer Dereferences in Agent Service

**Symptom:**
- 502 errors with panic in logs
- Stack trace shows nil pointer dereference in agent service

**Root Cause:**
- Missing safe type assertions when processing agent-builder responses
- Agent-builder returns nil values for optional fields

**Solution:**
Ensure all type assertions use safe patterns:
```go
// BAD - causes panic
agentName := agentData["name"].(string)

// GOOD - safe type assertion
agentName := ""
if name, ok := agentData["name"].(string); ok {
    agentName = name
}

// OR use helper function
agentName := getStringField(agentData, "name", "")
```

## Quick Diagnostic Commands

```bash
# 1. Check if backend is running
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose ps

# 2. Check backend logs for errors
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose logs --tail=50 aether-backend

# 3. Check if agent-builder is accessible
curl -s http://localhost:8087/health

# 4. Check current AGENT_BUILDER_URL in container
docker exec aether-be_aether-backend_1 env | grep AGENT_BUILDER

# 5. Test agent endpoint from command line
TOKEN_RESPONSE=$(curl -s -X POST "http://localhost:8081/realms/master/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password&client_id=aether-frontend&username=john@scharber.com&password=test123")
ACCESS_TOKEN=$(echo "$TOKEN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
curl -s "http://localhost:8080/api/v1/agents" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "X-Space-Type: personal" \
  -H "X-Space-ID: space_1756217701" | jq .
```

## Important Notes

1. **Never use `docker-compose restart` with environment variables** - they won't be updated. Always stop, remove, and recreate the container.

2. **Docker networking on Linux:** 
   - `host.docker.internal` may not work correctly
   - Use Docker bridge gateway IP: `172.17.0.1`
   - Find gateway: `docker network inspect bridge | grep Gateway`

3. **Agent-builder must be running** before starting the backend if agent endpoints are needed.

4. **Safe type assertions are critical** when handling external API responses to prevent panics.

## Complete Fix Sequence

```bash
# 1. Start agent-builder
cd /home/jscharber/eng/TAS/tas-agent-builder
DB_HOST=tas-postgres-shared DB_PORT=5432 DB_USER=tasuser DB_PASSWORD=taspassword \
DB_NAME=tas_shared JWT_SECRET=823xb9ZpXB+D4lur7MHA80/6uU60Dj/0njFAK4aD+m0= \
SERVER_PORT=8087 go run cmd/main.go &

# 2. Verify agent-builder is running
curl -s http://localhost:8087/health

# 3. Restart backend with correct configuration
cd /home/jscharber/eng/TAS/aether-be
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose stop aether-backend
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 docker-compose rm -f aether-backend
KEYCLOAK_CLIENT_SECRET=e78dEfml7xy6YKyHyiQWMMmw7fDs6Kz8 \
AGENT_BUILDER_URL=http://172.17.0.1:8087/api/v1 \
docker-compose up -d aether-backend

# 4. Verify configuration
docker exec aether-be_aether-backend_1 env | grep AGENT_BUILDER
# Should show: AGENT_BUILDER_URL=http://172.17.0.1:8087/api/v1
```