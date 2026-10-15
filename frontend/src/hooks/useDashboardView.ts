import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  DashboardViewApi,
  DashboardViewData,
  DashboardViewRequest,
} from '../api/dashboardApi';
import { loadDashboardView } from '../api/dashboardApi';
import { dashboardViewApi } from '../api/wailsDashboardApi';

interface DashboardViewRequestState {
  requestKey: string;
  refreshGeneration: number;
  data: DashboardViewData | null;
  error: string | null;
}

interface RefreshWaiter {
  resolve: () => void;
  reject: (error: Error) => void;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

/** Loads the single authoritative Dashboard projection with explicit refresh semantics. */
export function useDashboardView(
  request: DashboardViewRequest,
  api: DashboardViewApi = dashboardViewApi,
) {
  const { rangeStart, rangeEnd, today, timezone } = request;
  const requestKey = useMemo(
    () => JSON.stringify([rangeStart, rangeEnd, today, timezone]),
    [rangeStart, rangeEnd, today, timezone],
  );
  const latestRequestID = useRef(0);
  const refreshGenerationRef = useRef(0);
  const refreshWaiters = useRef(new Map<number, RefreshWaiter>());
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [state, setState] = useState<DashboardViewRequestState | null>(null);

  useEffect(() => {
    const requestID = ++latestRequestID.current;
    const waiters = refreshWaiters.current;
    let active = true;
    void loadDashboardView(api, { rangeStart, rangeEnd, today, timezone }).then(
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
        waiter.reject(new Error('Dashboard refresh was superseded by a newer request.'));
        waiters.delete(refreshGeneration);
      }
    };
  }, [api, rangeStart, rangeEnd, today, timezone, requestKey, refreshGeneration]);

  const refresh = useCallback(() => {
    const generation = refreshGenerationRef.current + 1;
    refreshGenerationRef.current = generation;
    return new Promise<void>((resolve, reject) => {
      refreshWaiters.current.set(generation, { resolve, reject });
      setRefreshGeneration(generation);
    });
  }, []);
  const retry = useCallback(() => void refresh().catch(() => undefined), [refresh]);

  const stateMatchesRequest = state?.requestKey === requestKey;
  const settled = stateMatchesRequest && state.refreshGeneration === refreshGeneration;
  const data = stateMatchesRequest ? state.data : null;
  return {
    data,
    loading: !settled && data === null,
    refreshing: !settled && data !== null,
    error: settled ? state.error : null,
    retry,
    refresh,
  };
}
