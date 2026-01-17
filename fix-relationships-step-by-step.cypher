// Step-by-step relationship fixing script
// Run these queries one at a time to diagnose and fix the issues

// Step 1: Inspect current state
CALL {
    MATCH (u:User) RETURN count(u) as users
} CALL {
    MATCH (t:Team) RETURN count(t) as teams  
} CALL {
    MATCH (o:Organization) RETURN count(o) as orgs
} CALL {
    MATCH ()-[r:MEMBER_OF]->() RETURN count(r) as relationships
}
RETURN users, teams, orgs, relationships;

// Step 2: Show users and what they created
MATCH (u:User)
OPTIONAL MATCH (t:Team) WHERE t.created_by = u.keycloak_id OR t.created_by = u.id
OPTIONAL MATCH (o:Organization) WHERE o.created_by = u.keycloak_id OR o.created_by = u.id
RETURN u.email, u.id as user_neo4j_id, u.keycloak_id, 
       collect(DISTINCT t.name) as teams_created,
       collect(DISTINCT o.name) as orgs_created;

// Step 3: Show existing MEMBER_OF relationships  
MATCH (u:User)-[r:MEMBER_OF]->(entity)
RETURN u.email, r.role, labels(entity)[0] as entity_type, entity.name, r.joined_at;

// Step 4: Find teams without owner relationships
MATCH (t:Team)
WHERE NOT EXISTS((u:User)-[:MEMBER_OF {role: 'owner'}]->(t))
OPTIONAL MATCH (creator:User) WHERE t.created_by = creator.keycloak_id OR t.created_by = creator.id
RETURN t.name, t.created_by, creator.email as creator_email, creator.id as creator_neo4j_id;

// Step 5: Find organizations without owner relationships
MATCH (o:Organization)  
WHERE NOT EXISTS((u:User)-[:MEMBER_OF {role: 'owner'}]->(o))
OPTIONAL MATCH (creator:User) WHERE o.created_by = creator.keycloak_id OR o.created_by = creator.id
RETURN o.name, o.created_by, creator.email as creator_email, creator.id as creator_neo4j_id;

// Step 6: Create missing team owner relationships (keycloak_id match)
MATCH (t:Team), (u:User)
WHERE t.created_by = u.keycloak_id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
CREATE (u)-[:MEMBER_OF {
  role: 'owner',
  joined_at: t.created_at,
  invited_by: u.id
}]->(t)
RETURN 'Team relationship created' as result, t.name, u.email;

// Step 7: Create missing team owner relationships (user id match)
MATCH (t:Team), (u:User)
WHERE t.created_by = u.id 
  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
CREATE (u)-[:MEMBER_OF {
  role: 'owner', 
  joined_at: t.created_at,
  invited_by: u.id
}]->(t)
RETURN 'Team relationship created (by ID)' as result, t.name, u.email;

// Step 8: Create missing organization owner relationships (keycloak_id match)
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
RETURN 'Organization relationship created' as result, o.name, u.email;

// Step 9: Create missing organization owner relationships (user id match)
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
RETURN 'Organization relationship created (by ID)' as result, o.name, u.email;

// Step 10: Final verification
MATCH (u:User)-[r:MEMBER_OF]->(entity)
RETURN u.email, r.role, labels(entity)[0] as type, entity.name
ORDER BY u.email, type, entity.name;