export interface NotificationActivation {
  notificationId: string;
  plannerItemId: string | null;
  workItemId: string | null;
}

export const NOTIFICATION_ACTIVATION_EVENT = 'lumharbor:notification-activate';

function optionalString(value: unknown): string | null {
  return typeof value === 'string' && value.trim() !== '' ? value : null;
}

export function mapNotificationActivation(value: unknown): NotificationActivation | null {
  if (!value || typeof value !== 'object') return null;
  const candidate = value as Record<string, unknown>;
  const notificationId = optionalString(candidate.notificationId);
  if (!notificationId) return null;
  return {
    notificationId,
    plannerItemId: optionalString(candidate.plannerItemId),
    workItemId: optionalString(candidate.workItemId),
  };
}
