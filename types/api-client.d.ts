/**
 * Aether Enterprise AI Platform - API Client Type Definitions
 * 
 * This file provides TypeScript interfaces for API client implementations,
 * ensuring type safety for frontend HTTP requests and WebSocket connections.
 */

import {
  User, Organization, Notebook, Document, MLModel, MLExperiment, Workflow, WorkflowExecution,
  StreamSource, LiveEvent, StreamAnalytics, ProcessingJob, PaginatedResponse, ApiResponse,
  SearchRequest, SearchResult, HealthCheck, SystemMetrics, SpaceContext,
  NotebookCreateRequest, NotebookUpdateRequest, NotebookShareRequest,
  DocumentUploadRequest, MLModelCreateRequest, MLExperimentCreateRequest,
  WorkflowCreateRequest, StreamSourceCreateRequest, StreamFilters,
  ChunkSearchRequest, ChunkSearchResult, JobStatusUpdate, StreamEventWebSocketMessage
} from './platform';

// ==================== API CLIENT CONFIGURATION ====================

export interface ApiClientConfig {
  baseUrl: string;
  timeout: number;
  retries: number;
  apiKey?: string;
  authToken?: string;
  defaultHeaders?: Record<string, string>;
}

export interface RequestOptions {
  headers?: Record<string, string>;
  timeout?: number;
  retries?: number;
  signal?: AbortSignal;
}

// ==================== HTTP CLIENT INTERFACE ====================

export interface HttpClient {
  get<T>(url: string, options?: RequestOptions): Promise<T>;
  post<T>(url: string, data?: any, options?: RequestOptions): Promise<T>;
  put<T>(url: string, data?: any, options?: RequestOptions): Promise<T>;
  patch<T>(url: string, data?: any, options?: RequestOptions): Promise<T>;
  delete<T>(url: string, options?: RequestOptions): Promise<T>;
  upload<T>(url: string, file: File, data?: any, options?: RequestOptions & {
    onProgress?: (progress: number) => void;
  }): Promise<T>;
}

// ==================== WEBSOCKET CLIENT INTERFACE ====================

export interface WebSocketClient {
  connect(url: string, protocols?: string[]): Promise<void>;
  disconnect(): void;
  send(data: any): void;
  on(event: 'message' | 'open' | 'close' | 'error', handler: (data: any) => void): void;
  off(event: 'message' | 'open' | 'close' | 'error', handler?: (data: any) => void): void;
  isConnected(): boolean;
  reconnect(): Promise<void>;
}

// ==================== API SERVICE INTERFACES ====================

export interface UserApiService {
  getCurrentUser(): Promise<ApiResponse<User>>;
  updateCurrentUser(data: Partial<User>): Promise<ApiResponse<User>>;
  deleteCurrentUser(): Promise<ApiResponse<void>>;
  getUserPreferences(): Promise<ApiResponse<Record<string, any>>>;
  updateUserPreferences(preferences: Record<string, any>): Promise<ApiResponse<void>>;
  getUserStats(): Promise<ApiResponse<any>>;
  getUserSpaces(): Promise<ApiResponse<any[]>>;
  searchUsers(query: string): Promise<ApiResponse<User[]>>;
  getUserById(id: string): Promise<ApiResponse<User>>;
}

export interface NotebookApiService {
  createNotebook(data: NotebookCreateRequest): Promise<ApiResponse<Notebook>>;
  getNotebooks(limit?: number, offset?: number): Promise<PaginatedResponse<Notebook>>;
  getNotebook(id: string): Promise<ApiResponse<Notebook>>;
  updateNotebook(id: string, data: NotebookUpdateRequest): Promise<ApiResponse<Notebook>>;
  deleteNotebook(id: string): Promise<ApiResponse<void>>;
  shareNotebook(id: string, data: NotebookShareRequest): Promise<ApiResponse<void>>;
  searchNotebooks(request: SearchRequest): Promise<SearchResult<Notebook>>;
}

