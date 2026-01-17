/**
 * Aether Enterprise AI Platform - React Hooks Type Definitions
 * 
 * This file provides TypeScript interfaces for React hooks that integrate
 * with the Aether API, ensuring type-safe state management and real-time updates.
 */

import { 
  User, Notebook, Document, MLModel, MLExperiment, Workflow, WorkflowExecution,
  StreamSource, LiveEvent, StreamAnalytics, ProcessingJob, PaginatedResponse,
  SearchRequest, SearchResult, SpaceContext, NotebookCreateRequest, NotebookUpdateRequest,
  DocumentUploadRequest, MLModelCreateRequest, WorkflowCreateRequest, StreamFilters,
  JobStatusUpdate, RealtimeEvent
} from './platform';

import {
  UseApiOptions, UseApiResult, UseMutationOptions, UseMutationResult,
  InfiniteQueryResult, AetherApiClient
} from './api-client';

// ==================== CORE HOOKS ====================

export function useAetherApi(): AetherApiClient;
export function useSpaceContext(): [SpaceContext | null, (context: Partial<SpaceContext>) => void];
export function useAuth(): {
  user: User | null;
  isAuthenticated: boolean;
  login: (token: string) => void;
  logout: () => void;
  loading: boolean;
  error: Error | null;
};

// ==================== USER HOOKS ====================

export function useCurrentUser(options?: UseApiOptions<User>): UseApiResult<User>;
export function useUpdateUser(): UseMutationResult<User, Partial<User>>;
export function useUserPreferences(options?: UseApiOptions<Record<string, any>>): UseApiResult<Record<string, any>>;
export function useUpdateUserPreferences(): UseMutationResult<void, Record<string, any>>;
export function useUserStats(options?: UseApiOptions<any>): UseApiResult<any>;
export function useUserSpaces(options?: UseApiOptions<any[]>): UseApiResult<any[]>;
export function useSearchUsers(query: string, options?: UseApiOptions<User[]>): UseApiResult<User[]>;

// ==================== NOTEBOOK HOOKS ====================

export function useNotebooks(
  limit?: number, 
  offset?: number, 
  options?: UseApiOptions<PaginatedResponse<Notebook>>
): UseApiResult<PaginatedResponse<Notebook>>;

export function useNotebook(
  id: string, 
  options?: UseApiOptions<Notebook>
): UseApiResult<Notebook>;

export function useCreateNotebook(): UseMutationResult<Notebook, NotebookCreateRequest>;

export function useUpdateNotebook(): UseMutationResult<Notebook, { id: string; data: NotebookUpdateRequest }>;

export function useDeleteNotebook(): UseMutationResult<void, string>;

export function useShareNotebook(): UseMutationResult<void, { id: string; data: any }>;

export function useSearchNotebooks(
  request: SearchRequest,
  options?: UseApiOptions<SearchResult<Notebook>>
): UseApiResult<SearchResult<Notebook>>;

export function useInfiniteNotebooks(
  limit?: number
): InfiniteQueryResult<PaginatedResponse<Notebook>>;

// ==================== DOCUMENT HOOKS ====================

export function useDocuments(
  notebookId?: string,
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<Document>>
): UseApiResult<PaginatedResponse<Document>>;

export function useDocument(
  id: string,
  options?: UseApiOptions<Document>
): UseApiResult<Document>;

export function useUploadDocument(): UseMutationResult<Document, DocumentUploadRequest & {
  onProgress?: (progress: number) => void;
}>;

export function useUploadDocumentBase64(): UseMutationResult<Document, {
  notebook_id: string;
  name: string;
  content: string;
  mime_type: string;
}>;

export function useUpdateDocument(): UseMutationResult<Document, { id: string; data: Partial<Document> }>;

export function useDeleteDocument(): UseMutationResult<void, string>;

export function useDocumentStatus(
  id: string,
  options?: UseApiOptions<any>
): UseApiResult<any>;

export function useReprocessDocument(): UseMutationResult<ProcessingJob, string>;

export function useDownloadDocument(): UseMutationResult<Blob, string>;

export function useSearchDocuments(
  request: SearchRequest,
  options?: UseApiOptions<SearchResult<Document>>
): UseApiResult<SearchResult<Document>>;

// ==================== ML HOOKS ====================

export function useMLModels(
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<MLModel>>
): UseApiResult<PaginatedResponse<MLModel>>;

