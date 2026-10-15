import type { app } from '../../wailsjs/go/models.ts';

/**
 * The application's own authoritative platform fact (backed by Go
 * `runtime.GOOS` via `internal/platform`) — never inferred by the frontend
 * from browser navigator/user-agent heuristics, which are unreliable and
 * unavailable in this Wails renderer's intended usage.
 */
export type Platform = 'MACOS' | 'WINDOWS' | 'OTHER';

export type NotificationPlacement =
  'TOP_LEFT' | 'TOP_CENTER' | 'TOP_RIGHT' | 'BOTTOM_LEFT' | 'BOTTOM_CENTER' | 'BOTTOM_RIGHT';

export type AutoDismissSeconds = 10 | 30 | 60 | 300 | 600 | 900 | 1800;

export type CloseWindowBehavior = 'background' | 'quit';

export interface GeneralSettings {
  closeWindowBehavior: CloseWindowBehavior;
}

export interface InAppNotificationSettings {
  autoDismiss: boolean;
  autoDismissSeconds: AutoDismissSeconds;
  placement: NotificationPlacement;
}

const KNOWN_PLATFORMS: readonly Platform[] = ['MACOS', 'WINDOWS', 'OTHER'];
const AUTO_DISMISS_SECONDS = [10, 30, 60, 300, 600, 900, 1800] as const;
const NOTIFICATION_PLACEMENTS: readonly NotificationPlacement[] = [
  'TOP_LEFT',
  'TOP_CENTER',
  'TOP_RIGHT',
  'BOTTOM_LEFT',
  'BOTTOM_CENTER',
  'BOTTOM_RIGHT',
];
const CLOSE_WINDOW_BEHAVIORS: readonly CloseWindowBehavior[] = ['background', 'quit'];

/** Frontend-only runtime settings exposed by the narrow Wails DTO. */
export interface RuntimeConfig {
  general: GeneralSettings;
  dashboard: {
    todoDisplayLimit: number;
  };
  notifications: {
    inApp: InAppNotificationSettings;
  };
  platform: Platform;
}

export function mapGeneralSettings(
  dto: app.RuntimeGeneralConfigDTO | undefined,
): GeneralSettings {
  const closeWindowBehavior = dto?.closeWindowBehavior ?? 'background';
  if (!CLOSE_WINDOW_BEHAVIORS.includes(closeWindowBehavior as CloseWindowBehavior)) {
    throw new Error(`Invalid general.closeWindowBehavior in runtime configuration: ${String(closeWindowBehavior)}`);
  }
  return {
    closeWindowBehavior: closeWindowBehavior as CloseWindowBehavior,
  };
}

export function mapInAppNotificationSettings(
  dto: app.InAppNotificationSettingsDTO,
): InAppNotificationSettings {
  const autoDismissSeconds = dto?.autoDismissSeconds;
  if (!AUTO_DISMISS_SECONDS.includes(autoDismissSeconds as AutoDismissSeconds)) {
    throw new Error('Invalid notifications.inApp.autoDismissSeconds in runtime configuration.');
  }
  const placement = dto?.placement;
  if (!NOTIFICATION_PLACEMENTS.includes(placement as NotificationPlacement)) {
    throw new Error('Invalid notifications.inApp.placement in runtime configuration.');
  }
  if (typeof dto?.autoDismiss !== 'boolean') {
    throw new Error('Invalid notifications.inApp.autoDismiss in runtime configuration.');
  }
  return {
    autoDismiss: dto.autoDismiss,
    autoDismissSeconds: autoDismissSeconds as AutoDismissSeconds,
    placement: placement as NotificationPlacement,
  };
}

/**
 * Pure validation/mapping boundary, kept in its own file with only a
 * type-only Wails import (erased at build time) so it stays directly
 * unit-testable under this project's plain Node test runner without
 * resolving the real generated `App.js` value import that `runtimeConfig.ts`
 * needs for its cached live call.
 */
export function mapRuntimeConfigDTO(dto: app.RuntimeConfigDTO): RuntimeConfig {
  const general = mapGeneralSettings(dto?.general);
  const todoDisplayLimit = dto?.dashboard?.todoDisplayLimit;
  if (!Number.isInteger(todoDisplayLimit) || todoDisplayLimit < 1 || todoDisplayLimit > 20) {
    throw new Error('Invalid dashboard.todoDisplayLimit in runtime configuration.');
  }
  const platform = dto?.platform;
  if (!KNOWN_PLATFORMS.includes(platform as Platform)) {
    throw new Error(`Invalid platform in runtime configuration: ${String(platform)}`);
  }
  return {
    general,
    dashboard: { todoDisplayLimit },
    notifications: { inApp: mapInAppNotificationSettings(dto.notifications?.inApp) },
    platform: platform as Platform,
  };
}
