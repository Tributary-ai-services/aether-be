// Migration: Add tenant fields to Organization nodes
// Purpose: Add TenantID and TenantAPIKey fields to support space-based architecture
// Date: 2025-01-20

// Add TenantID property to all Organization nodes (initially empty)
MATCH (o:Organization)
WHERE o.TenantID IS NULL
SET o.TenantID = ""
RETURN count(o) as updated_organizations;

// Add TenantAPIKey property to all Organization nodes (initially empty)
MATCH (o:Organization)
WHERE o.TenantAPIKey IS NULL
SET o.TenantAPIKey = ""
RETURN count(o) as updated_organizations;

// Create index on TenantID for better performance
CREATE INDEX organization_tenant_id_index IF NOT EXISTS
FOR (o:Organization)
ON (o.TenantID);

// Verify the migration
MATCH (o:Organization)
RETURN o.ID, o.Name, o.TenantID, 
       CASE WHEN o.TenantAPIKey IS NOT NULL THEN "present" ELSE "missing" END as api_key_status
LIMIT 5;