export interface DocumentApiService {
  uploadDocument(data: DocumentUploadRequest): Promise<ApiResponse<Document>>;
  uploadDocumentBase64(data: { notebook_id: string; name: string; content: string; mime_type: string }): Promise<ApiResponse<Document>>;
  getDocuments(notebookId?: string, limit?: number, offset?: number): Promise<PaginatedResponse<Document>>;
  getDocument(id: string): Promise<ApiResponse<Document>>;
  updateDocument(id: string, data: Partial<Document>): Promise<ApiResponse<Document>>;
  deleteDocument(id: string): Promise<ApiResponse<void>>;
  getDocumentStatus(id: string): Promise<ApiResponse<any>>;
  reprocessDocument(id: string): Promise<ApiResponse<ProcessingJob>>;
  refreshProcessingResults(): Promise<ApiResponse<void>>;
  downloadDocument(id: string): Promise<Blob>;
  getDocumentUrl(id: string): Promise<ApiResponse<{ url: string }>>;
  searchDocuments(request: SearchRequest): Promise<SearchResult<Document>>;
}

export interface MLApiService {
  // Model Management
  createModel(data: MLModelCreateRequest): Promise<ApiResponse<MLModel>>;
  getModels(limit?: number, offset?: number): Promise<PaginatedResponse<MLModel>>;
  getModel(id: string): Promise<ApiResponse<MLModel>>;
  updateModel(id: string, data: Partial<MLModel>): Promise<ApiResponse<MLModel>>;
  deleteModel(id: string): Promise<ApiResponse<void>>;
  deployModel(id: string, config?: any): Promise<ApiResponse<MLModel>>;
  
  // Experiment Management
  createExperiment(data: MLExperimentCreateRequest): Promise<ApiResponse<MLExperiment>>;
  getExperiments(modelId?: string, limit?: number, offset?: number): Promise<PaginatedResponse<MLExperiment>>;
  getExperiment(id: string): Promise<ApiResponse<MLExperiment>>;
  updateExperiment(id: string, data: Partial<MLExperiment>): Promise<ApiResponse<MLExperiment>>;
  deleteExperiment(id: string): Promise<ApiResponse<void>>;
  
  // Analytics
  getAnalytics(period?: 'hourly' | 'daily' | 'weekly' | 'monthly'): Promise<ApiResponse<any>>;
}

export interface WorkflowApiService {
  createWorkflow(data: WorkflowCreateRequest): Promise<ApiResponse<Workflow>>;
  getWorkflows(limit?: number, offset?: number): Promise<PaginatedResponse<Workflow>>;
  getWorkflow(id: string): Promise<ApiResponse<Workflow>>;
  updateWorkflow(id: string, data: Partial<Workflow>): Promise<ApiResponse<Workflow>>;
  deleteWorkflow(id: string): Promise<ApiResponse<void>>;
  executeWorkflow(id: string, data?: any): Promise<ApiResponse<WorkflowExecution>>;
  updateWorkflowStatus(id: string, status: string): Promise<ApiResponse<Workflow>>;
  getWorkflowExecutions(id: string, limit?: number, offset?: number): Promise<PaginatedResponse<WorkflowExecution>>;
  getWorkflowAnalytics(): Promise<ApiResponse<any>>;
}

export interface StreamApiService {
  // Stream Source Management
  createStreamSource(data: StreamSourceCreateRequest): Promise<ApiResponse<StreamSource>>;
  getStreamSources(limit?: number, offset?: number): Promise<PaginatedResponse<StreamSource>>;
  getStreamSource(id: string): Promise<ApiResponse<StreamSource>>;
  updateStreamSource(id: string, data: Partial<StreamSource>): Promise<ApiResponse<StreamSource>>;
  deleteStreamSource(id: string): Promise<ApiResponse<void>>;
  updateStreamSourceStatus(id: string, status: string): Promise<ApiResponse<StreamSource>>;
  
  // Live Events
  ingestEvent(sourceId: string, data: any): Promise<ApiResponse<LiveEvent>>;
  getLiveEvents(filters?: StreamFilters, limit?: number, offset?: number): Promise<PaginatedResponse<LiveEvent>>;
  getLiveEvent(id: string): Promise<ApiResponse<LiveEvent>>;
  
  // Analytics
  getStreamAnalytics(period?: string): Promise<ApiResponse<StreamAnalytics>>;
  getRealtimeAnalytics(): Promise<ApiResponse<StreamAnalytics>>;
  
  // WebSocket Streaming
  connectToLiveStream(filters?: StreamFilters): Promise<WebSocketClient>;
}

