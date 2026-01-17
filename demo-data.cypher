// Demo data for Aether backend - Run this in Neo4j Browser

// Clear existing demo data
MATCH (n) WHERE n.email ENDS WITH '@demo.aether' OR n.slug STARTS WITH 'demo-' DETACH DELETE n;

// Create demo users
CREATE 
  (john:User {
    id: 'demo-user-1',
    keycloak_id: 'demo-keycloak-john',
    email: 'john@demo.aether',
    username: 'john.doe',
    full_name: 'John Doe',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-01-15T10:00:00Z'),
    updated_at: datetime('2024-01-15T10:00:00Z')
  }),
  
  (jane:User {
    id: 'demo-user-2',
    keycloak_id: 'demo-keycloak-jane',
    email: 'jane@demo.aether',
    username: 'jane.smith',
    full_name: 'Jane Smith',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-01-16T09:00:00Z'),
    updated_at: datetime('2024-01-16T09:00:00Z')
  }),
  
  (bob:User {
    id: 'demo-user-3',
    keycloak_id: 'demo-keycloak-bob',
    email: 'bob@demo.aether',
    username: 'bob.wilson',
    full_name: 'Bob Wilson',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-01-20T14:00:00Z'),
    updated_at: datetime('2024-01-20T14:00:00Z')
  }),
  
  (alice:User {
    id: 'demo-user-4',
    keycloak_id: 'demo-keycloak-alice',
    email: 'alice@demo.aether',
    username: 'alice.brown',
    full_name: 'Alice Brown',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-02-01T10:30:00Z'),
    updated_at: datetime('2024-02-01T10:30:00Z')
  }),
  
  (charlie:User {
    id: 'demo-user-5',
    keycloak_id: 'demo-keycloak-charlie',
    email: 'charlie@demo.aether',
    username: 'charlie.davis',
    full_name: 'Charlie Davis',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-02-15T11:00:00Z'),
    updated_at: datetime('2024-02-15T11:00:00Z')
  }),
  
  (david:User {
    id: 'demo-user-6',
    keycloak_id: 'demo-keycloak-david',
    email: 'david@demo.aether',
    username: 'david.lee',
    full_name: 'David Lee',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-03-01T10:00:00Z'),
    updated_at: datetime('2024-03-01T10:00:00Z')
  }),
  
  (sarah:User {
    id: 'demo-user-7',
    keycloak_id: 'demo-keycloak-sarah',
    email: 'sarah@demo.aether',
    username: 'sarah.johnson',
    full_name: 'Sarah Johnson',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-04-01T14:00:00Z'),
    updated_at: datetime('2024-04-01T14:00:00Z')
  }),
  
  (maria:User {
    id: 'demo-user-8',
    keycloak_id: 'demo-keycloak-maria',
    email: 'maria@demo.aether',
    username: 'maria.garcia',
    full_name: 'Maria Garcia',
    avatar_url: '',
    status: 'active',
    created_at: datetime('2024-09-22T15:30:00Z'),
    updated_at: datetime('2024-09-22T15:30:00Z')
  });

// Create demo organizations
CREATE 
  (acme:Organization {
    id: 'demo-org-1',
    name: 'Acme Corporation',
    slug: 'demo-acme-corp',
    description: 'Leading provider of AI-powered enterprise solutions',
    website: 'https://acme.com',
    location: 'San Francisco, CA',
    visibility: 'public',
    billing: {
      plan: 'enterprise',
      seats: 500,
      billingEmail: 'billing@demo.aether'
    },
    settings: {
      membersCanCreateRepositories: true,
      membersCanCreateTeams: true,
      membersCanFork: true,
      defaultMemberPermissions: 'read',
      twoFactorRequired: true
    },
    created_by: 'demo-user-1',
    created_at: datetime('2023-06-15T10:00:00Z'),
    updated_at: datetime('2024-08-08T14:30:00Z')
  }),
  
  (datatech:Organization {
    id: 'demo-org-2',
    name: 'DataTech Labs',
    slug: 'demo-datatech-labs',
    description: 'Research and development in machine learning',
    website: 'https://datatech.io',
    location: 'Austin, TX',
    visibility: 'private',
    billing: {
      plan: 'pro',
      seats: 25,
      billingEmail: 'admin@demo.aether'
    },
    settings: {
      membersCanCreateRepositories: false,
      membersCanCreateTeams: false,
      membersCanFork: true,
      defaultMemberPermissions: 'read',
      twoFactorRequired: false
    },
    created_by: 'demo-user-8',
    created_at: datetime('2023-09-22T15:30:00Z'),
    updated_at: datetime('2024-08-07T11:45:00Z')
  });

