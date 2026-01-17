// Migration 003: Add Agent support to Neo4j schema
// Run this migration after the basic schema is in place
// This establishes agents as first-class entities with relationships to existing resources

// Create constraints for Agent nodes to ensure data integrity
CREATE CONSTRAINT agent_id_unique IF NOT EXISTS FOR (a:Agent) REQUIRE a.id IS UNIQUE;
CREATE CONSTRAINT agent_agent_builder_id_unique IF NOT EXISTS FOR (a:Agent) REQUIRE a.agent_builder_id IS UNIQUE;

// Create indexes for performance on common query patterns
CREATE INDEX agent_owner_id IF NOT EXISTS FOR (a:Agent) ON (a.owner_id);
CREATE INDEX agent_space_id IF NOT EXISTS FOR (a:Agent) ON (a.space_id);
CREATE INDEX agent_tenant_id IF NOT EXISTS FOR (a:Agent) ON (a.tenant_id);
CREATE INDEX agent_status IF NOT EXISTS FOR (a:Agent) ON (a.status);
CREATE INDEX agent_created_at IF NOT EXISTS FOR (a:Agent) ON (a.created_at);

// Agent node schema following existing patterns (User, Document, Notebook)
// Note: Agent data remains in PostgreSQL via agent-builder, this is metadata for relationships
// Fields mirror models/agent.go from tas-agent-builder for consistency

/* Agent node properties schema:
{
  id: string (UUID) - Neo4j identifier (not same as agent-builder ID)
  agent_builder_id: string (UUID) - Reference to agent in PostgreSQL via agent-builder
  name: string - Agent display name
  description: string - Agent description
  status: string - "draft", "published", "disabled"
  
  // Space/tenant context (mirrors existing patterns)
  owner_id: string (UUID) - User who created the agent
  space_id: string (UUID) - Space this agent belongs to
  space_type: string - "personal" or "organization"
  tenant_id: string - Tenant identifier for multi-tenancy
  
  // Team assignment for organization spaces
  team_id: string (UUID, optional) - Team that owns/manages this agent
  
  // Visibility and sharing
  is_public: boolean - Whether agent is publicly discoverable
  is_template: boolean - Whether agent serves as a template
  
  // Metadata tracking
  tags: [string] - Labels for categorization
  search_text: string - Combined searchable text (name + description + tags)
  
  // Statistics (synced from agent-builder)
  total_executions: integer - Number of times agent has been executed
  total_cost_usd: float - Total cost of all executions
  avg_response_time_ms: integer - Average response time
  last_executed_at: datetime - When agent was last run
  
  // Audit trail
  created_at: datetime - When agent was first created
  updated_at: datetime - Last modification timestamp
  synced_at: datetime - Last sync with agent-builder data
}
*/

// Relationship types for agents:
// (:Agent)-[:OWNED_BY]->(:User) - Agent ownership
// (:Agent)-[:BELONGS_TO_SPACE]->(:Space) - Space membership (when spaces exist)
// (:Agent)-[:MANAGED_BY_TEAM]->(:Team) - Team management for organization spaces
// (:Agent)-[:SEARCHES_IN {search_config}]->(:Notebook) - Vector search configuration
// (:Agent)-[:USES_DOCUMENT {purpose}]->(:Document) - Direct document access
// (:Agent)-[:EXECUTED_BY]->(:User) - Execution history (separate from ownership)

// The migration creates the schema but no actual Agent nodes yet
// Agents will be created via API calls that sync with agent-builder

// Create sample constraint verification query
// Verify the constraints were created successfully
SHOW CONSTRAINTS YIELD name, type, labelsOrTypes, properties
WHERE any(label IN labelsOrTypes WHERE label = 'Agent')
RETURN name, type, properties
ORDER BY name;

// Create sample index verification query  
// Verify the indexes were created successfully
SHOW INDEXES YIELD name, type, labelsOrTypes, properties
WHERE any(label IN labelsOrTypes WHERE label = 'Agent')
RETURN name, type, properties
ORDER BY name;