export interface ChunkApiService {
  getFileChunks(fileId: string, limit?: number, offset?: number): Promise<PaginatedResponse<any>>;
  getChunk(fileId: string, chunkId: string): Promise<ApiResponse<any>>;
  reprocessFileWithStrategy(fileId: string, strategy: string): Promise<ApiResponse<ProcessingJob>>;
  searchChunks(request: ChunkSearchRequest): Promise<ApiResponse<ChunkSearchResult[]>>;
  getAvailableStrategies(): Promise<ApiResponse<string[]>>;
  getOptimalStrategy(data: any): Promise<ApiResponse<string>>;
}

export interface JobApiService {
  getJobStatus(id: string): Promise<ApiResponse<ProcessingJob>>;
  connectToJobStream(id: string): Promise<WebSocketClient>;
}

export interface OrganizationApiService {
  createOrganization(data: any): Promise<ApiResponse<Organization>>;
  getOrganizations(): Promise<ApiResponse<Organization[]>>;
  getOrganization(id: string): Promise<ApiResponse<Organization>>;
  updateOrganization(id: string, data: Partial<Organization>): Promise<ApiResponse<Organization>>;
  deleteOrganization(id: string): Promise<ApiResponse<void>>;
  getOrganizationMembers(id: string): Promise<ApiResponse<any[]>>;
  inviteOrganizationMember(id: string, data: any): Promise<ApiResponse<void>>;
  updateOrganizationMemberRole(id: string, userId: string, role: string): Promise<ApiResponse<void>>;
  removeOrganizationMember(id: string, userId: string): Promise<ApiResponse<void>>;
}

export interface SpaceApiService {
  createSpace(data: any): Promise<ApiResponse<any>>;
  getSpaces(): Promise<ApiResponse<any[]>>;
  getSpace(id: string): Promise<ApiResponse<any>>;
  updateSpace(id: string, data: any): Promise<ApiResponse<any>>;
  deleteSpace(id: string): Promise<ApiResponse<void>>;
}

export interface HealthApiService {
  healthCheck(): Promise<HealthCheck>;
  livenessCheck(): Promise<ApiResponse<{ status: string }>>;
  readinessCheck(): Promise<ApiResponse<{ status: string }>>;
  getMetrics(): Promise<SystemMetrics>;
}

// ==================== MAIN API CLIENT INTERFACE ====================

export interface AetherApiClient {
  // Configuration
  config: ApiClientConfig;
  
  // Core HTTP client
  http: HttpClient;
  
  // Service APIs
  users: UserApiService;
  notebooks: NotebookApiService;
  documents: DocumentApiService;
  ml: MLApiService;
  workflows: WorkflowApiService;
  streams: StreamApiService;
  chunks: ChunkApiService;
  jobs: JobApiService;
  organizations: OrganizationApiService;
  spaces: SpaceApiService;
  health: HealthApiService;
  
  // Authentication
  setAuthToken(token: string): void;
  clearAuth(): void;
  isAuthenticated(): boolean;
  
  // Space Context
  setSpaceContext(context: Partial<SpaceContext>): void;
  getSpaceContext(): SpaceContext | null;
  
  // WebSocket Management
  connectWebSocket(endpoint: string, options?: any): Promise<WebSocketClient>;
  disconnectAllWebSockets(): void;
  
  // Error Handling
  onError(handler: (error: any) => void): void;
  offError(handler?: (error: any) => void): void;
}

// ==================== FACTORY INTERFACES ====================

export interface ApiClientFactory {
  create(config: Partial<ApiClientConfig>): AetherApiClient;
  createWithAuth(config: Partial<ApiClientConfig>, authToken: string): AetherApiClient;
}

// ==================== REACT HOOKS TYPES ====================

export interface UseApiOptions<T> {
  initialData?: T;
  enabled?: boolean;
  refetchOnMount?: boolean;
  refetchOnWindowFocus?: boolean;
  staleTime?: number;
  cacheTime?: number;
  onSuccess?: (data: T) => void;
  onError?: (error: Error) => void;
}

export interface UseApiResult<T> {
  data: T | undefined;
  error: Error | null;
  loading: boolean;
  refetch: () => Promise<void>;
  mutate: (data: T) => void;
}

export interface UseMutationOptions<TData, TVariables> {
  onSuccess?: (data: TData, variables: TVariables) => void;
  onError?: (error: Error, variables: TVariables) => void;
  onSettled?: (data: TData | undefined, error: Error | null, variables: TVariables) => void;
}

