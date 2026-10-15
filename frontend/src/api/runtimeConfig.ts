import {
  GetRuntimeConfig,
  SaveGeneralSettings,
  SaveInAppNotificationSettings,
} from '../../wailsjs/go/app/App';
import {
  mapGeneralSettings,
  mapInAppNotificationSettings,
  mapRuntimeConfigDTO,
  type AutoDismissSeconds,
  type CloseWindowBehavior,
  type GeneralSettings,
  type InAppNotificationSettings,
  type NotificationPlacement,
  type Platform,
  type RuntimeConfig,
} from './runtimeConfigMapping.ts';

export type {
  AutoDismissSeconds,
  CloseWindowBehavior,
  GeneralSettings,
  InAppNotificationSettings,
  NotificationPlacement,
  Platform,
  RuntimeConfig,
};

let runtimeConfigRequest: Promise<RuntimeConfig> | null = null;

/**
 * Loads and validates the process startup configuration through the generated
 * Wails boundary. The in-flight/result promise is shared for the application
 * lifetime so remounts do not create filesystem-like polling behavior. A
 * failed request clears the cache, allowing an explicit initialization retry.
 */
export function getRuntimeConfig(): Promise<RuntimeConfig> {
  if (!runtimeConfigRequest) {
    runtimeConfigRequest = GetRuntimeConfig()
      .then(mapRuntimeConfigDTO)
      .catch((error: unknown) => {
        runtimeConfigRequest = null;
        throw error instanceof Error ? error : new Error(String(error));
      });
  }
  return runtimeConfigRequest;
}

export async function saveGeneralSettings(
  value: GeneralSettings,
): Promise<GeneralSettings> {
  const saved = await SaveGeneralSettings({
    closeWindowBehavior: value.closeWindowBehavior,
  });
  return mapGeneralSettings(saved);
}

export async function saveInAppNotificationSettings(
  value: InAppNotificationSettings,
): Promise<InAppNotificationSettings> {
  const saved = await SaveInAppNotificationSettings({
    autoDismiss: value.autoDismiss,
    autoDismissSeconds: value.autoDismissSeconds,
    placement: value.placement,
  });
  return mapInAppNotificationSettings(saved);
}
