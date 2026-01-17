#!/bin/bash

# Load demo data into Neo4j
NEO4J_URL="http://localhost:7474/db/neo4j/tx/commit"
NEO4J_AUTH="neo4j:password"

echo "Loading demo users..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[{
    "statement":"CREATE (john:User {id: \"demo-user-1\", keycloak_id: \"demo-keycloak-john\", email: \"john@demo.aether\", username: \"john.doe\", full_name: \"John Doe\", avatar_url: \"\", status: \"active\", created_at: datetime(\"2024-01-15T10:00:00Z\"), updated_at: datetime(\"2024-01-15T10:00:00Z\")}), (jane:User {id: \"demo-user-2\", keycloak_id: \"demo-keycloak-jane\", email: \"jane@demo.aether\", username: \"jane.smith\", full_name: \"Jane Smith\", avatar_url: \"\", status: \"active\", created_at: datetime(\"2024-01-16T09:00:00Z\"), updated_at: datetime(\"2024-01-16T09:00:00Z\")}), (bob:User {id: \"demo-user-3\", keycloak_id: \"demo-keycloak-bob\", email: \"bob@demo.aether\", username: \"bob.wilson\", full_name: \"Bob Wilson\", avatar_url: \"\", status: \"active\", created_at: datetime(\"2024-01-20T14:00:00Z\"), updated_at: datetime(\"2024-01-20T14:00:00Z\")}), (alice:User {id: \"demo-user-4\", keycloak_id: \"demo-keycloak-alice\", email: \"alice@demo.aether\", username: \"alice.brown\", full_name: \"Alice Brown\", avatar_url: \"\", status: \"active\", created_at: datetime(\"2024-02-01T10:30:00Z\"), updated_at: datetime(\"2024-02-01T10:30:00Z\")})"
  }]
}' > /dev/null

echo "Loading demo organizations..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[{
    "statement":"CREATE (acme:Organization {id: \"demo-org-1\", name: \"Acme Corporation\", slug: \"demo-acme-corp\", description: \"Leading provider of AI-powered enterprise solutions\", website: \"https://acme.com\", location: \"San Francisco, CA\", visibility: \"public\", created_by: \"demo-user-1\", created_at: datetime(\"2023-06-15T10:00:00Z\"), updated_at: datetime(\"2024-08-08T14:30:00Z\")}), (datatech:Organization {id: \"demo-org-2\", name: \"DataTech Labs\", slug: \"demo-datatech-labs\", description: \"Research and development in machine learning\", website: \"https://datatech.io\", location: \"Austin, TX\", visibility: \"private\", created_by: \"demo-user-2\", created_at: datetime(\"2023-09-22T15:30:00Z\"), updated_at: datetime(\"2024-08-07T11:45:00Z\")})"
  }]
}' > /dev/null

echo "Loading demo teams..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[{
    "statement":"CREATE (engTeam:Team {id: \"demo-team-1\", name: \"Engineering Team\", description: \"Core engineering and development team\", organization_id: \"demo-org-1\", visibility: \"private\", created_by: \"demo-user-1\", created_at: datetime(\"2024-01-15T10:00:00Z\"), updated_at: datetime(\"2024-08-08T15:30:00Z\")}), (dataTeam:Team {id: \"demo-team-2\", name: \"Data Science\", description: \"ML and data analysis team\", organization_id: \"demo-org-1\", visibility: \"organization\", created_by: \"demo-user-2\", created_at: datetime(\"2024-02-20T14:00:00Z\"), updated_at: datetime(\"2024-08-07T09:15:00Z\")}), (researchTeam:Team {id: \"demo-team-3\", name: \"Research Team\", description: \"Research and development initiatives\", organization_id: \"demo-org-1\", visibility: \"private\", created_by: \"demo-user-2\", created_at: datetime(\"2024-03-10T11:30:00Z\"), updated_at: datetime(\"2024-08-06T16:45:00Z\")}), (mlTeam:Team {id: \"demo-team-4\", name: \"ML Research\", description: \"Advanced machine learning research\", organization_id: \"demo-org-2\", visibility: \"private\", created_by: \"demo-user-2\", created_at: datetime(\"2024-04-01T12:00:00Z\"), updated_at: datetime(\"2024-08-05T10:20:00Z\")})"
  }]
}' > /dev/null