export function useMLModel(
  id: string,
  options?: UseApiOptions<MLModel>
): UseApiResult<MLModel>;

export function useCreateMLModel(): UseMutationResult<MLModel, MLModelCreateRequest>;

export function useUpdateMLModel(): UseMutationResult<MLModel, { id: string; data: Partial<MLModel> }>;

export function useDeleteMLModel(): UseMutationResult<void, string>;

export function useDeployMLModel(): UseMutationResult<MLModel, { id: string; config?: any }>;

export function useMLExperiments(
  modelId?: string,
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<MLExperiment>>
): UseApiResult<PaginatedResponse<MLExperiment>>;

export function useMLExperiment(
  id: string,
  options?: UseApiOptions<MLExperiment>
): UseApiResult<MLExperiment>;

export function useCreateMLExperiment(): UseMutationResult<MLExperiment, any>;

export function useUpdateMLExperiment(): UseMutationResult<MLExperiment, { id: string; data: Partial<MLExperiment> }>;

export function useDeleteMLExperiment(): UseMutationResult<void, string>;

export function useMLAnalytics(
  period?: 'hourly' | 'daily' | 'weekly' | 'monthly',
  options?: UseApiOptions<any>
): UseApiResult<any>;

// ==================== WORKFLOW HOOKS ====================

export function useWorkflows(
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<Workflow>>
): UseApiResult<PaginatedResponse<Workflow>>;

export function useWorkflow(
  id: string,
  options?: UseApiOptions<Workflow>
): UseApiResult<Workflow>;

export function useCreateWorkflow(): UseMutationResult<Workflow, WorkflowCreateRequest>;

export function useUpdateWorkflow(): UseMutationResult<Workflow, { id: string; data: Partial<Workflow> }>;

export function useDeleteWorkflow(): UseMutationResult<void, string>;

export function useExecuteWorkflow(): UseMutationResult<WorkflowExecution, { id: string; data?: any }>;

export function useUpdateWorkflowStatus(): UseMutationResult<Workflow, { id: string; status: string }>;

export function useWorkflowExecutions(
  id: string,
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<WorkflowExecution>>
): UseApiResult<PaginatedResponse<WorkflowExecution>>;

export function useWorkflowAnalytics(options?: UseApiOptions<any>): UseApiResult<any>;

// ==================== STREAMING HOOKS ====================

export function useStreamSources(
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<StreamSource>>
): UseApiResult<PaginatedResponse<StreamSource>>;

export function useStreamSource(
  id: string,
  options?: UseApiOptions<StreamSource>
): UseApiResult<StreamSource>;

export function useCreateStreamSource(): UseMutationResult<StreamSource, any>;

export function useUpdateStreamSource(): UseMutationResult<StreamSource, { id: string; data: Partial<StreamSource> }>;

export function useDeleteStreamSource(): UseMutationResult<void, string>;

export function useUpdateStreamSourceStatus(): UseMutationResult<StreamSource, { id: string; status: string }>;

export function useLiveEvents(
  filters?: StreamFilters,
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<LiveEvent>>
): UseApiResult<PaginatedResponse<LiveEvent>>;

export function useLiveEvent(
  id: string,
  options?: UseApiOptions<LiveEvent>
): UseApiResult<LiveEvent>;

export function useIngestEvent(): UseMutationResult<LiveEvent, { sourceId: string; data: any }>;

export function useStreamAnalytics(
  period?: string,
  options?: UseApiOptions<StreamAnalytics>
): UseApiResult<StreamAnalytics>;

export function useRealtimeAnalytics(options?: UseApiOptions<StreamAnalytics>): UseApiResult<StreamAnalytics>;

// ==================== REAL-TIME HOOKS ====================

export function useLiveStream(filters?: StreamFilters): {
  events: LiveEvent[];
  analytics: StreamAnalytics | null;
  connected: boolean;
  error: Error | null;
  connect: () => void;
  disconnect: () => void;
  clearEvents: () => void;
};

export function useJobProgress(jobId: string): {
  job: ProcessingJob | null;
  progress: number;
  status: string;
  completed: boolean;
  error: Error | null;
  connect: () => void;
  disconnect: () => void;
};

export function useRealtimeEvents(): {
  events: RealtimeEvent[];
  subscribe: (eventType: string, handler: (event: RealtimeEvent) => void) => () => void;
  clearEvents: () => void;
  connected: boolean;
  error: Error | null;
};

