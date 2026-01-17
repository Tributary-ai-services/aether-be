// Chunk Schema Migration
// This file defines the Neo4j schema for Chunk nodes and their relationships

// 1. CREATE CHUNK CONSTRAINTS
// Unique constraint on Chunk.id
CREATE CONSTRAINT chunk_id_unique IF NOT EXISTS FOR (c:Chunk) REQUIRE c.id IS UNIQUE;

// Unique constraint on combination of file_id and chunk_id for integrity
CREATE CONSTRAINT chunk_file_chunk_unique IF NOT EXISTS FOR (c:Chunk) REQUIRE (c.file_id, c.chunk_id) IS UNIQUE;

// 2. CREATE CHUNK INDEXES FOR PERFORMANCE
// Basic indexes for common query patterns
CREATE INDEX chunk_tenant_id_index IF NOT EXISTS FOR (c:Chunk) ON (c.tenant_id);
CREATE INDEX chunk_file_id_index IF NOT EXISTS FOR (c:Chunk) ON (c.file_id);
CREATE INDEX chunk_type_index IF NOT EXISTS FOR (c:Chunk) ON (c.chunk_type);
CREATE INDEX chunk_language_index IF NOT EXISTS FOR (c:Chunk) ON (c.language);
CREATE INDEX chunk_created_at_index IF NOT EXISTS FOR (c:Chunk) ON (c.created_at);

// Workflow and processing indexes
CREATE INDEX chunk_embedding_status_index IF NOT EXISTS FOR (c:Chunk) ON (c.embedding_status);
CREATE INDEX chunk_dlp_scan_status_index IF NOT EXISTS FOR (c:Chunk) ON (c.dlp_scan_status);
CREATE INDEX chunk_pii_detected_index IF NOT EXISTS FOR (c:Chunk) ON (c.pii_detected);

// Full-text search index for content
CREATE FULLTEXT INDEX chunk_content_fulltext IF NOT EXISTS FOR (c:Chunk) ON EACH [c.content];

// Composite indexes for common multi-field queries
CREATE INDEX chunk_tenant_file_composite_index IF NOT EXISTS FOR (c:Chunk) ON (c.tenant_id, c.file_id);
CREATE INDEX chunk_file_type_composite_index IF NOT EXISTS FOR (c:Chunk) ON (c.file_id, c.chunk_type);
CREATE INDEX chunk_status_composite_index IF NOT EXISTS FOR (c:Chunk) ON (c.embedding_status, c.dlp_scan_status);

// 3. ESTABLISH RELATIONSHIPS
// Document to Chunk relationship (one-to-many)
// This will be created dynamically when chunks are processed
// Pattern: (d:Document)-[:CONTAINS]->(c:Chunk)

// Example of creating relationship when chunk is created:
// MATCH (d:Document {id: $file_id})
// CREATE (d)-[:CONTAINS]->(c:Chunk {
//   id: $chunk_id,
//   tenant_id: d.tenant_id,
//   file_id: d.id,
//   chunk_id: $chunk_internal_id,
//   chunk_type: $chunk_type,
//   content: $content,
//   ...other properties
// })

// 4. ADD CHUNK-RELATED FIELDS TO EXISTING DOCUMENTS
// Update existing documents to include chunk metadata fields
MATCH (d:Document)
WHERE d.chunking_strategy IS NULL OR d.chunk_count IS NULL
SET d.chunking_strategy = CASE 
    WHEN d.chunking_strategy IS NULL THEN 'semantic' 
    ELSE d.chunking_strategy 
END,
d.chunk_count = CASE 
    WHEN d.chunk_count IS NULL THEN 0 
    ELSE d.chunk_count 
END,
d.average_chunk_size = CASE 
    WHEN d.average_chunk_size IS NULL THEN 0 
    ELSE d.average_chunk_size 
END,
d.chunk_quality_score = CASE 
    WHEN d.chunk_quality_score IS NULL THEN NULL 
    ELSE d.chunk_quality_score 
END,
d.updated_at = datetime();

// 5. VERIFICATION QUERIES
// Verify chunk constraints
SHOW CONSTRAINTS YIELD name, type, entityType, properties 
WHERE name CONTAINS 'chunk';

// Verify chunk indexes
SHOW INDEXES YIELD name, type, entityType, properties, options
WHERE name CONTAINS 'chunk';

// Count chunks and verify relationships
MATCH (d:Document)-[:CONTAINS]->(c:Chunk)
RETURN d.id as document_id, count(c) as chunk_count
LIMIT 10;

// Verify chunk quality distribution
MATCH (c:Chunk)
RETURN 
    c.chunk_type as type,
    count(c) as chunk_count,
    avg(c.processing_time) as avg_processing_time,
    min(c.created_at) as oldest_chunk,
    max(c.created_at) as newest_chunk;

// Check embedding and DLP status distribution
MATCH (c:Chunk)
RETURN 
    c.embedding_status,
    c.dlp_scan_status,
    c.pii_detected,
    count(c) as count
ORDER BY count DESC;