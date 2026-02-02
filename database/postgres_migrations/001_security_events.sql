-- Security Events Migration
-- This migration creates tables for security event tracking and policy management

-- Security events table
-- Stores all detected security threats with their details and review status
CREATE TABLE IF NOT EXISTS security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    event_type VARCHAR(50) NOT NULL,          -- sql_injection, xss, html_injection, control_chars
    severity VARCHAR(20) NOT NULL,            -- low, medium, high, critical
    request_id VARCHAR(100) NOT NULL,
    request_path VARCHAR(500),
    request_method VARCHAR(10),
    client_ip VARCHAR(45),                    -- Supports both IPv4 and IPv6
    user_agent TEXT,
    user_id VARCHAR(255),
    field_name VARCHAR(255),
    threat_pattern VARCHAR(255),
    matched_content TEXT,
    action VARCHAR(20) NOT NULL,              -- sanitized, isolated, rejected
    resource_id VARCHAR(255),                 -- ID of created resource (if any)
    resource_type VARCHAR(50),                -- document, notebook, etc.
    status VARCHAR(50) DEFAULT 'new',         -- new, reviewed, approved, rejected, false_positive
    reviewed_by VARCHAR(255),
    reviewed_at TIMESTAMP WITH TIME ZONE,
    review_notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_security_events_tenant_id ON security_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_security_events_status ON security_events(status);
CREATE INDEX IF NOT EXISTS idx_security_events_severity ON security_events(severity);
CREATE INDEX IF NOT EXISTS idx_security_events_event_type ON security_events(event_type);
CREATE INDEX IF NOT EXISTS idx_security_events_created_at ON security_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_security_events_request_id ON security_events(request_id);
CREATE INDEX IF NOT EXISTS idx_security_events_resource ON security_events(resource_id, resource_type);

-- Composite index for dashboard queries
CREATE INDEX IF NOT EXISTS idx_security_events_dashboard
    ON security_events(tenant_id, status, created_at DESC);

-- Security policy configuration table
-- Defines how different threat types and severities should be handled
CREATE TABLE IF NOT EXISTS security_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,                           -- NULL = global default
    event_type VARCHAR(50) NOT NULL,          -- sql_injection, xss, etc. or '*' for all
    severity VARCHAR(20) NOT NULL,            -- low, medium, high, critical
    action VARCHAR(20) NOT NULL,              -- sanitize, isolate, reject
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, event_type, severity)
);

-- Index for policy lookups
CREATE INDEX IF NOT EXISTS idx_security_policies_tenant ON security_policies(tenant_id, enabled);
CREATE INDEX IF NOT EXISTS idx_security_policies_lookup
    ON security_policies(tenant_id, event_type, severity, enabled);

-- Insert default policies (global defaults, tenant_id = NULL)
INSERT INTO security_policies (tenant_id, event_type, severity, action) VALUES
    (NULL, '*', 'low', 'sanitize'),
    (NULL, '*', 'medium', 'isolate'),
    (NULL, '*', 'high', 'isolate'),
    (NULL, '*', 'critical', 'reject')
ON CONFLICT (tenant_id, event_type, severity) DO NOTHING;

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_security_policies_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update updated_at
DROP TRIGGER IF EXISTS trigger_security_policies_updated_at ON security_policies;
CREATE TRIGGER trigger_security_policies_updated_at
    BEFORE UPDATE ON security_policies
    FOR EACH ROW
    EXECUTE FUNCTION update_security_policies_updated_at();

-- Comments for documentation
COMMENT ON TABLE security_events IS 'Stores security threat detection events with review workflow';
COMMENT ON TABLE security_policies IS 'Configuration for how security threats should be handled by tenant';
COMMENT ON COLUMN security_events.action IS 'Action taken: sanitized (content cleaned), isolated (resource pending review), rejected (request blocked)';
COMMENT ON COLUMN security_events.status IS 'Review status: new (unreviewed), reviewed, approved, rejected, false_positive';
COMMENT ON COLUMN security_policies.event_type IS 'Threat type to match, or "*" for all types';
COMMENT ON COLUMN security_policies.tenant_id IS 'Tenant-specific policy, NULL for global defaults';