export function useDocumentStatusStream(documentId: string): {
  status: any | null;
  progress: number;
  completed: boolean;
  error: Error | null;
  connect: () => void;
  disconnect: () => void;
};

// ==================== SEARCH & CHUNK HOOKS ====================

export function useSearchChunks(
  request: any,
  options?: UseApiOptions<any[]>
): UseApiResult<any[]>;

export function useFileChunks(
  fileId: string,
  limit?: number,
  offset?: number,
  options?: UseApiOptions<PaginatedResponse<any>>
): UseApiResult<PaginatedResponse<any>>;

export function useChunk(
  fileId: string,
  chunkId: string,
  options?: UseApiOptions<any>
): UseApiResult<any>;

export function useReprocessFile(): UseMutationResult<ProcessingJob, { fileId: string; strategy: string }>;

export function useAvailableStrategies(options?: UseApiOptions<string[]>): UseApiResult<string[]>;

// ==================== ORGANIZATION & SPACE HOOKS ====================

export function useOrganizations(options?: UseApiOptions<any[]>): UseApiResult<any[]>;

export function useOrganization(
  id: string,
  options?: UseApiOptions<any>
): UseApiResult<any>;

export function useCreateOrganization(): UseMutationResult<any, any>;

export function useUpdateOrganization(): UseMutationResult<any, { id: string; data: any }>;

export function useDeleteOrganization(): UseMutationResult<void, string>;

export function useSpaces(options?: UseApiOptions<any[]>): UseApiResult<any[]>;

export function useSpace(
  id: string,
  options?: UseApiOptions<any>
): UseApiResult<any>;

export function useCreateSpace(): UseMutationResult<any, any>;

export function useUpdateSpace(): UseMutationResult<any, { id: string; data: any }>;

export function useDeleteSpace(): UseMutationResult<void, string>;

// ==================== HEALTH & MONITORING HOOKS ====================

export function useHealthCheck(options?: UseApiOptions<any>): UseApiResult<any>;

export function useSystemMetrics(options?: UseApiOptions<any>): UseApiResult<any>;

// ==================== UTILITY HOOKS ====================

export function useDebounce<T>(value: T, delay: number): T;

export function useLocalStorage<T>(
  key: string,
  initialValue: T
): [T, (value: T | ((prev: T) => T)) => void];

export function useSessionStorage<T>(
  key: string,
  initialValue: T
): [T, (value: T | ((prev: T) => T)) => void];

export function usePrevious<T>(value: T): T | undefined;

export function useToggle(initialValue?: boolean): [boolean, (value?: boolean) => void];

export function useAsync<T, E = Error>(
  asyncFunction: () => Promise<T>,
  dependencies?: React.DependencyList
): {
  data: T | null;
  error: E | null;
  loading: boolean;
  execute: () => Promise<void>;
};

export function useIntersectionObserver(
  ref: React.RefObject<Element>,
  options?: IntersectionObserverInit
): boolean;

export function useOnScreen(ref: React.RefObject<Element>): boolean;

export function useEventListener<T extends keyof WindowEventMap>(
  eventName: T,
  handler: (event: WindowEventMap[T]) => void,
  element?: Element | Window
): void;

export function useClickOutside(
  ref: React.RefObject<Element>,
  handler: () => void
): void;

export function useKeyPress(targetKey: string): boolean;

export function useCopyToClipboard(): [boolean, (text: string) => void];

export function usePermissions(resource: string): {
  canRead: boolean;
  canWrite: boolean;
  canAdmin: boolean;
  canDelete: boolean;
  loading: boolean;
};

// ==================== FORM HOOKS ====================

export interface FormField<T = any> {
  value: T;
  error?: string;
  touched: boolean;
  dirty: boolean;
  onChange: (value: T) => void;
  onBlur: () => void;
  validate: () => boolean;
  reset: () => void;
}

export interface FormState<T extends Record<string, any>> {
  values: T;
  errors: Partial<Record<keyof T, string>>;
  touched: Partial<Record<keyof T, boolean>>;
  dirty: boolean;
  valid: boolean;
  submitting: boolean;
}

export interface FormActions<T extends Record<string, any>> {
  setValue: <K extends keyof T>(name: K, value: T[K]) => void;
  setError: <K extends keyof T>(name: K, error: string) => void;
  setTouched: <K extends keyof T>(name: K, touched: boolean) => void;
  reset: (values?: Partial<T>) => void;
  submit: () => Promise<void>;
  validate: () => boolean;
  getField: <K extends keyof T>(name: K) => FormField<T[K]>;
}