// Create demo teams
CREATE 
  (engTeam:Team {
    id: 'demo-team-1',
    name: 'Engineering Team',
    description: 'Core engineering and development team',
    organization_id: 'demo-org-1',
    visibility: 'private',
    settings: {
      allowExternalSharing: false,
      requireApprovalForJoining: true,
      defaultNotebookVisibility: 'team'
    },
    created_by: 'demo-user-1',
    created_at: datetime('2024-01-15T10:00:00Z'),
    updated_at: datetime('2024-08-08T15:30:00Z')
  }),
  
  (dataTeam:Team {
    id: 'demo-team-2',
    name: 'Data Science',
    description: 'ML and data analysis team',
    organization_id: 'demo-org-1',
    visibility: 'organization',
    settings: {
      allowExternalSharing: true,
      requireApprovalForJoining: false,
      defaultNotebookVisibility: 'organization'
    },
    created_by: 'demo-user-2',
    created_at: datetime('2024-02-20T14:00:00Z'),
    updated_at: datetime('2024-08-07T09:15:00Z')
  }),
  
  (researchTeam:Team {
    id: 'demo-team-3',
    name: 'Research Team',
    description: 'Research and development initiatives',
    organization_id: 'demo-org-1',
    visibility: 'private',
    settings: {
      allowExternalSharing: false,
      requireApprovalForJoining: true,
      defaultNotebookVisibility: 'team'
    },
    created_by: 'demo-user-2',
    created_at: datetime('2024-03-10T11:30:00Z'),
    updated_at: datetime('2024-08-06T16:45:00Z')
  }),
  
  (mlTeam:Team {
    id: 'demo-team-4',
    name: 'ML Research',
    description: 'Advanced machine learning research',
    organization_id: 'demo-org-2',
    visibility: 'private',
    settings: {
      allowExternalSharing: false,
      requireApprovalForJoining: true,
      defaultNotebookVisibility: 'team'
    },
    created_by: 'demo-user-8',
    created_at: datetime('2024-04-01T12:00:00Z'),
    updated_at: datetime('2024-08-05T10:20:00Z')
  });

// Match users and organizations for relationships
MATCH 
  (john:User {id: 'demo-user-1'}),
  (jane:User {id: 'demo-user-2'}),
  (bob:User {id: 'demo-user-3'}),
  (alice:User {id: 'demo-user-4'}),
  (charlie:User {id: 'demo-user-5'}),
  (david:User {id: 'demo-user-6'}),
  (sarah:User {id: 'demo-user-7'}),
  (maria:User {id: 'demo-user-8'}),
  (acme:Organization {id: 'demo-org-1'}),
  (datatech:Organization {id: 'demo-org-2'})

// Create organization memberships
CREATE 
  // Acme Corp members
  (john)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2023-06-15T10:00:00Z'),
    invited_by: null,
    title: 'CEO',
    department: 'Executive'
  }]->(acme),
  
  (jane)-[:MEMBER_OF {
    role: 'admin',
    joined_at: datetime('2023-06-16T09:00:00Z'),
    invited_by: 'demo-user-1',
    title: 'CTO',
    department: 'Engineering'
  }]->(acme),
  
  (bob)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2023-07-01T14:00:00Z'),
    invited_by: 'demo-user-2',
    title: 'Senior Engineer',
    department: 'Engineering'
  }]->(acme),
  
  (alice)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-02-01T10:30:00Z'),
    invited_by: 'demo-user-2',
    title: 'Product Manager',
    department: 'Product'
  }]->(acme),
  
  // DataTech Labs members
  (maria)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2023-09-22T15:30:00Z'),
    invited_by: null,
    title: 'Founder & CEO',
    department: 'Executive'
  }]->(datatech),
  
  (john)-[:MEMBER_OF {
    role: 'admin',
    joined_at: datetime('2023-09-01T12:00:00Z'),
    invited_by: 'demo-user-8',
    title: 'Advisor',
    department: 'Advisory'
  }]->(datatech),
  
  (david)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-03-01T10:00:00Z'),
    invited_by: 'demo-user-8',
    title: 'ML Engineer',
    department: 'Research'
  }]->(datatech);

