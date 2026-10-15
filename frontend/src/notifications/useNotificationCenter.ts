import { useCallback, useEffect, useRef, useState } from 'react';
import type {
  CenterNotification,
  ProductNotificationsApi,
} from '../api/notifications/notificationsProductApi.ts';
import { runTrackingMutation, type TrackingMutationOutcome } from '../tracking/trackingMutation.ts';
import { scheduleExpiryReload } from './notificationCenterModel.ts';

export type CenterViewState =
  | { status: 'loading' }
  | { status: 'ready'; items: CenterNotification[]; at: string }
  | { status: 'error'; message: string };

/**
 * Loads the authoritative Notification Center (one collection). Every
 * successful command reloads it (no local truth); a committed command whose
 * reload fails is reported distinctly and never re-sent. One timer schedules an
 * authoritative reload at the earliest snooze expiry so a long-open page
 * refreshes without polling or recomputing disposition.
 */
export function useNotificationCenter(api: ProductNotificationsApi) {
  const [view, setView] = useState<CenterViewState>({ status: 'loading' });
  const [notice, setNotice] = useState<string | null>(null);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [mutating, setMutating] = useState(false);
  const generation = useRef(0);
  const mutatingRef = useRef(false);

  const fetchCenter = useCallback(async () => {
    const center = await api.getCenter();
    return center;
  }, [api]);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const center = await fetchCenter();
      if (mine === generation.current)
        setView({ status: 'ready', items: center.items, at: center.at });
    } catch (error) {
      if (mine === generation.current)
        setView({
          status: 'error',
          message: error instanceof Error ? error.message : String(error),
        });
    }
  }, [fetchCenter]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial authoritative load
    void load();
    return () => {
      generation.current += 1;
    };
  }, [load]);

  useEffect(() => {
    if (view.status !== 'ready') return undefined;
    return scheduleExpiryReload(view.items, Date.now(), () => void load(), {
      set: (callback, ms) => setTimeout(callback, ms),
      clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
    });
  }, [view, load]);

  const reloadOrThrow = useCallback(async () => {
    const mine = ++generation.current;
    const center = await fetchCenter();
    if (mine === generation.current)
      setView({ status: 'ready', items: center.items, at: center.at });
  }, [fetchCenter]);

  const run = useCallback(
    async (mutate: () => Promise<unknown>): Promise<TrackingMutationOutcome> => {
      if (mutatingRef.current)
        return { status: 'mutationFailed', message: 'Another change is in progress.' };
      mutatingRef.current = true;
      setMutating(true);
      setNotice(null);
      setCommandError(null);
      try {
        const outcome = await runTrackingMutation(mutate, reloadOrThrow);
        if (outcome.status === 'refreshFailed') {
          setNotice(
            `Your change was saved, but the list could not be refreshed: ${outcome.message}`,
          );
        } else if (outcome.status === 'mutationFailed') {
          setCommandError(outcome.message);
        }
        return outcome;
      } finally {
        mutatingRef.current = false;
        setMutating(false);
      }
    },
    [reloadOrThrow],
  );

  return { view, notice, commandError, mutating, reload: load, run };
}
