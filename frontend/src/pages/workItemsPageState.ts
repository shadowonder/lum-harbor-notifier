import type {
  ProductWorkItem,
  ProductWorkItemLifecycle,
  ProductWorkItemSummary,
} from '../api/workItems/productApi';

export interface WorkItemsPageState {
  status: 'loading' | 'ready' | 'error';
  items: ProductWorkItem[];
  /** Authoritative Tracking/Attention summaries by WorkItem ID (null section = unavailable). */
  summaries: Record<string, ProductWorkItemSummary>;
  readError: string | null;
  submitting: boolean;
  createError: string | null;
  selectedWorkItemId: string | null;
  lifecycleSaving: boolean;
  lifecycleError: string | null;
}

export type WorkItemsPageEvent =
  | { type: 'loadStarted' }
  | {
      type: 'loadSucceeded';
      items: ProductWorkItem[];
      summaries?: Record<string, ProductWorkItemSummary>;
    }
  | { type: 'loadFailed'; message: string }
  | { type: 'createStarted' }
  | { type: 'createSucceeded'; item: ProductWorkItem }
  | { type: 'createFailed'; message: string }
  | { type: 'selected'; id: string }
  | { type: 'closed' }
  | { type: 'lifecycleStarted' }
  | { type: 'lifecycleSucceeded'; item: ProductWorkItem }
  | { type: 'lifecycleFailed'; message: string };

export const initialWorkItemsPageState: WorkItemsPageState = {
  status: 'loading',
  items: [],
  summaries: {},
  readError: null,
  submitting: false,
  createError: null,
  selectedWorkItemId: null,
  lifecycleSaving: false,
  lifecycleError: null,
};

function orderWorkItems(items: ProductWorkItem[]) {
  return [...items].sort(
    (left, right) =>
      left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id),
  );
}

const EMPTY_TRACKING = {
  trackedItemCount: 0,
  workflow: null,
  items: [],
  stateDistribution: {
    notStarted: 0,
    inProgress: 0,
    waiting: 0,
    blocked: 0,
    failed: 0,
    unknown: 0,
    skipped: 0,
    completed: 0,
  },
  activeFinalTarget: null,
};
const EMPTY_ATTENTION = {
  openIssueCount: 0,
  actionableIssueCount: 0,
  snoozedIssueCount: 0,
  highestActionableSeverity: null,
  topActionable: [],
};

/** Valid zero summary for a freshly created WorkItem; a section unavailable at load stays unavailable. */
function newWorkItemSummary(
  existing: Record<string, ProductWorkItemSummary>,
): ProductWorkItemSummary {
  const sample = Object.values(existing)[0];
  return {
    tracking: sample && sample.tracking === null ? null : EMPTY_TRACKING,
    attention: sample && sample.attention === null ? null : EMPTY_ATTENTION,
  };
}

function replaceWorkItem(items: ProductWorkItem[], authoritative: ProductWorkItem) {
  return orderWorkItems(items.map((item) => (item.id === authoritative.id ? authoritative : item)));
}

export function workItemsPageReducer(
  state: WorkItemsPageState,
  event: WorkItemsPageEvent,
): WorkItemsPageState {
  switch (event.type) {
    case 'loadStarted':
      return { ...state, status: 'loading', readError: null };
    case 'loadSucceeded':
      return {
        ...state,
        status: 'ready',
        items: event.items,
        summaries: event.summaries ?? {},
        readError: null,
      };
    case 'loadFailed':
      return {
        ...state,
        status: 'error',
        items: [],
        summaries: {},
        readError: event.message,
        selectedWorkItemId: null,
        lifecycleSaving: false,
        lifecycleError: null,
      };
    case 'createStarted':
      return { ...state, submitting: true, createError: null };
    case 'createSucceeded':
      return {
        ...state,
        status: 'ready',
        items: orderWorkItems([...state.items, event.item]),
        // A new WorkItem has no tracked work and no attention: a valid zero
        // summary, but only where each section was readable at load time.
        summaries: {
          ...state.summaries,
          [event.item.id]: newWorkItemSummary(state.summaries),
        },
        submitting: false,
        createError: null,
      };
    case 'createFailed':
      return { ...state, submitting: false, createError: event.message };
    case 'selected':
      return {
        ...state,
        selectedWorkItemId: event.id,
        lifecycleError: null,
      };
    case 'closed':
      return {
        ...state,
        selectedWorkItemId: null,
        lifecycleSaving: false,
        lifecycleError: null,
      };
    case 'lifecycleStarted':
      if (state.lifecycleSaving) return state;
      return { ...state, lifecycleSaving: true, lifecycleError: null };
    case 'lifecycleSucceeded':
      return {
        ...state,
        items: replaceWorkItem(state.items, event.item),
        lifecycleSaving: false,
        lifecycleError: null,
      };
    case 'lifecycleFailed':
      return { ...state, lifecycleSaving: false, lifecycleError: event.message };
  }
}

export function selectedWorkItem(state: WorkItemsPageState): ProductWorkItem | null {
  return state.items.find((item) => item.id === state.selectedWorkItemId) ?? null;
}

export interface WorkItemLifecycleAction {
  label: 'Pause' | 'Resume' | 'Complete' | 'Cancel' | 'Reopen';
  target: ProductWorkItemLifecycle;
}

export function workItemLifecycleActions(
  lifecycle: ProductWorkItemLifecycle,
): WorkItemLifecycleAction[] {
  switch (lifecycle) {
    case 'ACTIVE':
      return [
        { label: 'Pause', target: 'PAUSED' },
        { label: 'Complete', target: 'COMPLETED' },
        { label: 'Cancel', target: 'CANCELLED' },
      ];
    case 'PAUSED':
      return [
        { label: 'Resume', target: 'ACTIVE' },
        { label: 'Complete', target: 'COMPLETED' },
        { label: 'Cancel', target: 'CANCELLED' },
      ];
    case 'COMPLETED':
    case 'CANCELLED':
      return [{ label: 'Reopen', target: 'ACTIVE' }];
  }
}

export function groupWorkItemsByLifecycle(items: ProductWorkItem[]) {
  return {
    current: items.filter(
      (item) => item.lifecycleState === 'ACTIVE' || item.lifecycleState === 'PAUSED',
    ),
    closed: items.filter(
      (item) => item.lifecycleState === 'COMPLETED' || item.lifecycleState === 'CANCELLED',
    ),
  };
}
