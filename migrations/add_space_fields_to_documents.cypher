// Migration: Add space and tenant fields to Document nodes
// Purpose: Support space-based tenant isolation for documents
// Date: 2025-01-24

// Add SpaceType property to all existing Document nodes (default to personal)
MATCH (d:Document)
WHERE NOT EXISTS(d.SpaceType)
SET d.SpaceType = 'personal';

// Add SpaceID property to all existing Document nodes (default to owner_id for personal)
MATCH (d:Document)
WHERE NOT EXISTS(d.SpaceID) AND EXISTS(d.owner_id)
SET d.SpaceID = d.owner_id;

// Add TenantID property to all existing Document nodes (empty for now, will be migrated)
MATCH (d:Document)
WHERE NOT EXISTS(d.TenantID)
SET d.TenantID = '';

// Create index on SpaceType + SpaceID for efficient space filtering
CREATE INDEX document_space IF NOT EXISTS
FOR (d:Document)
ON (d.SpaceType, d.SpaceID);

// Create index on TenantID for efficient tenant filtering
CREATE INDEX document_tenant_id IF NOT EXISTS
FOR (d:Document)
ON (d.TenantID);

// Create index on NotebookID + TenantID for efficient notebook document queries
CREATE INDEX document_notebook_tenant IF NOT EXISTS
FOR (d:Document)
ON (d.notebook_id, d.TenantID);

// Log migration completion
CREATE (m:Migration {
    name: 'add_space_fields_to_documents',
    version: '004',
    description: 'Add SpaceType, SpaceID, and TenantID fields to Document nodes',
    appliedAt: datetime(),
    status: 'completed'
});