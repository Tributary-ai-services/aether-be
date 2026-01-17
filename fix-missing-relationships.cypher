// Fix Missing MEMBER_OF Relationships
// This script creates missing relationships between users and teams/organizations they created

// First, let's see what we have
MATCH (u:User) RETURN count(u) as total_users;
MATCH (t:Team) RETURN count(t) as total_teams;
MATCH (o:Organization) RETURN count(o) as total_organizations;
MATCH ()-[r:MEMBER_OF]->() RETURN count(r) as existing_member_relationships;

// Create MEMBER_OF relationships for team creators
// Match teams with their creators and create owner relationships
MATCH (t:Team), (u:User)
WHERE t.created_by = u.keycloak_id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
CREATE (u)-[:MEMBER_OF {
  role: 'owner',
  joined_at: t.created_at,
  invited_by: u.id
}]->(t)
RETURN 'Created team owner relationship' as action, t.name as team_name, u.email as user_email;

// Create MEMBER_OF relationships for organization creators  
// Match organizations with their creators and create owner relationships
MATCH (o:Organization), (u:User)
WHERE o.created_by = u.keycloak_id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(o))
CREATE (u)-[:MEMBER_OF {
  role: 'owner',
  joined_at: o.created_at,
  invited_by: u.id,
  title: '',
  department: ''
}]->(o)
RETURN 'Created organization owner relationship' as action, o.name as org_name, u.email as user_email;

// Alternative approach if keycloak_id doesn't match:
// Try matching by user ID if the created_by field contains Neo4j user ID instead of keycloak_id
MATCH (t:Team), (u:User)
WHERE t.created_by = u.id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
CREATE (u)-[:MEMBER_OF {
  role: 'owner',
  joined_at: t.created_at,
  invited_by: u.id
}]->(t)
RETURN 'Created team owner relationship (by user ID)' as action, t.name as team_name, u.email as user_email;

MATCH (o:Organization), (u:User)
WHERE o.created_by = u.id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(o))
CREATE (u)-[:MEMBER_OF {
  role: 'owner',
  joined_at: o.created_at,
  invited_by: u.id,
  title: '',
  department: ''
}]->(o)
RETURN 'Created organization owner relationship (by user ID)' as action, o.name as org_name, u.email as user_email;

// Verify the results
MATCH (u:User)-[r:MEMBER_OF]->(t:Team) 
RETURN 'Team memberships created' as summary, count(r) as count;

MATCH (u:User)-[r:MEMBER_OF]->(o:Organization) 
RETURN 'Organization memberships created' as summary, count(r) as count;

// Show all relationships for debugging
MATCH (u:User)-[r:MEMBER_OF]->(entity)
RETURN u.email as user_email, 
       u.keycloak_id as keycloak_id,
       r.role as role, 
       labels(entity) as entity_type, 
       entity.name as entity_name,
       r.joined_at as joined_at
ORDER BY u.email, entity.name;