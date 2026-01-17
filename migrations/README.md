# Aether Backend Database Migrations

This directory contains database migration scripts for the Aether backend.

## Space-Based Tenant Model Migration

The primary migration (`001_migrate_to_space_model.go`) migrates the existing data to support the space-based tenant model.

### What it does:

1. **Notebook Migration**
   - Ensures all notebooks have `tenant_id` and `space_id`
   - Sets `space_type` to 'personal' for existing notebooks
   - Derives IDs from the notebook owner's personal tenant ID

2. **Document Migration**
   - Ensures all documents inherit `tenant_id` and `space_id` from their parent notebook
   - Sets `space_type` to match the parent notebook

3. **User Migration**
   - Ensures all users have `personal_space_id` field
   - Derives space ID from existing `personal_tenant_id`

### Running Migrations

1. **Using the shell script (recommended):**
   ```bash
   cd migrations
   ./run_migrations.sh
   ```

2. **Running individual migrations:**
   ```bash
   # Set environment variables
   export NEO4J_URI="bolt://localhost:7687"
   export NEO4J_USERNAME="neo4j"
   export NEO4J_PASSWORD="password"
   
   # Run the migration
   go run 001_migrate_to_space_model.go
   ```

### Rollback

If you need to rollback the space model migration:

```bash
go run rollback_space_model.go
```

**WARNING**: Only use rollback if absolutely necessary. It will remove all space-related fields from the database.

### Migration Safety

- The migration is idempotent - it only updates records that don't already have space information
- Existing space_id and tenant_id values are preserved
- The migration logs all changes for audit purposes

### Verification

After running the migration, you can verify the results:

```cypher
// Check notebooks
MATCH (n:Notebook) 
RETURN 
  count(n) as total,
  count(n.tenant_id) as with_tenant_id,
  count(n.space_id) as with_space_id

// Check documents
MATCH (d:Document)
RETURN 
  count(d) as total,
  count(d.tenant_id) as with_tenant_id,
  count(d.space_id) as with_space_id

// Check users
MATCH (u:User)
RETURN 
  count(u) as total,
  count(u.personal_space_id) as with_space_id
```

### Adding New Migrations

1. Create a new Go file with incrementing number: `002_migration_name.go`
2. Follow the same pattern as existing migrations
3. Update `run_migrations.sh` to include the new migration
4. Document the migration in this README

### Best Practices

- Always test migrations on a development database first
- Take a backup before running migrations in production
- Make migrations idempotent when possible
- Log all significant changes
- Provide a rollback mechanism for complex migrations