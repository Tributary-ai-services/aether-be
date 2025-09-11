// Create demo documents for Training Materials notebook
// This will create documents that should be visible when testing end-to-end functionality

// First, find the Training Materials notebook
MATCH (notebook:Notebook {name: 'Training Materials'})
WITH notebook

// Create demo documents for this notebook
CREATE (doc1:Document {
  id: 'doc-' + randomUUID(),
  name: 'AI Ethics Guidelines.pdf',
  description: 'Comprehensive guidelines for ethical AI development and deployment',
  type: 'pdf',
  status: 'active',
  original_name: 'AI_Ethics_Guidelines.pdf',
  mime_type: 'application/pdf',
  size_bytes: 2456789,
  checksum: 'sha256:abc123def456ghi789',
  storage_path: '/documents/training/ai-ethics-guidelines.pdf',
  storage_bucket: 'default',
  extracted_text: 'This document outlines the fundamental principles of ethical AI development, including fairness, transparency, accountability, and privacy protection.',
  notebook_id: notebook.id,
  owner_id: notebook.owner_id,
  space_type: notebook.space_type,
  space_id: notebook.space_id,
  tenant_id: notebook.tenant_id,
  tags: ['ethics', 'guidelines', 'ai', 'training'],
  search_text: 'AI Ethics Guidelines ethical AI development fairness transparency accountability privacy',
  metadata: '{"category": "training", "classification": "public", "version": "1.0"}',
  created_at: datetime('2024-01-15T10:00:00Z'),
  updated_at: datetime('2024-01-15T10:00:00Z')
}),

(doc2:Document {
  id: 'doc-' + randomUUID(),
  name: 'Machine Learning Best Practices.docx',
  description: 'Best practices for developing and deploying machine learning models',
  type: 'document',
  status: 'active',
  original_name: 'ML_Best_Practices.docx',
  mime_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  size_bytes: 1234567,
  checksum: 'sha256:def456ghi789abc123',
  storage_path: '/documents/training/ml-best-practices.docx',
  storage_bucket: 'default',
  extracted_text: 'Best practices for machine learning include proper data preprocessing, model validation, cross-validation, and continuous monitoring.',
  notebook_id: notebook.id,
  owner_id: notebook.owner_id,
  space_type: notebook.space_type,
  space_id: notebook.space_id,
  tenant_id: notebook.tenant_id,
  tags: ['machine-learning', 'best-practices', 'training', 'models'],
  search_text: 'Machine Learning Best Practices ML models validation preprocessing monitoring',
  metadata: '{"category": "training", "classification": "internal", "version": "2.1"}',
  created_at: datetime('2024-01-20T14:30:00Z'),
  updated_at: datetime('2024-01-20T14:30:00Z')
}),

(doc3:Document {
  id: 'doc-' + randomUUID(),
  name: 'Data Privacy Training.pptx',
  description: 'Training presentation on data privacy regulations and compliance',
  type: 'presentation',
  status: 'active',
  original_name: 'Data_Privacy_Training.pptx',
  mime_type: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  size_bytes: 5678901,
  checksum: 'sha256:ghi789abc123def456',
  storage_path: '/documents/training/data-privacy-training.pptx',
  storage_bucket: 'default',
  extracted_text: 'Data privacy training covers GDPR compliance, data minimization, user consent, and data retention policies.',
  notebook_id: notebook.id,
  owner_id: notebook.owner_id,
  space_type: notebook.space_type,
  space_id: notebook.space_id,
  tenant_id: notebook.tenant_id,
  tags: ['privacy', 'gdpr', 'compliance', 'training'],
  search_text: 'Data Privacy Training GDPR compliance data minimization consent retention',
  metadata: '{"category": "training", "classification": "confidential", "version": "3.0"}',
  created_at: datetime('2024-02-01T09:15:00Z'),
  updated_at: datetime('2024-02-01T09:15:00Z')
}),

(doc4:Document {
  id: 'doc-' + randomUUID(),
  name: 'Neural Network Fundamentals.pdf',
  description: 'Introduction to neural networks and deep learning concepts',
  type: 'pdf',
  status: 'active',
  original_name: 'Neural_Network_Fundamentals.pdf',
  mime_type: 'application/pdf',
  size_bytes: 3456789,
  checksum: 'sha256:jkl012mno345pqr678',
  storage_path: '/documents/training/neural-network-fundamentals.pdf',
  storage_bucket: 'default',
  extracted_text: 'Neural networks are computational models inspired by biological neural networks. This document covers perceptrons, backpropagation, and deep learning architectures.',
  notebook_id: notebook.id,
  owner_id: notebook.owner_id,
  space_type: notebook.space_type,
  space_id: notebook.space_id,
  tenant_id: notebook.tenant_id,
  tags: ['neural-networks', 'deep-learning', 'fundamentals', 'training'],
  search_text: 'Neural Network Fundamentals deep learning perceptrons backpropagation architectures',
  metadata: '{"category": "training", "classification": "public", "version": "1.5"}',
  created_at: datetime('2024-02-10T16:45:00Z'),
  updated_at: datetime('2024-02-10T16:45:00Z')
}),

(doc5:Document {
  id: 'doc-' + randomUUID(),
  name: 'API Security Checklist.xlsx',
  description: 'Security checklist for API development and deployment',
  type: 'spreadsheet',
  status: 'active',
  original_name: 'API_Security_Checklist.xlsx',
  mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  size_bytes: 987654,
  checksum: 'sha256:stu901vwx234yz567',
  storage_path: '/documents/training/api-security-checklist.xlsx',
  storage_bucket: 'default',
  extracted_text: 'API security checklist includes authentication, authorization, input validation, rate limiting, HTTPS enforcement, and security headers.',
  notebook_id: notebook.id,
  owner_id: notebook.owner_id,
  space_type: notebook.space_type,
  space_id: notebook.space_id,
  tenant_id: notebook.tenant_id,
  tags: ['api', 'security', 'checklist', 'development'],
  search_text: 'API Security Checklist authentication authorization validation rate limiting HTTPS',
  metadata: '{"category": "training", "classification": "internal", "version": "2.0"}',
  created_at: datetime('2024-02-15T11:20:00Z'),
  updated_at: datetime('2024-02-15T11:20:00Z')
})

// Create relationships between documents and notebook
CREATE (doc1)-[:BELONGS_TO]->(notebook),
       (doc2)-[:BELONGS_TO]->(notebook),
       (doc3)-[:BELONGS_TO]->(notebook),
       (doc4)-[:BELONGS_TO]->(notebook),
       (doc5)-[:BELONGS_TO]->(notebook)

// Create relationships between documents and owner
WITH notebook
MATCH (owner:User {id: notebook.owner_id})
MATCH (doc1:Document {notebook_id: notebook.id}),
      (doc2:Document {notebook_id: notebook.id}),
      (doc3:Document {notebook_id: notebook.id}),
      (doc4:Document {notebook_id: notebook.id}),
      (doc5:Document {notebook_id: notebook.id})
CREATE (doc1)-[:OWNED_BY]->(owner),
       (doc2)-[:OWNED_BY]->(owner),
       (doc3)-[:OWNED_BY]->(owner),
       (doc4)-[:OWNED_BY]->(owner),
       (doc5)-[:OWNED_BY]->(owner)

RETURN 'Created 5 demo documents for Training Materials notebook' as result;