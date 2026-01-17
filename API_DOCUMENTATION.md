# Aether Enterprise AI Platform - API Documentation

## Overview

The Aether Backend API provides comprehensive endpoints for the enterprise AI platform, supporting document processing, ML operations, workflow automation, real-time streaming, and collaboration features.

**Base URL:** `http://localhost:8080/api/v1`

**Authentication:** Bearer Token (JWT)

**Content-Type:** `application/json`

## API Categories

- [Health & Monitoring](#health--monitoring)
- [LLM Router Proxy](#llm-router-proxy)
- [User Management](#user-management)
- [Notebook Management](#notebook-management)
- [Document Processing](#document-processing)
- [ML & Analytics](#ml--analytics)
- [Workflow Automation](#workflow-automation)
- [Live Streaming](#live-streaming)
- [Team & Organization](#team--organization)
- [Real-time WebSocket](#real-time-websocket)

---

## Health & Monitoring

### Health Check
```http
GET /health
```
**Response:** Basic health status

### Liveness Check
```http
GET /health/live
```
**Response:** Service liveness status

### Readiness Check
```http
GET /health/ready
```
**Response:** Service readiness status

### Prometheus Metrics
```http
GET /metrics/prometheus
```
**Response:** Prometheus-formatted metrics

---

## LLM Router Proxy

The Aether Backend provides a proxy interface to the LLM Router service, supporting both public informational endpoints and authenticated operational endpoints. The proxy supports dual authentication modes for flexible deployment scenarios.

### Authentication Modes

**User Authentication Mode (Default):**
- Forwards user's `Authorization: Bearer {token}` header to LLM Router
- Maintains user context for billing and audit purposes
- Compatible with existing frontend implementations

**Service-to-Service Authentication Mode:**
- Uses `X-API-Key: {api_key}` header for backend communication
- Forwards user context as metadata headers (`X-User-Context`, `X-User-Email`)
- Enabled via `ROUTER_USE_SERVICE_AUTH=true` environment variable

### Public Endpoints (No Authentication Required)

#### Get Available Providers
```http
GET /api/v1/router/providers
```
**Response:** List of available LLM providers
```json
{
  "count": 2,
  "providers": ["openai", "anthropic"]
}
```

#### Get Router Health
```http
GET /api/v1/router/health
```
**Response:** LLM Router service health status

#### Get Router Capabilities
```http
GET /api/v1/router/capabilities
```
**Response:** Supported LLM Router features and capabilities

### Authenticated Endpoints (Requires Bearer Token)

#### Get Provider Details
```http
GET /api/v1/router/providers/{name}
Authorization: Bearer {token}
```
**Response:** Detailed information about a specific provider

#### Chat Completions
```http
POST /api/v1/router/chat/completions
Authorization: Bearer {token}
Content-Type: application/json

{
  "model": "gpt-4",
  "messages": [
    {
      "role": "user",
      "content": "Hello, how are you?"
    }
  ],
  "max_tokens": 100
}
```

#### Text Completions
```http
POST /api/v1/router/completions
Authorization: Bearer {token}
Content-Type: application/json

{
  "model": "gpt-3.5-turbo",
  "prompt": "The weather today is",
  "max_tokens": 50
}
```

#### Messages
```http
POST /api/v1/router/messages
Authorization: Bearer {token}
Content-Type: application/json

{
  "messages": [
    {
      "role": "user", 
      "content": "Explain quantum computing"
    }
  ]
}
```

### Configuration

The LLM Router proxy can be configured via environment variables:

```bash
# Enable/disable router proxy
ROUTER_ENABLED=true

# LLM Router service connection
ROUTER_SERVICE_BASE_URL=http://llm-router:8080

# Service-to-service authentication (optional)
ROUTER_API_KEY=your-service-api-key
ROUTER_USE_SERVICE_AUTH=false

# Connection settings
ROUTER_SERVICE_TIMEOUT=30s
ROUTER_SERVICE_MAX_RETRIES=3
ROUTER_SERVICE_CONNECT_TIMEOUT=10s
```

### Error Handling

The proxy implements automatic retry logic for failed requests:
- **Retry Logic:** Up to 3 attempts with exponential backoff
- **Timeout Handling:** Configurable request and connection timeouts
- **Error Responses:** Standardized error format with proper HTTP status codes

Common error responses:
- `401 Unauthorized`: Missing or invalid authentication token
- `502 Bad Gateway`: LLM Router service unavailable
- `504 Gateway Timeout`: Request timed out to LLM Router

---

## User Management

### Get Current User
```http
GET /api/v1/users/me
```
**Response:** Current user profile

### Update Current User
```http
PUT /api/v1/users/me
```
**Body:**
```json
{
  "first_name": "string",
  "last_name": "string",
  "bio": "string",
  "profile_picture_url": "string"
}
```

### Get User Preferences
```http
GET /api/v1/users/me/preferences
```
**Response:** User preferences object

### Update User Preferences
```http
PUT /api/v1/users/me/preferences
```
**Body:**
```json
{
  "theme": "dark",
  "notifications": true,
  "language": "en"
}
```

### Get User Statistics
```http
GET /api/v1/users/me/stats
```
**Response:** User activity statistics

### Search Users
```http
GET /api/v1/users/search?q=john&limit=10
```
**Response:** Array of matching users

---

## Notebook Management

### Create Notebook
```http
POST /api/v1/notebooks
```
**Body:**
```json
{
  "name": "My Research Notebook",
  "description": "AI research documents",
  "visibility": "private",
  "tags": ["ai", "research"],
  "compliance_settings": {
    "gdpr_enabled": true,
    "retention_days": 365
  }
}
```

### List Notebooks
```http
GET /api/v1/notebooks?limit=20&offset=0
```
**Response:** Paginated list of notebooks

### Get Notebook
```http
GET /api/v1/notebooks/{id}
```
**Response:** Notebook details with document count

### Update Notebook
```http
PUT /api/v1/notebooks/{id}
```
**Body:**
```json
{
  "name": "Updated Name",
  "description": "Updated description",
  "tags": ["updated", "tags"]
}
```

### Delete Notebook
```http
DELETE /api/v1/notebooks/{id}
```
**Response:** 204 No Content

### Share Notebook
```http
POST /api/v1/notebooks/{id}/share
```
**Body:**
```json
{
  "user_ids": ["user-id-1", "user-id-2"],
  "group_ids": ["group-id-1"],
  "permissions": ["read", "write"]
}
```

### Search Notebooks
```http
GET /api/v1/notebooks/search?q=machine learning&tags=ai
```
**Response:** Search results with relevance scores

---

## Document Processing

### Upload Document
```http
POST /api/v1/documents/upload
Content-Type: multipart/form-data
```
**Form Data:**
- `file`: Document file
- `notebook_id`: Target notebook ID
- `name`: Optional custom name

### Upload Document (Base64)
```http
POST /api/v1/documents/upload-base64
```
**Body:**
```json
{
  "notebook_id": "notebook-id",
  "name": "document.pdf",
  "content": "base64-encoded-content",
  "mime_type": "application/pdf"
}
```

### List Documents
```http
GET /api/v1/documents?notebook_id={id}&limit=20&offset=0
```
**Response:** Paginated list of documents

### Get Document
```http
GET /api/v1/documents/{id}
```
**Response:** Document details with processing status

### Get Document Status
```http
GET /api/v1/documents/{id}/status
```
**Response:** Real-time processing status

### Update Document
```http
PUT /api/v1/documents/{id}
```
**Body:**
```json
{
  "name": "Updated Document Name",
  "tags": ["updated"]
}
```

### Delete Document
```http
DELETE /api/v1/documents/{id}
```
**Response:** 204 No Content

### Reprocess Document
```http
POST /api/v1/documents/{id}/reprocess
```
**Response:** New processing job details

### Download Document
```http
GET /api/v1/documents/{id}/download
```
**Response:** File download

### Get Document URL
```http
GET /api/v1/documents/{id}/url
```
**Response:** Signed URL for direct access

### Search Documents
```http
GET /api/v1/documents/search?q=neural networks&notebook_id={id}
```
**Response:** Full-text search results

### Refresh Processing Results
```http
POST /api/v1/documents/refresh-processing
```
**Response:** Batch refresh status

---

## Chunk Management

### Get File Chunks
```http
GET /api/v1/files/{file_id}/chunks?limit=50&offset=0
```
**Response:** Paginated chunks for a file

### Get Specific Chunk
```http
GET /api/v1/files/{file_id}/chunks/{chunk_id}
```
**Response:** Chunk content and metadata

### Reprocess File with Strategy
```http
POST /api/v1/files/{file_id}/reprocess
```
**Body:**
```json
{
  "strategy": "semantic_chunking",
  "parameters": {
    "chunk_size": 1000,
    "overlap": 200
  }
}
```

### Search Chunks
```http
POST /api/v1/chunks/search
```
**Body:**
```json
{
  "query": "machine learning algorithms",
  "document_ids": ["doc-1", "doc-2"],
  "limit": 10,
  "similarity_threshold": 0.8
}
```

### Get Available Strategies
```http
GET /api/v1/strategies
```
**Response:** List of chunking strategies

### Get Optimal Strategy
```http
POST /api/v1/strategies/recommend
```
**Body:**
```json
{
  "document_type": "pdf",
  "content_type": "research_paper",
  "length": 50000
}
```

---

## ML & Analytics

### Create ML Model
```http
POST /api/v1/ml/models
```
**Body:**
```json
{
  "name": "Sentiment Classifier",
  "description": "Customer feedback sentiment analysis",
  "type": "classification",
  "framework": "tensorflow",
  "parameters": {
    "learning_rate": 0.001,
    "epochs": 100
  },
  "training_data_info": {
    "dataset_size": 10000,
    "features": 768
  }
}
```

### List ML Models
```http
GET /api/v1/ml/models?limit=20&offset=0
```
**Response:** Paginated ML models

### Get ML Model
```http
GET /api/v1/ml/models/{id}
```
**Response:** Model details with metrics

### Update ML Model
```http
PUT /api/v1/ml/models/{id}
```
**Body:**
```json
{
  "status": "deployed",
  "deployment_endpoint": "https://api.example.com/predict"
}
```

### Delete ML Model
```http
DELETE /api/v1/ml/models/{id}
```
**Response:** 204 No Content

### Deploy ML Model
```http
POST /api/v1/ml/models/{id}/deploy
```
**Body:**
```json
{
  "environment": "production",
  "resources": {
    "cpu": 2,
    "memory": "4Gi"
  }
}
```

### Create ML Experiment
```http
POST /api/v1/ml/experiments
```
**Body:**
```json
{
  "name": "Hyperparameter Tuning v1",
  "description": "Testing different learning rates",
  "model_id": "model-id",
  "parameters": {
    "learning_rate": 0.01,
    "batch_size": 32
  }
}
```

### List ML Experiments
```http
GET /api/v1/ml/experiments?model_id={id}&limit=20&offset=0
```
**Response:** Paginated experiments

### Get ML Experiment
```http
GET /api/v1/ml/experiments/{id}
```
**Response:** Experiment details with results

### Update ML Experiment
```http
PUT /api/v1/ml/experiments/{id}
```
**Body:**
```json
{
  "status": "completed",
  "metrics": {
    "accuracy": 0.95,
    "f1_score": 0.93
  }
}
```

### Get ML Analytics
```http
GET /api/v1/ml/analytics?period=daily
```
**Response:** ML performance analytics

---

## Workflow Automation

### Create Workflow
```http
POST /api/v1/workflows
```
**Body:**
```json
{
  "name": "Document Processing Pipeline",
  "description": "Automated document analysis workflow",
  "definition": {
    "steps": [
      {
        "id": "extract",
        "type": "extract_text",
        "parameters": {}
      },
      {
        "id": "analyze",
        "type": "sentiment_analysis",
        "parameters": {}
      }
    ]
  },
  "trigger_type": "manual",
  "trigger_config": {}
}
```

### List Workflows
```http
GET /api/v1/workflows?limit=20&offset=0
```
**Response:** Paginated workflows

### Get Workflow
```http
GET /api/v1/workflows/{id}
```
**Response:** Workflow definition and status

### Update Workflow
```http
PUT /api/v1/workflows/{id}
```
**Body:**
```json
{
  "status": "active",
  "definition": {
    "updated": "definition"
  }
}
```

### Execute Workflow
```http
POST /api/v1/workflows/{id}/execute
```
**Body:**
```json
{
  "input_data": {
    "document_id": "doc-id"
  }
}
```

### Get Workflow Executions
```http
GET /api/v1/workflows/{id}/executions?limit=20&offset=0
```
**Response:** Execution history

### Get Workflow Analytics
```http
GET /api/v1/workflows/analytics
```
**Response:** Workflow performance metrics

---

## Live Streaming

### Create Stream Source
```http
POST /api/v1/streams/sources
```
**Body:**
```json
{
  "name": "Twitter Monitor",
  "type": "social",
  "provider": "twitter",
  "configuration": {
    "keywords": ["AI", "machine learning"],
    "languages": ["en"],
    "api_key": "your-api-key"
  }
}
```

### List Stream Sources
```http
GET /api/v1/streams/sources?limit=20&offset=0
```
**Response:** Paginated stream sources

### Get Stream Source
```http
GET /api/v1/streams/sources/{id}
```
**Response:** Stream source with current status

### Update Stream Source
```http
PUT /api/v1/streams/sources/{id}
```
**Body:**
```json
{
  "name": "Updated Stream Name",
  "configuration": {
    "updated": "config"
  }
}
```

### Update Stream Source Status
```http
PUT /api/v1/streams/sources/{id}/status
```
**Body:**
```json
{
  "status": "active"
}
```

### Ingest Event
```http
POST /api/v1/streams/sources/{id}/events
```
**Body:**
```json
{
  "event_type": "mention",
  "content": "Great AI product!",
  "media_type": "text",
  "metadata": {
    "source": "twitter",
    "user": "@username"
  }
}
```

### Get Live Events
```http
GET /api/v1/streams/events?source_ids=src1,src2&sentiment=positive&limit=50
```
**Response:** Filtered live events

### Get Live Event
```http
GET /api/v1/streams/events/{id}
```
**Response:** Event details with sentiment analysis

### Get Stream Analytics
```http
GET /api/v1/streams/analytics?period=hourly
```
**Response:** Stream performance metrics

### Get Realtime Analytics
```http
GET /api/v1/streams/analytics/realtime
```
**Response:** Current streaming statistics

---

## Job Tracking

### Get Job Status
```http
GET /api/v1/jobs/{id}
```
**Response:** Job progress and status

---

## Team & Organization

### Create Organization
```http
POST /api/v1/organizations
```
**Body:**
```json
{
  "name": "Acme Corp",
  "description": "AI research organization",
  "settings": {
    "default_visibility": "private"
  }
}
```

### List Organizations
```http
GET /api/v1/organizations
```
**Response:** User's organizations

### Get Organization Members
```http
GET /api/v1/organizations/{id}/members
```
**Response:** Organization member list

### Create Space
```http
POST /api/v1/spaces
```
**Body:**
```json
{
  "name": "Research Space",
  "description": "AI research workspace",
  "organization_id": "org-id"
}
```

### List Spaces
```http
GET /api/v1/spaces
```
**Response:** Available spaces

---

## Real-time WebSocket

### Document Status Stream
```websocket
GET /api/v1/documents/{id}/stream
```
**Messages:** Real-time document processing updates

### Job Status Stream
```websocket
GET /api/v1/jobs/{id}/stream
```
**Messages:** Real-time job progress updates

### Live Event Stream
```websocket
GET /api/v1/streams/live?source_ids=src1&event_types=mention,multimodal
```
**Messages:**
```json
{
  "type": "live_event",
  "event": {
    "id": "event-id",
    "content": "Event content",
    "sentiment": "positive",
    "confidence": 0.95
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Analytics Updates:**
```json
{
  "type": "analytics_update",
  "analytics": {
    "events_per_second": 45.2,
    "active_streams": 3,
    "sentiment_distribution": {
      "positive": 120,
      "neutral": 80,
      "negative": 15
    }
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

## Error Responses

All endpoints return consistent error responses:

```json
{
  "error": "validation_error",
  "message": "Invalid request data",
  "details": {
    "field": "name",
    "code": "required"
  },
  "timestamp": "2024-01-15T10:30:00Z",
  "request_id": "req-12345"
}
```

### HTTP Status Codes

- `200` - Success
- `201` - Created
- `204` - No Content
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `409` - Conflict
- `422` - Unprocessable Entity
- `429` - Too Many Requests
- `500` - Internal Server Error
- `503` - Service Unavailable

---

## Rate Limiting

- **General API**: 1000 requests per hour per user
- **Upload endpoints**: 100 requests per hour per user
- **WebSocket connections**: 10 concurrent connections per user

Rate limit headers:
```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1642244400
```

---

## Pagination

List endpoints support pagination:

**Request:**
```http
GET /api/v1/notebooks?limit=20&offset=40
```

**Response:**
```json
{
  "data": [...],
  "pagination": {
    "total": 150,
    "limit": 20,
    "offset": 40,
    "has_more": true
  }
}
```

---

## Filtering and Sorting

Many endpoints support filtering and sorting:

```http
GET /api/v1/documents?status=processed&sort_by=created_at&sort_order=desc&tags=ai,ml
```

---

## Space Context

Most endpoints require space context headers:

```http
X-Space-ID: space-12345
X-Tenant-ID: tenant-67890
```

---

## Authentication

Include JWT token in Authorization header:

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

Token can be obtained from Keycloak authentication service.

---

## CORS

The API supports CORS for browser-based applications:

- **Allowed Origins**: Configurable (development: `http://localhost:3000`)
- **Allowed Methods**: GET, POST, PUT, DELETE, OPTIONS
- **Allowed Headers**: Authorization, Content-Type, X-Space-ID, X-Tenant-ID

---

## Webhook Support

### AudiModal Processing Webhook
```http
POST /webhooks/audimodal/processing-complete
```
**Body:** AudiModal processing results

This webhook receives processing completion notifications from external services.

---

## SDK and Client Libraries

TypeScript interfaces are provided in the `/types` directory:
- `platform.d.ts` - Core data models
- `api-client.d.ts` - API client interfaces  
- `react-hooks.d.ts` - React hook definitions

---

## Example Usage

### Upload and Process Document

1. **Upload Document**
```javascript
const formData = new FormData();
formData.append('file', file);
formData.append('notebook_id', notebookId);

const response = await fetch('/api/v1/documents/upload', {
  method: 'POST',
  headers: { 'Authorization': `Bearer ${token}` },
  body: formData
});
const document = await response.json();
```

2. **Monitor Processing**
```javascript
const ws = new WebSocket(`/api/v1/documents/${document.id}/stream`);
ws.onmessage = (event) => {
  const status = JSON.parse(event.data);
  console.log(`Progress: ${status.progress}%`);
};
```

### Real-time Streaming

```javascript
const ws = new WebSocket('/api/v1/streams/live?event_types=mention');
ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  if (message.type === 'live_event') {
    console.log('New event:', message.event.content);
    console.log('Sentiment:', message.event.sentiment);
  }
};
```

### ML Model Deployment

```javascript
// Create model
const model = await fetch('/api/v1/ml/models', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    name: 'My Model',
    type: 'classification',
    framework: 'tensorflow'
  })
});

// Deploy model
await fetch(`/api/v1/ml/models/${model.id}/deploy`, {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    environment: 'production'
  })
});
```