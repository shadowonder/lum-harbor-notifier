import type { app } from '../../../wailsjs/go/models';
import type {
  AttentionState,
  CenterNotification,
  NotificationCenter,
  NotificationDisposition,
  NotificationSeverity,
} from './notificationsProductApi.ts';

const dispositions: readonly NotificationDisposition[] = ['NEEDS_ACTION', 'SNOOZED', 'RESOLVED'];
const severities: readonly NotificationSeverity[] = ['INFO', 'WARNING', 'CRITICAL'];
const attentionStates: readonly AttentionState[] = ['OPEN', 'RESOLVED'];

function oneOf<T extends string>(values: readonly T[], value: string, field: string): T {
  if (!values.includes(value as T)) throw new Error(`Unsupported ${field}: ${value}`);
  return value as T;
}

function instant(value: string | null | undefined, field: string): string {
  if (!value || Number.isNaN(Date.parse(value))) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

function optionalInstant(value: string | null | undefined, field: string): string | null {
  return value == null ? null : instant(value, field);
}

/**
 * Maps one Notification Center DTO. The product center only ever contains
 * delivered notifications: anything not SENT, or without a sent time, is a
 * contract violation and is rejected instead of being displayed.
 */
export function mapCenterNotification(dto: app.NotificationCenterItemDTO): CenterNotification {
  if (dto.deliveryState !== 'SENT') {
    throw new Error(`Notification center received a ${dto.deliveryState} notification`);
  }
  return {
    notificationId: dto.notificationId,
    attentionIssueId: dto.attentionIssueId,
    workItemId: dto.workItemId ?? null,
    workItemTitle: dto.workItemTitle ?? null,
    subjectTrackedItemId: dto.subjectTrackedItemId ?? null,
    plannerItemId: dto.plannerItemId ?? null,
    title: dto.title,
    message: dto.message,
    severity: oneOf(severities, dto.severity, 'notification severity'),
    recommendedAction: dto.recommendedAction ?? null,
    sentAt: instant(dto.sentAt, 'notification sentAt'),
    readAt: optionalInstant(dto.readAt, 'notification readAt'),
    attentionState: oneOf(attentionStates, dto.attentionState, 'attention state'),
    snoozedUntil: optionalInstant(dto.snoozedUntil, 'snoozedUntil'),
    resolvedAt: optionalInstant(dto.resolvedAt, 'resolvedAt'),
    disposition: oneOf(dispositions, dto.disposition, 'disposition'),
    reason: {
      monitorTitle: dto.reason?.monitorTitle ?? '',
      monitorKind: dto.reason?.monitorKind ?? '',
      dueAt: dto.reason?.dueAt ? instant(dto.reason.dueAt, 'reason dueAt') : '',
      timezone: dto.reason?.timezone ?? '',
      subjectKind: dto.reason?.subjectKind ?? '',
      subjectTitle: dto.reason?.subjectTitle ?? null,
      expectedKind: dto.reason.expectedKind,
      expectedWorkItemLifecycle: dto.reason.expectedWorkItemLifecycle ?? null,
      expectedStepTitle: dto.reason.expectedStepTitle ?? null,
      actualWorkItemLifecycle: dto.reason.actualWorkItemLifecycle ?? null,
      actualStepState: dto.reason.actualStepState ?? null,
    },
  };
}

export function mapNotificationCenter(dto: app.NotificationCenterDTO): NotificationCenter {
  return { at: instant(dto.at, 'center at'), items: (dto.items ?? []).map(mapCenterNotification) };
}