export function useForm<T extends Record<string, any>>(
  initialValues: T,
  options?: {
    validationSchema?: any;
    onSubmit?: (values: T) => Promise<void> | void;
    validateOnChange?: boolean;
    validateOnBlur?: boolean;
  }
): [FormState<T>, FormActions<T>];

export function useFormField<T>(
  name: string,
  initialValue: T,
  validation?: (value: T) => string | undefined
): FormField<T>;

// ==================== PAGINATION HOOKS ====================

export interface PaginationState {
  page: number;
  limit: number;
  offset: number;
  total: number;
  totalPages: number;
  hasNext: boolean;
  hasPrevious: boolean;
}

export interface PaginationActions {
  setPage: (page: number) => void;
  setLimit: (limit: number) => void;
  nextPage: () => void;
  previousPage: () => void;
  firstPage: () => void;
  lastPage: () => void;
}

export function usePagination(
  initialPage?: number,
  initialLimit?: number
): [PaginationState, PaginationActions];

export function useInfiniteScroll<T>(
  fetchMore: () => Promise<T[]>,
  hasMore: boolean,
  options?: {
    threshold?: number;
    rootMargin?: string;
  }
): {
  loading: boolean;
  error: Error | null;
  ref: React.RefObject<Element>;
};

// ==================== TABLE HOOKS ====================

export interface TableColumn<T> {
  key: keyof T;
  label: string;
  sortable?: boolean;
  width?: string;
  render?: (value: any, item: T, index: number) => React.ReactNode;
}

export interface TableState<T> {
  data: T[];
  loading: boolean;
  error: Error | null;
  selectedItems: T[];
  sortBy?: keyof T;
  sortOrder: 'asc' | 'desc';
  filters: Record<string, any>;
}

export interface TableActions<T> {
  selectItem: (item: T) => void;
  selectItems: (items: T[]) => void;
  selectAll: () => void;
  clearSelection: () => void;
  sort: (column: keyof T) => void;
  filter: (filters: Record<string, any>) => void;
  refresh: () => void;
}

export function useTable<T extends { id: string }>(
  data: T[],
  options?: {
    selectable?: boolean;
    sortable?: boolean;
    filterable?: boolean;
  }
): [TableState<T>, TableActions<T>];

export function useSorting<T>(
  data: T[],
  initialSortBy?: keyof T,
  initialSortOrder?: 'asc' | 'desc'
): {
  sortedData: T[];
  sortBy?: keyof T;
  sortOrder: 'asc' | 'desc';
  sort: (column: keyof T) => void;
};

export function useFiltering<T>(
  data: T[],
  initialFilters?: Record<string, any>
): {
  filteredData: T[];
  filters: Record<string, any>;
  setFilter: (key: string, value: any) => void;
  clearFilters: () => void;
};

export function useSelection<T extends { id: string }>(
  data: T[]
): {
  selectedItems: T[];
  selectedIds: string[];
  isSelected: (item: T) => boolean;
  isAllSelected: boolean;
  isIndeterminate: boolean;
  selectItem: (item: T) => void;
  selectItems: (items: T[]) => void;
  selectAll: () => void;
  clearSelection: () => void;
  toggleItem: (item: T) => void;
  toggleAll: () => void;
};

// ==================== NOTIFICATION HOOKS ====================

export interface Notification {
  id: string;
  type: 'success' | 'error' | 'warning' | 'info';
  title: string;
  message?: string;
  duration?: number;
  persistent?: boolean;
  actions?: Array<{
    label: string;
    onClick: () => void;
    variant?: 'primary' | 'secondary';
  }>;
}

export function useNotifications(): {
  notifications: Notification[];
  add: (notification: Omit<Notification, 'id'>) => string;
  remove: (id: string) => void;
  clear: () => void;
  success: (title: string, message?: string) => string;
  error: (title: string, message?: string) => string;
  warning: (title: string, message?: string) => string;
  info: (title: string, message?: string) => string;
};

// ==================== THEME HOOKS ====================

export interface Theme {
  mode: 'light' | 'dark';
  colors: Record<string, string>;
  spacing: Record<string, string>;
  typography: Record<string, any>;
  breakpoints: Record<string, string>;
}

export function useTheme(): {
  theme: Theme;
  mode: 'light' | 'dark';
  toggleMode: () => void;
  setMode: (mode: 'light' | 'dark') => void;
};