import type { ProductWorkItem } from '../api/workItems/productApi';

/** One-shot identity-only navigation state resolved by Work Items after load. */
export interface WorkItemNavigationIntent {
  workItemId: string;
}

export function createWorkItemNavigationIntent(workItemId: string): WorkItemNavigationIntent {
  return { workItemId };
}

export function resolveWorkItemNavigationIntent(
  intent: WorkItemNavigationIntent,
  items: ProductWorkItem[],
) {
  return items.some((item) => item.id === intent.workItemId) ? intent.workItemId : null;
}

/** Resolves only after authoritative items load, then always consumes once. */
export function consumeWorkItemNavigationIntent(
  intent: WorkItemNavigationIntent,
  items: ProductWorkItem[],
  select: (id: string) => void,
  consume: () => void,
) {
  const targetID = resolveWorkItemNavigationIntent(intent, items);
  if (targetID) select(targetID);
  consume();
}
