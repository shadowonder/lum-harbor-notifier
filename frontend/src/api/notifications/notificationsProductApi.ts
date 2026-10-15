/**
 * Frontend product contract for the M11 Notification Center. Semantic and
 * transport-free: generated Wails types stop at notificationDtoAdapter.ts.
 * Attention disposition is derived by the BACKEND and used as delivered.
 */
export type NotificationDisposition = 'NEEDS_ACTION' | 'SNOOZED' | 'RESOLVED';
export type NotificationSeverity = 'INFO' | 'WARNING' | 'CRITICAL';
export type AttentionState = 'OPEN' | 'RESOLVED';

export interface NotificationReason {
  monitorTitle: string;
  monitorKind: string;
  dueAt: string;
  timezone: string;
  subjectKind: string;
  subjectTitle: string | null;
  expectedKind: string;
  expectedWorkItemLifecycle: string | null;
  expectedStepTitle: string | null;
  actualWorkItemLifecycle: string | null;
  actualStepState: string | null;
}

/** One delivered (SENT) notification with its linked Attention truth. */
export interface CenterNotification {
  notificationId: string;
  attentionIssueId: string;
  workItemId: string | null;
  workItemTitle: string | null;
  subjectTrackedItemId: string | null;
  plannerItemId: string | null;
  title: string;
  message: string;
  severity: NotificationSeverity;
  recommendedAction: string | null;
  sentAt: string;
  readAt: string | null;
  attentionState: AttentionState;
  snoozedUntil: string | null;
  resolvedAt: string | null;
  disposition: NotificationDisposition;
  reason: NotificationReason;
}

export interface NotificationCenter {
  /** Backend clock instant used to derive dispositions. */
  at: string;
  items: CenterNotification[];
}

export interface ProductNotificationsApi {
  getCenter(): Promise<NotificationCenter>;
  markRead(notificationId: string): Promise<void>;
  snooze(attentionIssueId: string, until: Date): Promise<void>;
  acknowledgePresented?(notificationId: string): Promise<void>;
}
