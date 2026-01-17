// Migration: Add space and tenant fields to Notebook nodes
// Purpose: Support space-based tenant isolation for notebooks
// Date: 2025-01-24

// Add SpaceType property to all existing Notebook nodes
MATCH (n:Notebook)
WHERE NOT EXISTS(n.SpaceType)
SET n.SpaceType = 'personal';

// Add SpaceID property to all existing Notebook nodes (default to owner_id for personal)
MATCH (n:Notebook)
WHERE NOT EXISTS(n.SpaceID) AND EXISTS(n.owner_id)
SET n.SpaceID = n.owner_id;

// Add TenantID property to all existing Notebook nodes (empty for now, will be migrated)
MATCH (n:Notebook)
WHERE NOT EXISTS(n.TenantID)
SET n.TenantID = '';

// Add TeamID property to all existing Notebook nodes
MATCH (n:Notebook)
WHERE NOT EXISTS(n.TeamID)
SET n.TeamID = '';

// Create index on SpaceType + SpaceID for efficient space filtering
CREATE INDEX notebook_space IF NOT EXISTS
FOR (n:Notebook)
ON (n.SpaceType, n.SpaceID);

// Create index on TenantID for efficient tenant filtering
CREATE INDEX notebook_tenant_id IF NOT EXISTS
FOR (n:Notebook)
ON (n.TenantID);

// Create index on TeamID for team-based filtering
CREATE INDEX notebook_team_id IF NOT EXISTS
FOR (n:Notebook)
ON (n.TeamID);

// Log migration completion
CREATE (m:Migration {
    name: 'add_space_fields_to_notebooks',
    version: '003',
    description: 'Add SpaceType, SpaceID, TenantID, and TeamID fields to Notebook nodes',
    appliedAt: datetime(),
    status: 'completed'
});