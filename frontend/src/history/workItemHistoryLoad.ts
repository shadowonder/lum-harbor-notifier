import type { HistoryApi, HistoryEventDto } from '../api/history/contracts.ts';

export type WorkItemHistoryState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; asOf: string; events: HistoryEventDto[]; truncated: boolean };

/**
 * Loads the authoritative M12 History linked to one WorkItem. A failure is an
 * `error` state, never an empty or fixture timeline; zero linked events is a
 * valid `ready` state.
 */
export async function loadWorkItemHistory(
  api: Pick<HistoryApi, 'listWorkItemEvents'>,
  workItemId: string,
): Promise<WorkItemHistoryState> {
  try {
    const page = await api.listWorkItemEvents(workItemId);
    return { status: 'ready', asOf: page.asOf, events: page.events, truncated: page.truncated };
  } catch {
    return { status: 'error' };
  }
}
