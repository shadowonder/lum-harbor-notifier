/**
 * Production Dashboard adapter for the Active Workflows timeline.
 *
 * A row is a real time span: START = the authoritative Workflow.startedAt, END = the
 * WorkItem's ACTIVE FINAL_TARGET (the backend selects it; React never scans MonitorPoints).
 * A WorkItem without a Workflow start or without an ACTIVE FINAL_TARGET has no time span
 * and is NOT a row (no open-ended bar, no estimated date). WorkItem/Workflow/TrackedItem
 * creation or update times, TrackedItem activity, Workflow current step and History are
 * never dates here, and there is no previous-target inference.
 */
import type { DashboardCurrentWorkItem } from '../../../api/dashboardApi.ts';
import { monitorCalendarDate } from '../../../tracking/trackingPresentation.ts';
import { resolveRoadmapRowTimelineMode, type ActiveWorkflowRoadmapRow } from './roadmapLayout.ts';

export const targetDateKeyOf = monitorCalendarDate;

export function buildAuthoritativeRoadmapRows(
  items: ReadonlyArray<DashboardCurrentWorkItem>,
  trackingAvailable = true,
): ActiveWorkflowRoadmapRow[] {
  if (!trackingAvailable) return [];
  const rows: Array<ActiveWorkflowRoadmapRow & { startedAtMs: number }> = [];
  for (const item of items) {
    const target = item.activeFinalTarget;
    if (!target || !item.workflowStartedAt) continue;
    const startedAtMs = Date.parse(item.workflowStartedAt);
    if (Number.isNaN(startedAtMs)) continue; // invalid start: skip, never substitute another date
    // Both endpoints use the target's own timezone so they share one calendar.
    const targetKey = targetDateKeyOf(target.dueAt, target.timezone);
    const startKey = targetDateKeyOf(item.workflowStartedAt, target.timezone);
    rows.push({
      startedAtMs,
      workItemId: item.id,
      title: item.title,
      description: item.description,
      // A start after its own target is a data anomaly: keep only the real target.
      timeline: resolveRoadmapRowTimelineMode(startKey <= targetKey ? startKey : null, targetKey),
    });
  }
  // Presentation order: earliest Workflow start first. Ties are broken by title then id so the
  // order is deterministic across refreshes. Target date, lifecycle and input order never matter.
  rows.sort(
    (a, b) =>
      a.startedAtMs - b.startedAtMs ||
      a.title.localeCompare(b.title) ||
      (a.workItemId < b.workItemId ? -1 : a.workItemId > b.workItemId ? 1 : 0),
  );
  return rows.map((row) => ({
    workItemId: row.workItemId,
    title: row.title,
    description: row.description,
    timeline: row.timeline,
  }));
}
