#!/bin/bash
# Script to create sample documents for a user missing them after onboarding
# Usage: ./fix-missing-sample-docs.sh <user_token> <space_id> <notebook_id>
#
# Example:
#   ./fix-missing-sample-docs.sh "eyJhbGciOiJSUzI1..." "space_1766596584" "71aac4fa-feca-441f-9975-cc18fa125857"

set -e

TOKEN="${1:-}"
SPACE_ID="${2:-}"
NOTEBOOK_ID="${3:-}"
BASE_URL="${BASE_URL:-https://aether-api.tas.scharber.com}"

if [ -z "$TOKEN" ] || [ -z "$SPACE_ID" ] || [ -z "$NOTEBOOK_ID" ]; then
    echo "Usage: $0 <user_token> <space_id> <notebook_id>"
    echo ""
    echo "Arguments:"
    echo "  user_token   - JWT access token for the user"
    echo "  space_id     - User's personal space ID (e.g., space_1766596584)"
    echo "  notebook_id  - ID of the 'Getting Started' notebook"
    echo ""
    echo "Environment:"
    echo "  BASE_URL     - API base URL (default: https://aether-api.tas.scharber.com)"
    exit 1
fi

echo "Creating sample documents..."
echo "  Space ID:    $SPACE_ID"
echo "  Notebook ID: $NOTEBOOK_ID"
echo "  Base URL:    $BASE_URL"
echo ""

# Document 1: Welcome to Aether.txt
DOC1_CONTENT='Welcome to Aether - AI-Powered Document Intelligence Platform

Aether is your comprehensive platform for intelligent document processing and AI-powered knowledge extraction.

KEY FEATURES:
1. Multi-Modal Document Processing
   - Upload PDFs, images, audio, and video files
   - Automatic text extraction and OCR
   - Metadata extraction and indexing

2. Intelligent Search
   - Semantic search across all your documents
   - Vector-based similarity matching
   - Entity recognition and relationship mapping

3. AI Agents
   - Create custom AI assistants for your notebooks
   - Ask questions about your documents
   - Get intelligent summaries and insights

4. Collaborative Workspaces
   - Organize documents in notebooks
   - Share with team members
   - Role-based access control

GETTING STARTED:
1. Upload your first document using the "Upload" button
2. Wait for processing to complete
3. Use the search bar to find content
4. Create an AI agent to interact with your documents

For more information, visit our documentation or contact support.'

# Document 2: Quick Start Guide.txt
DOC2_CONTENT='Aether Quick Start Guide

STEP 1: UPLOAD DOCUMENTS
- Click the "Upload" button in your notebook
- Select one or more files (PDF, DOCX, images, audio, video)
- Add a description and tags
- Click "Upload" to start processing

STEP 2: WAIT FOR PROCESSING
- Documents are automatically processed by our AI engine
- Text is extracted and indexed
- Metadata and entities are identified
- Processing typically takes 30-60 seconds per document

STEP 3: SEARCH YOUR DOCUMENTS
- Use the search bar to find content across all documents
- Search supports natural language queries
- Results are ranked by relevance
- Click any result to view the full document

STEP 4: CREATE AN AI AGENT
- Go to the "Agents" tab
- Click "Create Agent"
- Configure the agent with your preferences
- The agent will have access to all documents in selected notebooks

STEP 5: ASK QUESTIONS
- Chat with your AI agent
- Ask questions about your documents
- Get summaries and insights
- The agent will cite sources from your documents

ADVANCED FEATURES:
- Create hierarchical notebook structures
- Share notebooks with team members
- Export search results and insights
- Set up automated workflows

Need help? Check the FAQ or contact our support team.'

# Document 3: Sample FAQ.txt
DOC3_CONTENT='Aether Platform - Frequently Asked Questions

Q: What file types does Aether support?
A: Aether supports a wide range of file types including:
   - Documents: PDF, DOCX, TXT, MD
   - Images: JPG, PNG, GIF, TIFF
   - Audio: MP3, WAV, M4A
   - Video: MP4, MOV, AVI
   - Archives: ZIP (auto-extracted)

