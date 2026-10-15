import {
  AcknowledgeNotificationPresented,
  GetNotificationCenter,
  MarkNotificationRead,
  SnoozeAttentionIssue,
} from '../../../wailsjs/go/app/App';
import { mapNotificationCenter } from './notificationDtoAdapter.ts';
import type { ProductNotificationsApi } from './notificationsProductApi.ts';

export const productNotificationsApi: ProductNotificationsApi = {
  async getCenter() {
    return mapNotificationCenter(await GetNotificationCenter());
  },
  async markRead(notificationId) {
    await MarkNotificationRead({ notificationId });
  },
  async snooze(attentionIssueId, until) {
    await SnoozeAttentionIssue({ attentionIssueId, snoozedUntil: until.toISOString() });
  },
  async acknowledgePresented(notificationId) {
    await AcknowledgeNotificationPresented(notificationId);
  },
};
