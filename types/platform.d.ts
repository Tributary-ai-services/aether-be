/**
 * Aether Enterprise AI Platform - TypeScript Interface Definitions
 * 
 * This file provides comprehensive TypeScript interfaces that align with the
 * backend Go models to ensure type safety across the frontend-backend integration.
 */

// ==================== CORE PLATFORM TYPES ====================

export interface SpaceContext {
  tenant_id: string;
  space_id: string;
  organization_id: string;
  user_id: string;
}

export interface User {
  id: string;
  keycloak_id: string;
  username: string;
  email: string;
  first_name: string;
  last_name: string;
  profile_picture_url?: string;
  bio?: string;
  preferences: Record<string, any>;
  created_at: string;
  updated_at: string;
  last_login?: string;
  is_active: boolean;
  tenant_id: string;
}

export interface Organization {
  id: string;
  name: string;
  description?: string;
  logo_url?: string;
  settings: Record<string, any>;
  created_at: string;
  updated_at: string;
  created_by: string;
  owner_id: string;
}

// ==================== NOTEBOOK & DOCUMENT TYPES ====================

export interface Notebook {
  id: string;
  name: string;
  description?: string;
  owner_id: string;
  visibility: 'private' | 'shared' | 'public';
  tags: string[];
  status: 'active' | 'archived' | 'deleted';
  compliance_settings?: Record<string, any>;
  document_count: number;
  total_size: number;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface NotebookCreateRequest {
  name: string;
  description?: string;
  visibility?: 'private' | 'shared' | 'public';
  tags?: string[];
  compliance_settings?: Record<string, any>;
}

export interface NotebookUpdateRequest {
  name?: string;
  description?: string;
  visibility?: 'private' | 'shared' | 'public';
  tags?: string[];
  status?: 'active' | 'archived' | 'deleted';
  compliance_settings?: Record<string, any>;
}

export interface NotebookShareRequest {
  user_ids?: string[];
  group_ids?: string[];
  permissions: ('read' | 'write' | 'admin')[];
}

export interface Document {
  id: string;
  name: string;
  original_name: string;
  type: 'pdf' | 'docx' | 'txt' | 'image' | 'audio' | 'video';
  mime_type: string;
  size: number;
  status: 'uploading' | 'processing' | 'processed' | 'failed' | 'deleted';
  extracted_text?: string;
  storage_path: string;
  notebook_id: string;
  processing_job_id?: string;
  processing_result?: Record<string, any>;
  processing_time?: number;
  confidence_score?: number;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface DocumentUploadRequest {
  notebook_id: string;
  name?: string;
  tags?: string[];
  file: File;
}

export interface DocumentStatus {
  id: string;
  status: 'uploading' | 'processing' | 'processed' | 'failed' | 'deleted';
  progress: number;
  message?: string;
  processing_time?: number;
  confidence_score?: number;
  extracted_text?: string;
}

// ==================== ML & ANALYTICS TYPES ====================

export interface MLModel {
  id: string;
  name: string;
  description?: string;
  type: 'classification' | 'regression' | 'clustering' | 'nlp' | 'computer_vision' | 'custom';
  version: string;
  status: 'training' | 'trained' | 'deployed' | 'failed' | 'archived';
  framework: 'tensorflow' | 'pytorch' | 'scikit_learn' | 'huggingface' | 'custom';
  parameters: Record<string, any>;
  metrics: Record<string, any>;
  training_data_info: Record<string, any>;
  model_file_path?: string;
  deployment_endpoint?: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface MLModelCreateRequest {
  name: string;
  description?: string;
  type: 'classification' | 'regression' | 'clustering' | 'nlp' | 'computer_vision' | 'custom';
  framework: 'tensorflow' | 'pytorch' | 'scikit_learn' | 'huggingface' | 'custom';
  parameters?: Record<string, any>;
  training_data_info?: Record<string, any>;
}

export interface MLExperiment {
  id: string;
  name: string;
  description?: string;
  model_id: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled';
  parameters: Record<string, any>;
  metrics: Record<string, any>;
  artifacts: Record<string, any>;
  start_time: string;
  end_time?: string;
  duration?: number;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface MLExperimentCreateRequest {
  name: string;
  description?: string;
  model_id: string;
  parameters: Record<string, any>;
}

export interface MLAnalytics {
  id: string;
  period: 'hourly' | 'daily' | 'weekly' | 'monthly';
  timestamp: string;
  total_models: number;
  active_experiments: number;
  model_performance: Record<string, number>;
  experiment_success_rate: number;
  resource_usage: Record<string, any>;
  popular_frameworks: Record<string, number>;
  tenant_id: string;
  organization_id: string;
  created_at: string;
}

// ==================== WORKFLOW TYPES ====================

export interface Workflow {
  id: string;
  name: string;
  description?: string;
  definition: Record<string, any>;
  status: 'active' | 'paused' | 'archived';
  trigger_type: 'manual' | 'scheduled' | 'event' | 'webhook';
  trigger_config: Record<string, any>;
  execution_count: number;
  success_rate: number;
  average_duration: number;
  last_execution?: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface WorkflowCreateRequest {
  name: string;
  description?: string;
  definition: Record<string, any>;
  trigger_type: 'manual' | 'scheduled' | 'event' | 'webhook';
  trigger_config?: Record<string, any>;
}

export interface WorkflowExecution {
  id: string;
  workflow_id: string;
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled';
  trigger_data?: Record<string, any>;
  execution_data: Record<string, any>;
  start_time: string;
  end_time?: string;
  duration?: number;
  error_message?: string;
  step_results: Record<string, any>;
  created_at: string;
  tenant_id: string;
}

export interface WorkflowAnalytics {
  id: string;
  period: 'hourly' | 'daily' | 'weekly' | 'monthly';
  timestamp: string;
  total_workflows: number;
  active_workflows: number;
  total_executions: number;
  successful_executions: number;
  failed_executions: number;
  average_execution_time: number;
  execution_volume: Record<string, number>;
  error_distribution: Record<string, number>;
  tenant_id: string;
  organization_id: string;
  created_at: string;
}

// ==================== LIVE STREAMING TYPES ====================

export interface StreamSource {
  id: string;
  name: string;
  type: 'social' | 'financial' | 'enterprise' | 'media' | 'news';
  provider: 'twitter' | 'stocks' | 'salesforce' | 'youtube' | 'news_api' | 'custom';
  status: 'active' | 'paused' | 'disconnected' | 'error';
  configuration: Record<string, any>;
  events_processed: number;
  events_per_second: number;
  last_event_at?: string;
  connected_at?: string;
  error_count: number;
  error_message?: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
  organization_id: string;
}

export interface StreamSourceCreateRequest {
  name: string;
  type: 'social' | 'financial' | 'enterprise' | 'media' | 'news';
  provider: 'twitter' | 'stocks' | 'salesforce' | 'youtube' | 'news_api' | 'custom';
  configuration: Record<string, any>;
}

export interface LiveEvent {
  id: string;
  stream_source_id: string;
  event_type: 'mention' | 'multimodal' | 'audio' | 'document' | 'video' | 'image';
  content: string;
  media_type: 'text' | 'image' | 'video' | 'audio' | 'document';
  media_url?: string;
  sentiment: 'positive' | 'neutral' | 'negative';
  sentiment_score: number; // -1.0 to 1.0
  confidence: number; // 0.0 to 1.0
  processing_time: number; // milliseconds
  has_audit_trail: boolean;
  audit_score: number; // 0.0 to 1.0
  metadata: Record<string, any>;
  extracted_data: Record<string, any>;
  processed_at: string;
  event_timestamp: string;
  tenant_id: string;
  organization_id: string;
}

export interface StreamFilters {
  source_ids?: string[];
  event_types?: ('mention' | 'multimodal' | 'audio' | 'document' | 'video' | 'image')[];
  media_types?: ('text' | 'image' | 'video' | 'audio' | 'document')[];
  sentiments?: ('positive' | 'neutral' | 'negative')[];
  providers?: string[];
  min_confidence?: number;
}

export interface StreamAnalytics {
  id: string;
  period: 'realtime' | 'hourly' | 'daily';
  timestamp: string;
  active_streams: number;
  total_events_processed: number;
  events_per_second: number;
  media_processed: number;
  average_processing_time: number;
  average_audit_score: number;
  sentiment_distribution: Record<string, number>;
  event_type_distribution: Record<string, number>;
  provider_performance: Record<string, number>;
  error_rate: number;
  tenant_id: string;
  organization_id: string;
  created_at: string;
}

// ==================== WEBSOCKET TYPES ====================

export interface StreamEventWebSocketMessage {
  type: 'live_event' | 'analytics_update' | 'stream_status';
  event?: LiveEvent;
  analytics?: StreamAnalytics;
  status?: StreamSourceStatus;
  timestamp: string;
}

export interface StreamSourceStatus {
  source_id: string;
  status: 'active' | 'paused' | 'disconnected' | 'error';
  events_per_second: number;
  last_event_at: string;
  error_message?: string;
}

// ==================== JOB TRACKING TYPES ====================

export interface ProcessingJob {
  id: string;
  document_id: string;
  type: 'document_processing' | 'ml_training' | 'workflow_execution' | 'data_ingestion';
  status: 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
  progress: number; // 0-100
  message?: string;
  started_at?: string;
  completed_at?: string;
  duration?: number;
  result?: Record<string, any>;
  error_message?: string;
  created_at: string;
  created_by: string;
  tenant_id: string;
}

export interface JobStatusUpdate {
  job_id: string;
  status: 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
  progress: number;
  message?: string;
  timestamp: string;
}

// ==================== API RESPONSE TYPES ====================

export interface PaginatedResponse<T> {
  data: T[];
  pagination: {
    total: number;
    limit: number;
    offset: number;
    has_more: boolean;
  };
}

export interface ApiError {
  error: string;
  message: string;
  details?: Record<string, any>;
  timestamp: string;
  request_id?: string;
}

export interface ApiResponse<T> {
  data: T;
  message?: string;
  timestamp: string;
  request_id?: string;
}

// ==================== SEARCH & FILTER TYPES ====================

export interface SearchRequest {
  query: string;
  filters?: Record<string, any>;
  limit?: number;
  offset?: number;
  sort_by?: string;
  sort_order?: 'asc' | 'desc';
}

export interface SearchResult<T> {
  items: T[];
  total: number;
  query: string;
  filters: Record<string, any>;
  execution_time: number;
}

// ==================== CHUNK & VECTOR SEARCH TYPES ====================

export interface Chunk {
  id: string;
  content: string;
  document_id: string;
  chunk_index: number;
  token_count: number;
  embedding?: number[];
  metadata: Record<string, any>;
  created_at: string;
  tenant_id: string;
}

export interface ChunkSearchRequest {
  query: string;
  document_ids?: string[];
  limit?: number;
  similarity_threshold?: number;
  include_metadata?: boolean;
}

export interface ChunkSearchResult {
  chunk: Chunk;
  similarity_score: number;
  document_name: string;
  highlight?: string;
}

// ==================== HEALTH & MONITORING TYPES ====================

export interface HealthCheck {
  status: 'healthy' | 'degraded' | 'unhealthy';
  timestamp: string;
  services: {
    database: 'healthy' | 'degraded' | 'unhealthy';
    storage: 'healthy' | 'degraded' | 'unhealthy';
    kafka: 'healthy' | 'degraded' | 'unhealthy';
    redis: 'healthy' | 'degraded' | 'unhealthy';
  };
  version: string;
  uptime: number;
}

export interface SystemMetrics {
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  active_connections: number;
  request_rate: number;
  error_rate: number;
  average_response_time: number;
  timestamp: string;
}

// ==================== TYPE GUARDS ====================

export function isDocument(obj: any): obj is Document {
  return obj && typeof obj.id === 'string' && typeof obj.name === 'string';
}

export function isNotebook(obj: any): obj is Notebook {
  return obj && typeof obj.id === 'string' && typeof obj.name === 'string' && 'owner_id' in obj;
}

export function isLiveEvent(obj: any): obj is LiveEvent {
  return obj && typeof obj.id === 'string' && 'stream_source_id' in obj && 'event_type' in obj;
}

export function isProcessingJob(obj: any): obj is ProcessingJob {
  return obj && typeof obj.id === 'string' && 'status' in obj && 'progress' in obj;
}

// ==================== UTILITY TYPES ====================

export type EntityStatus = 'active' | 'archived' | 'deleted';
export type Visibility = 'private' | 'shared' | 'public';
export type Permission = 'read' | 'write' | 'admin';
export type SortOrder = 'asc' | 'desc';

export interface BaseEntity {
  id: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  tenant_id: string;
}

export interface TenantedEntity extends BaseEntity {
  organization_id: string;
}

// ==================== FRONTEND-SPECIFIC TYPES ====================

export interface UIState {
  loading: boolean;
  error?: string;
  selectedItems: string[];
  filters: Record<string, any>;
  sortBy: string;
  sortOrder: SortOrder;
}

export interface NavigationItem {
  id: string;
  label: string;
  icon: string;
  path: string;
  badge?: number;
  active: boolean;
}

export interface TableColumn<T> {
  key: keyof T;
  label: string;
  sortable: boolean;
  width?: string;
  render?: (value: any, item: T) => React.ReactNode;
}

export interface FormField {
  name: string;
  label: string;
  type: 'text' | 'email' | 'password' | 'select' | 'textarea' | 'file' | 'checkbox' | 'radio';
  required: boolean;
  placeholder?: string;
  options?: Array<{ value: string; label: string }>;
  validation?: Record<string, any>;
}