Q: How long does document processing take?
A: Processing time varies by document size and complexity:
   - Text documents (< 10 pages): 10-30 seconds
   - Large PDFs (100+ pages): 1-3 minutes
   - Images with OCR: 20-60 seconds
   - Audio/video transcription: ~30% of file duration

Q: Is my data secure?
A: Yes, Aether implements enterprise-grade security:
   - All data encrypted at rest and in transit
   - Role-based access control (RBAC)
   - Audit logging of all activities
   - SOC 2 and GDPR compliant
   - Regular security audits

Q: Can I share notebooks with my team?
A: Yes! Create an Organization space to:
   - Invite team members
   - Assign roles (owner, admin, member, viewer)
   - Share notebooks and documents
   - Collaborate on AI agents
   - Track team activity

Q: How does the AI agent work?
A: AI agents use advanced language models to:
   - Understand natural language questions
   - Search relevant documents in your notebooks
   - Synthesize information from multiple sources
   - Provide cited answers with source references
   - Learn from your feedback

Q: What are the storage limits?
A: Storage limits depend on your plan:
   - Personal Free: 5GB, 1,000 files
   - Professional: 100GB, 10,000 files
   - Enterprise: Unlimited storage

Q: Can I export my data?
A: Yes, you can export:
   - Individual documents (original format)
   - Search results (CSV, JSON)
   - Notebook metadata
   - Chat transcripts with AI agents
   - Full data export for migration

Q: How accurate is the text extraction?
A: Our AI-powered extraction achieves:
   - 99%+ accuracy on digital PDFs
   - 95%+ accuracy on scanned documents (OCR)
   - 90%+ accuracy on handwritten text
   - 95%+ accuracy on audio transcription

Q: Can I integrate Aether with other tools?
A: Yes, Aether provides:
   - REST API for programmatic access
   - Webhooks for event notifications
   - Zapier and Make integrations
   - Direct integrations with Slack, Teams, Google Drive

For more questions, contact support@aether.ai'

# Function to upload a document
upload_document() {
    local name="$1"
    local description="$2"
    local content="$3"
    local tags="$4"
    local order="$5"

    # Base64 encode the content
    local base64_content=$(echo -n "$content" | base64 -w 0)

    echo "Uploading: $name"

    local response=$(curl -sk -X POST "${BASE_URL}/api/v1/documents/upload-base64" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -H "X-Space-Type: personal" \
        -H "X-Space-ID: $SPACE_ID" \
        -d "{
            \"name\": \"$name\",
            \"description\": \"$description\",
            \"notebook_id\": \"$NOTEBOOK_ID\",
            \"tags\": $tags,
            \"file_data\": \"$base64_content\",
            \"mime_type\": \"text/plain\",
            \"metadata\": {
                \"source\": \"onboarding\",
                \"is_sample\": true,
                \"order\": $order
            }
        }" 2>&1)

    # Check for success
    if echo "$response" | grep -q '"id"'; then
        local doc_id=$(echo "$response" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo "  Success! Document ID: $doc_id"
        return 0
    else
        echo "  Failed! Response: $response"
        return 1
    fi
}

# Upload all three sample documents
echo "--- Document 1/3 ---"
upload_document \
    "Welcome to Aether.txt" \
    "Introduction to the Aether AI Platform" \
    "$DOC1_CONTENT" \
    '["welcome", "introduction", "getting-started"]' \
    1

echo ""
echo "--- Document 2/3 ---"
upload_document \
    "Quick Start Guide.txt" \
    "Step-by-step guide to using Aether" \
    "$DOC2_CONTENT" \
    '["guide", "tutorial", "quick-start"]' \
    2

echo ""
echo "--- Document 3/3 ---"
upload_document \
    "Sample FAQ.txt" \
    "Frequently Asked Questions about Aether" \
    "$DOC3_CONTENT" \
    '["faq", "help", "support"]' \
    3

echo ""
echo "Done! Sample documents have been created."
echo ""
echo "To verify, run this Cypher query:"
echo "MATCH (n:Notebook {id: '$NOTEBOOK_ID'})"
echo "OPTIONAL MATCH (d:Document {notebook_id: n.id})"
echo "RETURN n.name, count(d) as doc_count, collect(d.name) as documents"