// Sample queries for future agent operations:

/* 
// Create an agent (example - actual creation will be via Go service)
CREATE (agent:Agent {
  id: 'agent-uuid-from-neo4j',
  agent_builder_id: 'agent-uuid-from-postgresql',
  name: 'Customer Support Assistant',
  description: 'Helps customers with common questions using company knowledge base',
  status: 'published',
  owner_id: 'user-uuid',
  space_id: 'space-uuid',
  space_type: 'organization',
  tenant_id: 'tenant-uuid',
  team_id: 'team-uuid',
  is_public: false,
  is_template: false,
  tags: ['customer-support', 'knowledge-base'],
  search_text: 'Customer Support Assistant Helps customers with common questions customer-support knowledge-base',
  total_executions: 0,
  total_cost_usd: 0.0,
  avg_response_time_ms: 0,
  last_executed_at: null,
  created_at: datetime(),
  updated_at: datetime(),
  synced_at: datetime()
});

// Link agent to owner
MATCH (agent:Agent {id: 'agent-uuid'}), (user:User {id: 'user-uuid'})
CREATE (agent)-[:OWNED_BY {
  created_at: datetime()
}]->(user);

// Link agent to team (for organization spaces)
MATCH (agent:Agent {id: 'agent-uuid'}), (team:Team {id: 'team-uuid'})
CREATE (agent)-[:MANAGED_BY_TEAM {
  assigned_at: datetime(),
  assigned_by: 'user-uuid'
}]->(team);

// Configure agent to search specific notebooks (vector collections)
MATCH (agent:Agent {id: 'agent-uuid'}), (notebook:Notebook {id: 'notebook-uuid'})
CREATE (agent)-[:SEARCHES_IN {
  added_at: datetime(),
  added_by: 'user-uuid',
  search_strategy: 'hybrid', // semantic + keyword
  max_results: 10,
  similarity_threshold: 0.75,
  search_weight: 1.0, // For multiple notebooks, relative importance
  filters: '{"document_type": ["pdf", "markdown"]}' // JSON string of search filters
}]->(notebook);

// Query patterns for agent operations:

// Find all agents accessible to a user (owned + team + public)
MATCH (user:User {id: $userId})
OPTIONAL MATCH (user)<-[:OWNED_BY]-(ownedAgents:Agent)
OPTIONAL MATCH (user)-[:MEMBER_OF]->(team:Team)<-[:MANAGED_BY_TEAM]-(teamAgents:Agent)
OPTIONAL MATCH (publicAgents:Agent {is_public: true})
WITH collect(DISTINCT ownedAgents) + collect(DISTINCT teamAgents) + collect(DISTINCT publicAgents) as allAgents
UNWIND allAgents as agent
WHERE agent IS NOT NULL
RETURN DISTINCT agent
ORDER BY agent.updated_at DESC;

// Find agents using a specific notebook
MATCH (notebook:Notebook {id: $notebookId})<-[:SEARCHES_IN]-(agents:Agent)
RETURN agents.id, agents.name, agents.owner_id
ORDER BY agents.name;

// Get agent's complete vector search configuration
MATCH (agent:Agent {id: $agentId})-[search:SEARCHES_IN]->(notebooks:Notebook)
RETURN notebooks.id as notebook_id, 
       notebooks.name as notebook_name,
       search.search_strategy,
       search.max_results,
       search.similarity_threshold,
       search.search_weight,
       search.filters
ORDER BY search.search_weight DESC;

// Impact analysis: find agents affected by notebook changes
MATCH (notebook:Notebook {id: $notebookId})<-[:SEARCHES_IN]-(affectedAgents:Agent)
OPTIONAL MATCH (affectedAgents)-[:OWNED_BY]->(owners:User)
RETURN affectedAgents.id, affectedAgents.name, owners.email, affectedAgents.total_executions
ORDER BY affectedAgents.total_executions DESC;

*/

// This migration establishes the foundation for agent relationship management
// while keeping the actual agent data in PostgreSQL via agent-builder