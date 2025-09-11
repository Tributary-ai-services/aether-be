// Migration: Add personal tenant fields to User nodes
// Purpose: Support personal tenant spaces for each user
// Date: 2025-01-24

// Add PersonalTenantID property to all existing User nodes
MATCH (u:User)
WHERE NOT EXISTS(u.PersonalTenantID)
SET u.PersonalTenantID = '';

// Add PersonalAPIKey property to all existing User nodes  
MATCH (u:User)
WHERE NOT EXISTS(u.PersonalAPIKey)
SET u.PersonalAPIKey = '';

// Create index on PersonalTenantID for efficient lookups
CREATE INDEX user_personal_tenant_id IF NOT EXISTS
FOR (u:User)
ON (u.PersonalTenantID);

// Create constraint to ensure PersonalTenantID is unique when set
CREATE CONSTRAINT unique_user_personal_tenant_id IF NOT EXISTS
FOR (u:User)
REQUIRE u.PersonalTenantID IS UNIQUE;

// Log migration completion
CREATE (m:Migration {
    name: 'add_tenant_fields_to_users',
    version: '002',
    description: 'Add PersonalTenantID and PersonalAPIKey fields to User nodes',
    appliedAt: datetime(),
    status: 'completed'
});