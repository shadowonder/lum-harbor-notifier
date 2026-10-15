import { useCallback, useEffect, useRef, useState } from 'react';
import {
  TrackingUnavailableError,
  type ProductTrackingApi,
  type WorkItemTracking,
} from '../api/tracking/trackingProductApi.ts';
import { runTrackingMutation, type TrackingMutationOutcome } from './trackingMutation.ts';

export type TrackingViewState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ready'; data: WorkItemTracking }
  | { status: 'unavailable' }
  | { status: 'error'; message: string };

/**
 * Loads the authoritative Tracking projection of one WorkItem. Every
 * successful command reloads it (no optimistic local truth). Pass `null` to
 * disable (fixture/prototype rendering).
 */
export function useWorkItemTracking(api: ProductTrackingApi, workItemId: string | null) {
  const [view, setView] = useState<TrackingViewState>(
    workItemId ? { status: 'loading' } : { status: 'idle' },
  );
  const [notice, setNotice] = useState<string | null>(null);
  const [mutating, setMutating] = useState(false);
  const generation = useRef(0);
  const mutatingRef = useRef(false);

  const fetchTracking = useCallback(async (): Promise<WorkItemTracking> => {
    if (!workItemId) throw new Error('No Work Item selected.');
    return api.getWorkItemTracking(workItemId);
  }, [api, workItemId]);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    if (!workItemId) {
      setView({ status: 'idle' });
      return;
    }
    setView((previous) => (previous.status === 'ready' ? previous : { status: 'loading' }));
    try {
      const data = await fetchTracking();
      if (mine === generation.current) setView({ status: 'ready', data });
    } catch (error) {
      if (mine !== generation.current) return;
      setView(
        error instanceof TrackingUnavailableError
          ? { status: 'unavailable' }
          : { status: 'error', message: error instanceof Error ? error.message : String(error) },
      );
    }
  }, [fetchTracking, workItemId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- load resets and fetches authoritative data
    void load();
    return () => {
      generation.current += 1;
    };
  }, [load]);

  /** Reload used after a committed command; rejects so the caller can distinguish it. */
  const reloadOrThrow = useCallback(async () => {
    const mine = ++generation.current;
    const data = await fetchTracking();
    if (mine === generation.current) setView({ status: 'ready', data });
  }, [fetchTracking]);

  const run = useCallback(
    async (mutate: () => Promise<unknown>): Promise<TrackingMutationOutcome> => {
      if (mutatingRef.current)
        return { status: 'mutationFailed', message: 'Another change is in progress.' };
      mutatingRef.current = true;
      setMutating(true);
      setNotice(null);
      try {
        const outcome = await runTrackingMutation(mutate, reloadOrThrow);
        if (outcome.status === 'refreshFailed') {
          setNotice(
            `Your change was saved, but the view could not be refreshed: ${outcome.message}`,
          );
        }
        return outcome;
      } finally {
        mutatingRef.current = false;
        setMutating(false);
      }
    },
    [reloadOrThrow],
  );

  return { view, notice, mutating, reload: load, run, clearNotice: () => setNotice(null) };
}
