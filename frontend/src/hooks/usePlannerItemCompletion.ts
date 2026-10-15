import { useRef, useState } from 'react';
import { completeLocalTodo } from '../api/plannerApi';
import type { PlannerItem } from '../pages/plannerData';

/** User-facing completion outcome that may optionally offer a read retry. */
export interface PlannerCompletionNotice {
  message: string;
  refreshFailed: boolean;
}

interface UsePlannerItemCompletionOptions {
  /** Reloads the caller's authoritative PlannerView after a committed mutation. */
  refresh: () => Promise<void>;
  /** Receives committed item state without changing projection membership. */
  onCommitted?: (item: PlannerItem) => void;
}

function mutationErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Coordinates the shared LOCAL Todo completion lifecycle for independent UI
 * surfaces. The hook owns per-item duplicate protection and post-commit read
 * recovery; callers continue to own their PlannerView request and rendering.
 *
 * Projection arrays are never updated optimistically. A successful backend
 * mutation is followed by the supplied full-view refresh. If that read fails,
 * the notice states that completion committed and offers a refresh-only retry.
 */
export function usePlannerItemCompletion({
  refresh,
  onCommitted,
}: UsePlannerItemCompletionOptions) {
  const [completingItemIds, setCompletingItemIds] = useState<Set<string>>(() => new Set());
  const [notice, setNotice] = useState<PlannerCompletionNotice | null>(null);
  const completingItemIdsRef = useRef(new Set<string>());
  const awaitingRefreshItemIdsRef = useRef(new Set<string>());
  const refreshQueueRef = useRef<Promise<void>>(Promise.resolve());

  /** Updates the synchronous duplicate-click guard before publishing UI state. */
  const setItemCompleting = (id: string, completing: boolean) => {
    if (completing) completingItemIdsRef.current.add(id);
    else completingItemIdsRef.current.delete(id);
    setCompletingItemIds(new Set(completingItemIdsRef.current));
  };

  /** Clears only mutations covered by a successfully completed refresh read. */
  const clearRefreshedItems = (ids: Iterable<string>) => {
    for (const id of ids) {
      awaitingRefreshItemIdsRef.current.delete(id);
      completingItemIdsRef.current.delete(id);
    }
    setCompletingItemIds(new Set(completingItemIdsRef.current));
  };

  /**
   * Serializes refreshes so concurrent completion callers cannot supersede one
   * another. Membership is captured when each read starts; later commits stay
   * pending for the next queued read.
   */
  const queueRefresh = () => {
    let coveredItemIds: string[] = [];
    const queued = refreshQueueRef.current
      .catch(() => undefined)
      .then(() => {
        coveredItemIds = [...awaitingRefreshItemIdsRef.current];
        return refresh();
      })
      .then(() => clearRefreshedItems(coveredItemIds));
    refreshQueueRef.current = queued.catch(() => undefined);
    return queued;
  };

  /** Completes one eligible item through the shared Wails adapter boundary. */
  const complete = async (item: PlannerItem) => {
    const id = item.id.trim();
    if (!id) {
      setNotice({ message: 'Planner item ID must not be empty.', refreshFailed: false });
      return;
    }
    if (item.authority !== 'LOCAL') {
      setNotice({ message: 'This task is managed by an external source.', refreshFailed: false });
      return;
    }
    if (item.status === 'COMPLETED' || completingItemIdsRef.current.has(id)) return;

    setNotice(null);
    setItemCompleting(id, true);

    let committed: PlannerItem;
    try {
      committed = await completeLocalTodo(id);
    } catch (error: unknown) {
      setItemCompleting(id, false);
      setNotice({
        message: `Unable to complete task: ${mutationErrorMessage(error)}`,
        refreshFailed: false,
      });
      return;
    }

    awaitingRefreshItemIdsRef.current.add(id);
    onCommitted?.(committed);

    try {
      await queueRefresh();
      setNotice(null);
    } catch (error: unknown) {
      setNotice({
        message: `Task completed, but Planner refresh failed. Retry refresh. ${mutationErrorMessage(error)}`,
        refreshFailed: true,
      });
    }
  };

  /** Retries only reads for mutations that have already committed. */
  const retryRefresh = async () => {
    try {
      await queueRefresh();
      setNotice(null);
    } catch (error: unknown) {
      setNotice({
        message: `Task completed, but Planner refresh failed. Retry refresh. ${mutationErrorMessage(error)}`,
        refreshFailed: true,
      });
    }
  };

  return { completingItemIds, notice, complete, retryRefresh };
}
