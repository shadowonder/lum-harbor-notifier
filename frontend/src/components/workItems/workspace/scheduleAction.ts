import type { TrackingStep, WorkItemTracking } from '../../../api/tracking/trackingProductApi.ts';
import { getLocalTodayKey } from '../../../pages/plannerData.ts';

/** Reminder presets offered by the WorkItem "Schedule Action" dialog. */
export type ScheduleReminderPolicy = 'standard' | 'urgent' | 'near' | '1d' | 'due' | 'none';

/** Maps a reminder preset to the threshold offsets (seconds before due) sent to the backend. */
export function reminderOffsetsForPolicy(policy: ScheduleReminderPolicy): number[] | undefined {
  switch (policy) {
    case 'standard':
      return [259200, 172800, 86400]; // 3d, 2d, 1d
    case 'urgent':
      return [7200, 1800]; // 2h, 30m
    case 'near':
      return [900]; // 15m
    case '1d':
      return [86400];
    case 'due':
      return [0];
    case 'none':
      return undefined;
  }
}

/**
 * Resolves the dialog's Due Date / optional Due Time into the Planner due
 * fields. The backend accepts exactly one of `dueAt` (exact instant) or
 * `dueDate` (date-only), never both: with a time the local date+time becomes
 * `dueAt`; without one the date stays date-only.
 */
export function scheduleActionDue(
  dueDate: string,
  dueTime: string,
): { dueDate?: string; dueAt?: string } {
  if (!dueDate) return {};
  if (!dueTime) return { dueDate };
  return { dueAt: new Date(`${dueDate}T${dueTime}:00`).toISOString() };
}

/**
 * Default Due Date for a newly scheduled action: today in the user's local
 * calendar. Never derived from `toISOString()`, which rolls the date forward
 * in the evening for timezones behind UTC.
 */
export function defaultScheduleActionDueDate(now = new Date()): string {
  return getLocalTodayKey(now);
}

/**
 * Workflow Step provenance for a scheduled action: the Workflow's own current
 * step. TrackedItem progression is an independent fact and never stands in
 * for it; with no Workflow there is no step provenance.
 */
export function scheduleActionWorkflowStep(
  tracking: Pick<WorkItemTracking, 'workflow'> | null,
): TrackingStep | null {
  const workflow = tracking?.workflow;
  if (!workflow?.currentStepId) return null;
  return workflow.steps.find((step) => step.id === workflow.currentStepId) ?? null;
}
