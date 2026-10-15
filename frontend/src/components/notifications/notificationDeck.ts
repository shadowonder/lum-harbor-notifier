export const COMPACT_DECK_THRESHOLD = 3;

export type NotificationCardMode = 'full' | 'primary' | 'compressed' | 'hidden';

export interface NotificationDeckPresentation {
  compact: boolean;
  expanded: boolean;
  additionalCount: number;
  modes: NotificationCardMode[];
}

export function resolveNotificationDeck(
  itemCount: number,
  requestedExpanded: boolean,
): NotificationDeckPresentation {
  const count = Math.max(0, Math.trunc(itemCount));
  const compact = count > COMPACT_DECK_THRESHOLD;
  const expanded = compact && requestedExpanded;

  if (!compact || expanded) {
    return {
      compact,
      expanded,
      additionalCount: compact ? count - 1 : 0,
      modes: Array.from({ length: count }, () => 'full'),
    };
  }

  return {
    compact: true,
    expanded: false,
    additionalCount: count - 1,
    modes: Array.from({ length: count }, (_, index) => {
      if (index === 0) return 'primary';
      if (index <= 2) return 'compressed';
      return 'hidden';
    }),
  };
}

export interface NotificationLifetime {
  startedAtMs: number;
  durationMs: number;
  expiresAtMs: number;
}

export function createNotificationLifetime(
  startedAtMs: number,
  durationMs: number,
): NotificationLifetime {
  return {
    startedAtMs,
    durationMs,
    expiresAtMs: startedAtMs + durationMs,
  };
}

export function notificationLifetimeRemainingMs(
  lifetime: NotificationLifetime,
  nowMs: number,
): number {
  return Math.max(0, lifetime.expiresAtMs - nowMs);
}

export function notificationLifetimeElapsedMs(
  lifetime: NotificationLifetime,
  nowMs: number,
): number {
  return Math.min(lifetime.durationMs, Math.max(0, nowMs - lifetime.startedAtMs));
}