echo "Creating organization memberships..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[{
    "statement":"MATCH (john:User {id: \"demo-user-1\"}), (jane:User {id: \"demo-user-2\"}), (bob:User {id: \"demo-user-3\"}), (alice:User {id: \"demo-user-4\"}), (acme:Organization {id: \"demo-org-1\"}), (datatech:Organization {id: \"demo-org-2\"}) CREATE (john)-[:MEMBER_OF {role: \"owner\", joined_at: datetime(\"2023-06-15T10:00:00Z\"), title: \"CEO\", department: \"Executive\"}]->(acme), (jane)-[:MEMBER_OF {role: \"admin\", joined_at: datetime(\"2023-06-16T09:00:00Z\"), title: \"CTO\", department: \"Engineering\"}]->(acme), (bob)-[:MEMBER_OF {role: \"member\", joined_at: datetime(\"2023-07-01T14:00:00Z\"), title: \"Senior Engineer\", department: \"Engineering\"}]->(acme), (alice)-[:MEMBER_OF {role: \"member\", joined_at: datetime(\"2024-02-01T10:30:00Z\"), title: \"Product Manager\", department: \"Product\"}]->(acme), (jane)-[:MEMBER_OF {role: \"owner\", joined_at: datetime(\"2023-09-22T15:30:00Z\"), title: \"Founder & CEO\", department: \"Executive\"}]->(datatech)"
  }]
}' > /dev/null

echo "Creating team memberships..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[{
    "statement":"MATCH (john:User {id: \"demo-user-1\"}), (jane:User {id: \"demo-user-2\"}), (bob:User {id: \"demo-user-3\"}), (alice:User {id: \"demo-user-4\"}), (engTeam:Team {id: \"demo-team-1\"}), (dataTeam:Team {id: \"demo-team-2\"}), (researchTeam:Team {id: \"demo-team-3\"}) CREATE (john)-[:MEMBER_OF {role: \"owner\", joined_at: datetime(\"2024-01-15T10:00:00Z\")}]->(engTeam), (jane)-[:MEMBER_OF {role: \"admin\", joined_at: datetime(\"2024-01-16T09:00:00Z\")}]->(engTeam), (bob)-[:MEMBER_OF {role: \"member\", joined_at: datetime(\"2024-01-20T14:00:00Z\")}]->(engTeam), (alice)-[:MEMBER_OF {role: \"member\", joined_at: datetime(\"2024-02-01T10:30:00Z\")}]->(engTeam), (jane)-[:MEMBER_OF {role: \"owner\", joined_at: datetime(\"2024-02-20T14:00:00Z\")}]->(dataTeam), (john)-[:MEMBER_OF {role: \"admin\", joined_at: datetime(\"2024-02-20T14:30:00Z\")}]->(dataTeam), (jane)-[:MEMBER_OF {role: \"owner\", joined_at: datetime(\"2024-03-10T11:30:00Z\")}]->(researchTeam), (bob)-[:MEMBER_OF {role: \"member\", joined_at: datetime(\"2024-03-15T09:00:00Z\")}]->(researchTeam)"
  }]
}' > /dev/null

echo "Verifying demo data..."
curl -u $NEO4J_AUTH -X POST -H "Content-Type: application/json" $NEO4J_URL -d '{
  "statements":[
    {"statement":"MATCH (u:User) WHERE u.email ENDS WITH \"@demo.aether\" RETURN count(u) as users"},
    {"statement":"MATCH (o:Organization) WHERE o.slug STARTS WITH \"demo-\" RETURN count(o) as organizations"},
    {"statement":"MATCH (t:Team) WHERE t.id STARTS WITH \"demo-\" RETURN count(t) as teams"},
    {"statement":"MATCH ()-[r:MEMBER_OF]->() RETURN count(r) as memberships"}
  ]
}' | jq '.results[] | .data[] | .row'

echo "Demo data loaded successfully!"