export interface UseMutationResult<TData, TVariables> {
  mutate: (variables: TVariables) => Promise<TData>;
  mutateAsync: (variables: TVariables) => Promise<TData>;
  data: TData | undefined;
  error: Error | null;
  loading: boolean;
  reset: () => void;
}

// ==================== STREAMING & REAL-TIME TYPES ====================

export interface StreamConnection {
  id: string;
  endpoint: string;
  client: WebSocketClient;
  filters?: StreamFilters;
  status: 'connecting' | 'connected' | 'disconnected' | 'error';
  reconnectAttempts: number;
  lastMessage?: string;
}

export interface RealtimeEvent {
  type: 'job_update' | 'live_event' | 'analytics_update' | 'system_notification';
  data: JobStatusUpdate | LiveEvent | StreamAnalytics | any;
  timestamp: string;
  source: string;
}

export interface StreamManager {
  connections: Map<string, StreamConnection>;
  connect(endpoint: string, options?: any): Promise<string>;
  disconnect(connectionId: string): void;
  disconnectAll(): void;
  send(connectionId: string, data: any): void;
  onMessage(handler: (connectionId: string, data: any) => void): void;
  onStatusChange(handler: (connectionId: string, status: StreamConnection['status']) => void): void;
  getConnection(id: string): StreamConnection | undefined;
}

// ==================== ERROR TYPES ====================

export interface ApiErrorResponse {
  error: string;
  message: string;
  details?: Record<string, any>;
  status: number;
  timestamp: string;
  request_id?: string;
}

export class ApiError extends Error {
  status: number;
  response?: ApiErrorResponse;
  request?: any;
  
  constructor(message: string, status: number, response?: ApiErrorResponse, request?: any);
}

export class NetworkError extends ApiError {
  constructor(message: string, request?: any);
}

export class AuthenticationError extends ApiError {
  constructor(message?: string);
}

export class AuthorizationError extends ApiError {
  constructor(message?: string);
}

export class ValidationError extends ApiError {
  constructor(message: string, details?: Record<string, any>);
}

export class NotFoundError extends ApiError {
  constructor(resource: string);
}

export class ConflictError extends ApiError {
  constructor(message: string);
}

export class RateLimitError extends ApiError {
  retryAfter?: number;
  constructor(message: string, retryAfter?: number);
}

export class ServerError extends ApiError {
  constructor(message: string);
}

// ==================== UTILITY TYPES ====================

export type QueryKey = readonly unknown[];

export interface InfiniteQueryResult<T> extends UseApiResult<T> {
  hasNextPage: boolean;
  hasPreviousPage: boolean;
  fetchNextPage: () => Promise<void>;
  fetchPreviousPage: () => Promise<void>;
  isFetchingNextPage: boolean;
  isFetchingPreviousPage: boolean;
}

export interface CacheEntry<T> {
  data: T;
  timestamp: number;
  staleTime: number;
  cacheTime: number;
}

export interface QueryCache {
  get<T>(key: QueryKey): CacheEntry<T> | undefined;
  set<T>(key: QueryKey, data: T, options?: { staleTime?: number; cacheTime?: number }): void;
  invalidate(key: QueryKey): void;
  clear(): void;
  size(): number;
}

// ==================== MOCK & TESTING TYPES ====================

export interface MockApiResponse<T> {
  data: T;
  delay?: number;
  status?: number;
  headers?: Record<string, string>;
}

export interface MockApiClient extends AetherApiClient {
  mockResponse<T>(endpoint: string, response: MockApiResponse<T>): void;
  mockError(endpoint: string, error: ApiError): void;
  clearMocks(): void;
  getMockHistory(): Array<{ endpoint: string; method: string; data?: any; timestamp: number }>;
}

export interface TestHelpers {
  createMockClient(): MockApiClient;
  createMockData: {
    user(overrides?: Partial<User>): User;
    notebook(overrides?: Partial<Notebook>): Notebook;
    document(overrides?: Partial<Document>): Document;
    mlModel(overrides?: Partial<MLModel>): MLModel;
    workflow(overrides?: Partial<Workflow>): Workflow;
    streamSource(overrides?: Partial<StreamSource>): StreamSource;
    liveEvent(overrides?: Partial<LiveEvent>): LiveEvent;
  };
  waitForApiCall(client: MockApiClient, endpoint: string, timeout?: number): Promise<void>;
}