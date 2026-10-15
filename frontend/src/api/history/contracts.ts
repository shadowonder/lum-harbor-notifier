export type HistoryScope = 'planner' | 'workItems' | 'attention' | 'automation' | 'evidence';
export type CanonicalHistoryScope =
  | 'TRACKING'
  | 'ATTENTION'
  | 'NOTIFICATION'
  | 'PROPOSAL'
  | 'WORK_ITEM'
  | 'PLANNER'
  | 'SOURCE'
  | 'AI';

export type HistoryEventType = string;

export type HistoryTraceStage =
  | 'source'
  | 'evidence'
  | 'state'
  | 'evaluation'
  | 'attention'
  | 'notification'
  | 'action'
  | 'resolution';

export interface HistoryActorDto {
  label: string;
  kind: 'user' | 'system' | 'integration' | 'automation';
}

export interface HistorySubjectDto {
  id: string;
  type: string;
  label: string;
}

export interface HistoryDetailDto {
  label: string;
  value: string;
}

export interface HistoryEventDto {
  id: string;
  occurredAt: string;
  eventType: HistoryEventType;
  scope: HistoryScope;
  canonicalScope?: CanonicalHistoryScope;
  actor: HistoryActorDto;
  subject: HistorySubjectDto;
  workItemId?: string;
  workItemTitle?: string;
  title: string;
  summary: string;
  before?: string;
  after?: string;
  source?: string;
  reason?: string;
  correlationId?: string;
  causationId?: string;
  revisionId?: string;
  proposalId?: string;
  aiRunId?: string;
  sourceRevisionId?: string;
  traceId?: string;
  traceStage?: HistoryTraceStage;
  details?: HistoryDetailDto[];
  meaningful: boolean;
}

export interface HistoryEventQueryDto {
  days: 7 | 30 | 90;
  scope?: CanonicalHistoryScope;
  search?: string;
}

export interface HistoryEventListDto {
  asOf: string;
  events: HistoryEventDto[];
  truncated: boolean;
}

export interface HistoryTraceSummaryDto {
  id: string;
  eventCount: number;
  firstOccurredAt: string;
  lastOccurredAt: string;
  title: string;
  representativeScope: CanonicalHistoryScope;
  workItemId?: string;
  scopes: CanonicalHistoryScope[];
}

export interface HistoryTraceDto {
  id: string;
  events: HistoryEventDto[];
  truncated: boolean;
}

/** The Source revision a causal chain explains back to, with its captured payload. */
export interface HistoryCausalSourceDto {
  sourceRevisionId: string;
  extensionId: string;
  instanceId: string;
  sourceKind: string;
  payloadJson: string;
}

export interface HistoryCausalChainDto {
  targetEventId: string;
  targetEvent?: HistoryEventDto;
  rootEvent?: HistoryEventDto;
  events: HistoryEventDto[];
  rootSource?: HistoryCausalSourceDto;
}

export interface HistoryApi {
  listEvents(query?: HistoryEventQueryDto): Promise<HistoryEventListDto>;
  listTraces(): Promise<{ traces: HistoryTraceSummaryDto[]; truncated: boolean }>;
  getTrace(traceId: string): Promise<HistoryTraceDto>;
  /** Events authoritatively linked to one WorkItem (newest first, bounded); not date-ranged. */
  listWorkItemEvents(workItemId: string): Promise<HistoryEventListDto>;
  getCausalChain?(targetEventId: string): Promise<HistoryCausalChainDto>;
  getCausalChainBySubject?(subjectEntityId: string): Promise<HistoryCausalChainDto>;
}
