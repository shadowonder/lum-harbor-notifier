import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getPlannerView, type PlannerViewRequest } from '../api/plannerApi';
import type { PlannerViewData } from '../pages/plannerData';

interface PlannerViewRequestState {
  requestKey: string;
  refreshGeneration: number;
  data: PlannerViewData | null;
  error: string | null;
}

/** Public state exposed by the Planner page-level read lifecycle. */
export interface UsePlannerViewResult {
  data: PlannerViewData | null;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  retry: () => void;
  refresh: () => Promise<void>;
}

/** Settles a caller awaiting one exact explicit refresh generation. */
interface RefreshWaiter {
  resolve: () => void;
  reject: (error: Error) => void;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Loads the Planner projections for one visible Calendar range and current
 * local day.
 *
 * The semantic request key contains only rangeStart, rangeEnd, today, and
 * timezone. A refresh generation can repeat the same request without changing
 * that key. Data is returned only when it belongs to the current key, so a
 * render for a new range can never paint the previous range's Calendar data.
 *
 * Wails promises are not abortable. Each effect receives a monotonically
 * increasing request ID and an active flag. A response may commit state only
 * while both still identify the latest request, preventing a slower stale
 * response from overwriting a newer Month, Week, or navigation request.
 */
export function usePlannerView(request: PlannerViewRequest): UsePlannerViewResult {
  const { rangeStart, rangeEnd, today, timezone } = request;
  const requestKey = useMemo(
    () => JSON.stringify([rangeStart, rangeEnd, today, timezone]),
    [rangeStart, rangeEnd, today, timezone],
  );
  const latestRequestID = useRef(0);
  const refreshGenerationRef = useRef(0);
  const refreshWaiters = useRef(new Map<number, RefreshWaiter>());
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [state, setState] = useState<PlannerViewRequestState | null>(null);

  useEffect(() => {
    const requestID = ++latestRequestID.current;
    const waiters = refreshWaiters.current;
    let active = true;

    void getPlannerView({ rangeStart, rangeEnd, today, timezone }).then(
      (data) => {
        if (!active || latestRequestID.current !== requestID) return;
        setState({ requestKey, refreshGeneration, data, error: null });
        waiters.get(refreshGeneration)?.resolve();
        waiters.delete(refreshGeneration);
      },
      (error: unknown) => {
        if (!active || latestRequestID.current !== requestID) return;
        setState((current) => ({
          requestKey,
          refreshGeneration,
          data: current?.requestKey === requestKey ? current.data : null,
          error: errorMessage(error),
        }));
        waiters
          .get(refreshGeneration)
          ?.reject(error instanceof Error ? error : new Error(String(error)));
        waiters.delete(refreshGeneration);
      },
    );

    return () => {
      active = false;
      const waiter = waiters.get(refreshGeneration);
      if (waiter) {
        waiter.reject(new Error('Planner refresh was superseded by a newer request.'));
        waiters.delete(refreshGeneration);
      }
    };
  }, [rangeStart, rangeEnd, today, timezone, requestKey, refreshGeneration]);

  /**
   * Starts an explicit reload and settles only when that exact generation
   * succeeds or fails. Mutation owners can therefore distinguish a committed
   * backend write followed by a failed read refresh from a failed mutation.
   */
  const refresh = useCallback(() => {
    const generation = refreshGenerationRef.current + 1;
    refreshGenerationRef.current = generation;

    return new Promise<void>((resolve, reject) => {
      refreshWaiters.current.set(generation, { resolve, reject });
      setRefreshGeneration(generation);
    });
  }, []);

  // Passive page retry controls rely on the hook's error state for feedback.
  // Swallow the returned promise there while mutation callers use refresh()
  // directly when they must distinguish write failure from reload failure.
  const retry = useCallback(() => {
    void refresh().catch(() => undefined);
  }, [refresh]);

  const stateMatchesRequest = state?.requestKey === requestKey;
  const requestSettled = stateMatchesRequest && state.refreshGeneration === refreshGeneration;
  const data = stateMatchesRequest ? state.data : null;
  const requestPending = !requestSettled;

  return {
    data,
    loading: requestPending && data === null,
    refreshing: requestPending && data !== null,
    error: requestSettled ? state.error : null,
    retry,
    refresh,
  };
}
