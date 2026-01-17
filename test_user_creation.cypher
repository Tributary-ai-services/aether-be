// Test user creation query
CREATE (u:User {
  id: 'd8f32f6d-f49e-4ce9-a873-c337172b1a29-test',
  keycloak_id: 'd8f32f6d-f49e-4ce9-a873-c337172b1a29',
  email: 'john@scharber.com',
  username: 'john@scharber.com',
  full_name: 'John Scharber',
  status: 'active',
  created_at: datetime(),
  updated_at: datetime()
})
RETURN u
EOF < /dev/null