// Match teams for team memberships
MATCH 
  (john:User {id: 'demo-user-1'}),
  (jane:User {id: 'demo-user-2'}),
  (bob:User {id: 'demo-user-3'}),
  (alice:User {id: 'demo-user-4'}),
  (charlie:User {id: 'demo-user-5'}),
  (david:User {id: 'demo-user-6'}),
  (sarah:User {id: 'demo-user-7'}),
  (maria:User {id: 'demo-user-8'}),
  (engTeam:Team {id: 'demo-team-1'}),
  (dataTeam:Team {id: 'demo-team-2'}),
  (researchTeam:Team {id: 'demo-team-3'}),
  (mlTeam:Team {id: 'demo-team-4'})

// Create team memberships
CREATE 
  // Engineering Team members
  (john)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2024-01-15T10:00:00Z'),
    invited_by: null
  }]->(engTeam),
  
  (jane)-[:MEMBER_OF {
    role: 'admin',
    joined_at: datetime('2024-01-16T09:00:00Z'),
    invited_by: 'demo-user-1'
  }]->(engTeam),
  
  (bob)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-01-20T14:00:00Z'),
    invited_by: 'demo-user-1'
  }]->(engTeam),
  
  (alice)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-02-01T10:30:00Z'),
    invited_by: 'demo-user-2'
  }]->(engTeam),
  
  (charlie)-[:MEMBER_OF {
    role: 'viewer',
    joined_at: datetime('2024-02-15T11:00:00Z'),
    invited_by: 'demo-user-1'
  }]->(engTeam),
  
  // Data Science Team members
  (jane)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2024-02-20T14:00:00Z'),
    invited_by: null
  }]->(dataTeam),
  
  (john)-[:MEMBER_OF {
    role: 'admin',
    joined_at: datetime('2024-02-20T14:30:00Z'),
    invited_by: 'demo-user-2'
  }]->(dataTeam),
  
  (david)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-03-01T10:00:00Z'),
    invited_by: 'demo-user-2'
  }]->(dataTeam),
  
  // Research Team members
  (jane)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2024-03-10T11:30:00Z'),
    invited_by: null
  }]->(researchTeam),
  
  (bob)-[:MEMBER_OF {
    role: 'member',
    joined_at: datetime('2024-03-15T09:00:00Z'),
    invited_by: 'demo-user-2'
  }]->(researchTeam),
  
  (sarah)-[:MEMBER_OF {
    role: 'viewer',
    joined_at: datetime('2024-04-01T14:00:00Z'),
    invited_by: 'demo-user-2'
  }]->(researchTeam),
  
  // ML Research Team members
  (maria)-[:MEMBER_OF {
    role: 'owner',
    joined_at: datetime('2024-04-01T12:00:00Z'),
    invited_by: null
  }]->(mlTeam),
  
  (david)-[:MEMBER_OF {
    role: 'admin',
    joined_at: datetime('2024-04-02T10:00:00Z'),
    invited_by: 'demo-user-8'
  }]->(mlTeam);

// Verify data was created
MATCH (u:User) WHERE u.email ENDS WITH '@demo.aether' RETURN count(u) as users;
MATCH (o:Organization) WHERE o.slug STARTS WITH 'demo-' RETURN count(o) as organizations;
MATCH (t:Team) WHERE t.id STARTS WITH 'demo-' RETURN count(t) as teams;
MATCH ()-[r:MEMBER_OF]->() RETURN count(r) as memberships;