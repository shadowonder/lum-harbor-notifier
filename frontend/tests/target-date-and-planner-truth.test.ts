import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  formatMonitorDate,
  formatMonitorDue,
  monitorCalendarDate,
} from '../src/tracking/trackingPresentation.ts';
import { targetDateKeyOf } from '../src/components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts';
import { buildOverviewRowView } from '../src/presentation/workItems/workItemOverviewPresentation.ts';
import type { MonitorPoint } from '../src/api/tracking/trackingProductApi.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

// 2026-10-11T02:06Z is Oct 11 in UTC but Oct 10 in America/Chicago and Oct 11 in Asia/Tokyo.
// The Monitor timezone, not the machine/browser timezone, decides the day.
const monitor = (timezone: string): MonitorPoint => ({
  id: 'mp',
  subjectKind: 'WORK_ITEM',
  subjectId: 'wi',
  kind: 'FINAL_TARGET',
  title: 'Final target',
  dueAt: '2026-10-11T02:06:00Z',
  timezone,
  expectationKind: 'WORK_ITEM_LIFECYCLE',
  expectedWorkItemLifecycle: 'COMPLETED',
  expectedStepId: null,
  state: 'ACTIVE',
  satisfiedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
});

const cases: Array<[string, string, string]> = [
  ['UTC', '2026-10-11', 'Oct 11'],
  ['America/Chicago', '2026-10-10', 'Oct 10'],
  ['Asia/Tokyo', '2026-10-11', 'Oct 11'],
];

for (const [timezone, key, short] of cases) {
  test(`every surface agrees on the ${timezone} calendar day (${key}) regardless of machine timezone`, () => {
    const m = monitor(timezone);
    assert.equal(monitorCalendarDate(m.dueAt, timezone), key);
    // Dashboard roadmap
    assert.equal(targetDateKeyOf(m.dueAt, timezone), key);
    // Work Items overview row
    const overview = buildOverviewRowView(
      {
        tracking: {
          trackedItemCount: 0,
          workflowBoundItemCount: 0,
          progressions: [],
          activeFinalTarget: m,
        },
        attention: null,
      },
      new Date('2026-09-20T00:00:00Z'),
    );
    assert.equal(overview.target.status === 'set' && overview.target.dateLabel, short);
    // Workspace header ("Oct 11, 2026") and context bar ("Oct 11, 2026, 2:06 AM (UTC)")
    assert.ok(formatMonitorDate(m.dueAt, timezone).startsWith(short));
    assert.ok(formatMonitorDue(m).startsWith(short));
    assert.ok(formatMonitorDue(m).endsWith(`(${timezone})`));
  });
}

test('the Workspace header no longer formats the target with the browser locale/timezone', () => {
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  assert.equal(/finalTarget\.dueAt\)\.toLocaleDateString/.test(workspace), false);
  assert.match(workspace, /formatMonitorDate\(finalTarget\.dueAt, finalTarget\.timezone\)/);
});

test('Planner DayPanel never reads the WorkItem mock family and shows no fake related WorkItem', () => {
  const panel = source('components/planner/DayPanel.tsx');
  assert.equal(/mockData|findWorkItem|Related Work Item|relatedWorkItem/.test(panel), false);
  assert.equal(/mockData|findWorkItem/.test(source('pages/Planner.tsx')), false);
});

test('real Planner mapping carries no related-WorkItem fact (none exists authoritatively)', () => {
  const api = source('api/plannerApi.ts');
  assert.equal(/relatedWorkItemId/.test(api), false);
});

test('no production page or component imports the mock WorkItem data', () => {
  for (const file of [
    'pages/Planner.tsx',
    'pages/Dashboard.tsx',
    'pages/WorkItems.tsx',
    'components/planner/DayPanel.tsx',
    'components/AppShell.tsx',
  ]) {
    assert.equal(/workItems\/mockData|mockWorkItemApi/.test(source(file)), false, file);
  }
});
