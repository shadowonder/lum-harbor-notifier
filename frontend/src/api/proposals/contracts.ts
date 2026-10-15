export type ProposalOrigin = 'user_ai' | 'background_ai' | 'rule_engine';
export type ProposalStatus = 'pending' | 'applied' | 'rejected' | 'stale';

export interface ProposalSourceDto {
  id: string;
  label: string;
  detail: string;
  observedAt: string;
}

export interface ProposedWorkItemEntryDto {
  id: string;
  title: string;
  category: 'story' | 'defect' | 'task';
  destination: string;
}

export interface ProposedPlannerItemDto {
  id: string;
  type: string;
  title: string;
  details?: string | null;
  status: 'OPEN';
  scheduledAt?: string | null;
  scheduledDate?: string | null;
  dueAt?: string | null;
  dueDate?: string | null;
  authority?: string;
  relatedWorkItemId?: string;
}

export type ProposalOperationDto =
  | {
      id: string;
      kind: 'SET_WORK_ITEM_TARGET';
      label: string;
      workItemId: string;
      before: string;
      after: string;
    }
  | {
      id: string;
      kind: 'ADD_WORK_ITEM_ENTRIES';
      label: string;
      workItemId: string;
      entries: ProposedWorkItemEntryDto[];
    }
  | { id: string; kind: 'ADD_PLANNER_TODO'; label: string; item: ProposedPlannerItemDto }
  | {
      id: string;
      kind: 'ENRICH_PLANNER_TODO';
      label: string;
      todoId: string;
      before: string;
      after: string;
    }
  | {
      id: string;
      kind: 'RESOLVE_ATTENTION';
      label: string;
      workItemId: string;
      before: string;
      after: string;
    };

export interface ProposalImpactDto {
  surface: 'Dashboard' | 'Work Items' | 'Planner' | 'Notifications';
  summary: string;
}

export interface ChangeProposalDto {
  id: string;
  workItemId?: string;
  context: string;
  title: string;
  summary: string;
  origin: ProposalOrigin;
  status: ProposalStatus;
  sources: ProposalSourceDto[];
  operations: ProposalOperationDto[];
  impacts: ProposalImpactDto[];
  createdAt: string;
  staleReason?: string;
}

export interface ProposalApi {
  listProposals(): Promise<ChangeProposalDto[]>;
}
