import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  buildRoadmapHoverSummary,
  computeAdaptivePixelsPerDay,
  computeDayOffset,
  computeRoadmapWindow,
  computeScheduleRevisionDeltaDays,
  computeTimelineWidth,
  describeTargetDate,
  formatScheduleRevisionLabel,
  resolveRoadmapRowTimelineMode,
  timelineDateKeys,
  ROADMAP_MAX_PIXELS_PER_DAY,
  ROADMAP_MIN_PIXELS_PER_DAY,
  ROADMAP_PANEL_MAX_HEIGHT,
  type ActiveWorkflowRoadmapRow,
} from '../src/components/dashboard/activeWorkflowsRoadmap/roadmapLayout.ts';
import {
  buildPrototypeRoadmapRows,
  isRealPrototypeWorkItemId,
} from '../src/components/dashboard/activeWorkflowsRoadmap/roadmapPrototypeAdapter.ts';
import { buildAuthoritativeRoadmapRows } from '../src/components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts';
import type { DashboardCurrentWorkItem } from '../src/api/dashboardApi.ts';
import { PROTOTYPE_WORKFLOW_MODELS } from '../src/components/workItems/fixtures/workItemPrototypeFixtures.ts';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const TODAY = '2026-09-19';

// 1. Prototype-date sourcing -------------------------------------------------

test('roadmap rows use prototype/presentation dates only, never createdAt/updatedAt', () => {
  const rows = buildPrototypeRoadmapRows();
  const checkout = rows.find((row) => row.workItemId === 'wi-checkout-platform')!;
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  assert.equal(checkout.timeline.kind, 'span');
  assert.equal(
    checkout.timeline.kind === 'span' ? checkout.timeline.currentTargetDateKey : null,
    model.targetDate,
  );
});

test('roadmapPrototypeAdapter.ts never reads createdAt/updatedAt off a WorkItem to derive a roadmap date', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapPrototypeAdapter.ts',
  );
  assert.ok(
    !/\.createdAt|\.updatedAt/.test(source),
    'adapter must not derive dates from createdAt/updatedAt',
  );
});

// 2. Real active workflows use workflow-span mode, not milestone-only -------

test('every real active prototype WorkItem renders as a workflow span (start -> current target), not a milestone point', () => {
  const rows = buildPrototypeRoadmapRows();
  for (const id of ['wi-checkout-platform', 'wi-dist-systems', 'wi-lob-onboarding']) {
    const row = rows.find((r) => r.workItemId === id)!;
    assert.equal(
      row.timeline.kind,
      'span',
      `${id} should be a workflow span, not a milestone point`,
    );
  }
});

// 3-5. Timeline mode resolution ----------------------------------------------

test('a target-only date (no start) resolves to milestone mode with no fabricated span', () => {
  const timeline = resolveRoadmapRowTimelineMode(null, '2026-10-01');
  assert.deepEqual(timeline, { kind: 'milestone', targetDateKey: '2026-10-01' });
});

test('an explicit start and current target with no revision resolves to span mode with no previousTargetDateKey', () => {
  const timeline = resolveRoadmapRowTimelineMode('2026-09-16', '2026-10-02');
  assert.deepEqual(timeline, {
    kind: 'span',
    startDateKey: '2026-09-16',
    currentTargetDateKey: '2026-10-02',
  });
});

test('no displayable date resolves to unscheduled mode', () => {
  assert.deepEqual(resolveRoadmapRowTimelineMode(null, null), { kind: 'unscheduled' });
  assert.deepEqual(resolveRoadmapRowTimelineMode('2026-09-16', null), { kind: 'unscheduled' });
});

// 6. Target revision presentation --------------------------------------------

test('a previous target different from the current target produces a revised-target span', () => {
  const timeline = resolveRoadmapRowTimelineMode('2026-09-16', '2026-10-15', '2026-10-02');
  assert.deepEqual(timeline, {
    kind: 'span',
    startDateKey: '2026-09-16',
    currentTargetDateKey: '2026-10-15',
    previousTargetDateKey: '2026-10-02',
  });
});

test('a previous target equal to the current target is not a revision and is dropped', () => {
  const timeline = resolveRoadmapRowTimelineMode('2026-09-16', '2026-10-02', '2026-10-02');
  assert.deepEqual(timeline, {
    kind: 'span',
    startDateKey: '2026-09-16',
    currentTargetDateKey: '2026-10-02',
  });
});

test('the prototype fixture set demonstrates a revision on a real, navigable WorkItem', () => {
  const rows = buildPrototypeRoadmapRows();
  const revised = rows.find(
    (row) => row.timeline.kind === 'span' && row.timeline.previousTargetDateKey,
  )!;
  assert.ok(revised, 'expected at least one real row with a schedule revision');
  assert.ok(
    isRealPrototypeWorkItemId(revised.workItemId),
    'the revision example should be a real WorkItem',
  );
});

// 7. Schedule revision date arithmetic ----------------------------------------

test('computeScheduleRevisionDeltaDays and formatScheduleRevisionLabel compute "moved later" and "moved earlier" correctly', () => {
  assert.equal(computeScheduleRevisionDeltaDays('2026-09-23', '2026-10-02'), 9);
  assert.equal(formatScheduleRevisionLabel(9), 'Moved +9 days');
  assert.equal(computeScheduleRevisionDeltaDays('2026-10-02', '2026-09-23'), -9);
  assert.equal(formatScheduleRevisionLabel(-9), 'Moved earlier by 9 days');
  assert.equal(formatScheduleRevisionLabel(0), 'Target unchanged');
  assert.equal(formatScheduleRevisionLabel(1), 'Moved +1 day');
});

// 8. Today always within the visible window ----------------------------------

test('Today is always within the computed visible timeline window', () => {
  const emptyWindow = computeRoadmapWindow(TODAY, []);
  assert.ok(emptyWindow.startDate <= TODAY && TODAY <= emptyWindow.endDate);

  const rows = buildPrototypeRoadmapRows();
  const allDateKeys = rows.flatMap((row) => timelineDateKeys(row.timeline));
  const populatedWindow = computeRoadmapWindow(TODAY, allDateKeys);
  assert.ok(populatedWindow.startDate <= TODAY && TODAY <= populatedWindow.endDate);

  const outlierWindow = computeRoadmapWindow(TODAY, ['2027-06-01']);
  assert.ok(outlierWindow.startDate <= TODAY && TODAY <= outlierWindow.endDate);
});

// 9. Date-derived arithmetic ---------------------------------------------------

test('describeTargetDate computes overdue/due-today/remaining-day states from real arithmetic only', () => {
  assert.deepEqual(describeTargetDate(null, TODAY), { state: 'unscheduled' });
  assert.deepEqual(describeTargetDate(TODAY, TODAY), { state: 'due-today' });
  assert.deepEqual(describeTargetDate('2026-09-17', TODAY), { state: 'overdue', overdueDays: 2 });
  assert.deepEqual(describeTargetDate('2026-09-21', TODAY), {
    state: 'upcoming',
    remainingDays: 2,
  });
});

test('date-derived wording never includes fabricated health/risk vocabulary', () => {
  const source = sourceOf('../src/components/dashboard/activeWorkflowsRoadmap/roadmapLayout.ts');
  for (const forbidden of ['BLOCKED', 'AT RISK', 'CRITICAL']) {
    assert.ok(!source.includes(forbidden), `roadmapLayout.ts must not contain "${forbidden}"`);
  }
});

// 10. Deterministic position calculation ---------------------------------------

test('timeline day-offset calculation is deterministic for the same inputs', () => {
  const window_ = computeRoadmapWindow(TODAY, ['2026-09-21', '2026-10-15']);
  const first = computeDayOffset('2026-09-21', window_.startDate);
  const second = computeDayOffset('2026-09-21', window_.startDate);
  assert.equal(first, second);
});

// 11. Adaptive timeline scale --------------------------------------------------

test('a normal date span in a wide viewport fills the available width instead of using a tiny fixed scale', () => {
  const wideViewportPxPerDay = computeAdaptivePixelsPerDay(90, 1400);
  assert.ok(
    wideViewportPxPerDay > ROADMAP_MIN_PIXELS_PER_DAY,
    'a wide viewport should scale up beyond the minimum for a normal span',
  );
});

test('a very wide date span never drops pixels-per-day below the configured minimum', () => {
  const pxPerDay = computeAdaptivePixelsPerDay(3000, 800);
  assert.equal(pxPerDay, ROADMAP_MIN_PIXELS_PER_DAY);
});

test('a very short date span never exceeds the configured maximum pixels-per-day', () => {
  const pxPerDay = computeAdaptivePixelsPerDay(3, 5000);
  assert.equal(pxPerDay, ROADMAP_MAX_PIXELS_PER_DAY);
});

test('timeline width scales with the supplied pixels-per-day, not a single hardcoded constant', () => {
  const window_ = computeRoadmapWindow(TODAY, ['2026-10-15']);
  const narrow = computeTimelineWidth(window_, ROADMAP_MIN_PIXELS_PER_DAY);
  const wide = computeTimelineWidth(window_, ROADMAP_MAX_PIXELS_PER_DAY);
  assert.ok(wide > narrow);
});

test('the renderer measures its own viewport via ResizeObserver rather than a fixed pixels-per-day constant, and never uses transform: scale', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.ok(
    source.includes('ResizeObserver'),
    'renderer should measure available width adaptively',
  );
  assert.ok(source.includes('computeAdaptivePixelsPerDay'));
  assert.ok(!/transform:\s*['"]scale/.test(source), 'renderer must never scale-shrink the roadmap');
  assert.ok(!/fontSize/.test(source), 'adaptive scale must never touch font size');
});

// 12. Bounded viewport policy ---------------------------------------------------

test('the roadmap viewport height is a fixed bounded policy, not a function of row count', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.ok(source.includes('ROADMAP_PANEL_MAX_HEIGHT'), 'renderer must bound its own height');
  assert.ok(!/maxHeight:\s*rows\.length/.test(source), 'max height must not scale with row count');
  assert.equal(typeof ROADMAP_PANEL_MAX_HEIGHT, 'number');
});

// 13. Hover summary view model --------------------------------------------------

test('hover summary for a plain span includes title, start, and current target', () => {
  const row: ActiveWorkflowRoadmapRow = {
    workItemId: 'wi-checkout-platform',
    title: 'Checkout & Payments Platform V2',
    description: 'Multi-region payment rollout',
    timeline: { kind: 'span', startDateKey: '2026-09-03', currentTargetDateKey: '2026-09-21' },
  };
  const summary = buildRoadmapHoverSummary(row, TODAY);
  assert.equal(summary.title, row.title);
  assert.equal(summary.description, row.description);
  assert.equal(summary.startLabel, 'Sep 3');
  assert.equal(summary.currentTargetLabel, 'Sep 21');
  assert.equal(summary.previousTargetLabel, null);
  assert.equal(summary.scheduleRevisionLabel, null);
  assert.equal(summary.dateStateLabel, '2 days remaining');
});

test('hover summary for a revised span includes previous target and the schedule delta', () => {
  const row: ActiveWorkflowRoadmapRow = {
    workItemId: 'wi-lob-onboarding',
    title: 'LOB Client Automated Onboarding Portal',
    description: null,
    timeline: {
      kind: 'span',
      startDateKey: '2026-09-16',
      currentTargetDateKey: '2026-10-15',
      previousTargetDateKey: '2026-10-02',
    },
  };
  const summary = buildRoadmapHoverSummary(row, TODAY);
  assert.equal(summary.previousTargetLabel, 'Oct 2');
  assert.equal(summary.currentTargetLabel, 'Oct 15');
  assert.equal(summary.scheduleRevisionLabel, 'Moved +13 days');
});

test('hover summary for an unscheduled row carries no fabricated dates', () => {
  const row: ActiveWorkflowRoadmapRow = {
    workItemId: 'wi-roadmap-demo-unscheduled-planning',
    title: 'Q4 Platform Planning Spike (prototype only)',
    timeline: { kind: 'unscheduled' },
  };
  const summary = buildRoadmapHoverSummary(row, TODAY);
  assert.equal(summary.startLabel, null);
  assert.equal(summary.currentTargetLabel, null);
  assert.equal(summary.dateStateLabel, null);
});

test('the renderer wires up a hover summary using an existing MUI Tooltip, not a bespoke tooltip framework', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.ok(/from ['"]@mui\/material['"]/.test(source) && source.includes('Tooltip'));
  assert.ok(source.includes('buildRoadmapHoverSummary'));
});

// 14. Navigation ----------------------------------------------------------------

test('clicking a row calls the injected onOpenWorkItem prop rather than a second navigation architecture', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.ok(source.includes('onOpenWorkItem(row.workItemId, row.title)'));
  assert.ok(
    !/window\.location|history\.push|navigate\(/.test(source),
    'must not invent its own navigation',
  );
});

test('the identity area and the timeline bar area are both wired to the same activate handler', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  const activateUsages = source.match(/onClick=\{activate\}/g) ?? [];
  assert.ok(
    activateUsages.length >= 2,
    'expected both the identity ButtonBase and the timeline cell to use the same activate handler',
  );
});

test('the renderer treats every supplied production row as a real navigation target', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.ok(!source.includes('isRealPrototypeWorkItemId'));
  assert.ok(!source.includes('disabled={!interactive}'));
});

test('only real prototype WorkItem ids are treated as click-through targets', () => {
  assert.equal(isRealPrototypeWorkItemId('wi-checkout-platform'), true);
  assert.equal(isRealPrototypeWorkItemId('wi-roadmap-demo-unscheduled-planning'), false);
});

// 15. Import boundary -----------------------------------------------------------

test('the roadmap module never imports the Dependency Explorer, Workflow Stage renderer, or backend-generated types', () => {
  const forbiddenImportFragments = [
    'dependencyExperiment',
    'workflow/WorkflowCanvas',
    'workflow/workflowLayout',
    'dependencyRouting',
    'wailsjs/go/models',
  ];
  for (const file of [
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapLayout.ts',
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapPrototypeAdapter.ts',
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts',
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  ]) {
    const source = sourceOf(file);
    for (const fragment of forbiddenImportFragments) {
      assert.ok(!source.includes(fragment), `${file} must not import ${fragment}`);
    }
  }
});

// 16. Production Dashboard adapter untouched by fake roadmap truth -------------

test('the production dashboardApi.ts is not wired into the prototype roadmap and stays free of roadmap concerns', () => {
  const dashboardApiSource = sourceOf('../src/api/dashboardApi.ts');
  assert.ok(
    !/roadmap/i.test(dashboardApiSource),
    'dashboardApi.ts must not know about the Roadmap panel',
  );

  const adapterSource = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapPrototypeAdapter.ts',
  );
  assert.ok(
    !/from\s+['"][^'"]*dashboardApi/.test(adapterSource),
    'the prototype adapter must not import the production Dashboard API',
  );
});

test('the production Dashboard uses only the authoritative adapter, never prototype fixtures', () => {
  const source = sourceOf('../src/pages/Dashboard.tsx');
  assert.ok(source.includes('buildAuthoritativeRoadmapRows'));
  assert.ok(source.includes('workItems?.allCurrentItems ?? []'));
  assert.ok(!source.includes('roadmapPrototypeAdapter'));
  assert.ok(!source.includes('buildPrototypeRoadmapRows'));
  assert.ok(!source.includes('PROTOTYPE_WORKFLOW_MODELS'));
});

test('the authoritative roadmap adapter derives dates only from startedAt and the ACTIVE FINAL_TARGET', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts',
  );
  const code = source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/.*$/gm, '');
  assert.ok(!/createdAt|updatedAt|progress|currentStep|history|checkpoint|previous/i.test(code));
  assert.match(code, /item\.workflowStartedAt/);
  assert.match(code, /item\.activeFinalTarget/);
});

test('the prototype roadmap fixture set keeps real, navigable workflows as the visual-acceptance majority', () => {
  const rows: ActiveWorkflowRoadmapRow[] = buildPrototypeRoadmapRows();
  assert.ok(rows.length >= 4 && rows.length <= 8);

  const realRowCount = rows.filter((row) => isRealPrototypeWorkItemId(row.workItemId)).length;
  const syntheticRowCount = rows.length - realRowCount;
  assert.ok(
    realRowCount >= syntheticRowCount,
    'real WorkItems should not be outnumbered by synthetic demo rows',
  );

  const hasUnscheduled = rows.some((row) => row.timeline.kind === 'unscheduled');
  assert.ok(hasUnscheduled, 'expected at least one honest unscheduled row');
});

const monitor = (dueAt: string, timezone = 'UTC') => ({
  id: 'mp-1',
  subjectKind: 'WORK_ITEM' as const,
  subjectId: 'wi-1',
  kind: 'FINAL_TARGET' as const,
  title: 'Launch',
  dueAt,
  timezone,
  expectationKind: 'WORK_ITEM_LIFECYCLE' as const,
  expectedWorkItemLifecycle: 'COMPLETED' as const,
  expectedStepId: null,
  state: 'ACTIVE' as const,
  satisfiedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
});

const spanItem = (
  id: string,
  startedAt: string | null,
  target: ReturnType<typeof monitor> | null,
  over: Partial<DashboardCurrentWorkItem> = {},
): DashboardCurrentWorkItem => ({
  id,
  title: id,
  description: null,
  lifecycleState: 'ACTIVE',
  updatedAt: '2026-09-19T12:00:00Z',
  activeFinalTarget: target,
  workflowStartedAt: startedAt,
  ...over,
});

test('a row is a span from Workflow.startedAt to the ACTIVE FINAL_TARGET', () => {
  const [row] = buildAuthoritativeRoadmapRows([
    spanItem('a', '2026-08-20T09:00:00Z', monitor('2026-10-15T12:00:00Z')),
  ]);
  assert.deepEqual(row.timeline, {
    kind: 'span',
    startDateKey: '2026-08-20',
    currentTargetDateKey: '2026-10-15',
  });
  assert.equal('previousTargetDateKey' in row.timeline, false);
});

test('WorkItem timestamps never become the start: only workflowStartedAt does', () => {
  const [row] = buildAuthoritativeRoadmapRows([
    spanItem('a', '2026-08-01T09:00:00Z', monitor('2026-10-15T12:00:00Z'), {
      updatedAt: '2026-09-19T12:00:00Z',
    }),
  ]);
  assert.equal(row.timeline.kind === 'span' && row.timeline.startDateKey, '2026-08-01');
});

test('no ACTIVE FINAL_TARGET means no row, and no fallback end date is invented', () => {
  const rows = buildAuthoritativeRoadmapRows([
    spanItem('no-target', '2026-08-20T09:00:00Z', null),
    spanItem('has-target', '2026-08-20T09:00:00Z', monitor('2026-10-15T12:00:00Z')),
  ]);
  assert.deepEqual(
    rows.map((r) => r.workItemId),
    ['has-target'],
  );
});

test('no Workflow start means no row (the frontend never derives a start)', () => {
  assert.deepEqual(
    buildAuthoritativeRoadmapRows([spanItem('no-wf', null, monitor('2026-10-15T12:00:00Z'))]),
    [],
  );
});

test('unavailable Tracking yields no rows instead of fake unscheduled rows', () => {
  assert.deepEqual(
    buildAuthoritativeRoadmapRows(
      [spanItem('a', '2026-08-20T09:00:00Z', monitor('2026-10-15T12:00:00Z'))],
      false,
    ),
    [],
  );
});

test('start and target use one calendar: the target timezone decides both day keys', () => {
  const [row] = buildAuthoritativeRoadmapRows([
    spanItem('a', '2026-08-21T03:00:00Z', monitor('2026-10-16T03:00:00Z', 'America/Los_Angeles')),
  ]);
  assert.deepEqual(row.timeline, {
    kind: 'span',
    startDateKey: '2026-08-20',
    currentTargetDateKey: '2026-10-15',
  });
});

test('an overdue target keeps its real position left of today; a future one right of it', () => {
  const rows = buildAuthoritativeRoadmapRows([
    spanItem('overdue', '2026-08-01T09:00:00Z', monitor('2026-09-10T12:00:00Z')),
    spanItem('future', '2026-08-01T09:00:00Z', monitor('2026-10-30T12:00:00Z')),
  ]);
  const today = '2026-09-20';
  const window_ = computeRoadmapWindow(
    today,
    rows.flatMap((r) => timelineDateKeys(r.timeline)),
  );
  const at = (key: string) => computeDayOffset(key, window_.startDate);
  const spanOf = (id: string) => {
    const t = rows.find((r) => r.workItemId === id)!.timeline;
    return t.kind === 'span' ? t : null;
  };
  const overdue = spanOf('overdue');
  const future = spanOf('future');
  assert.ok(at(overdue!.currentTargetDateKey) < at(today));
  assert.ok(at(future!.currentTargetDateKey) > at(today));
  // Start earlier than everything else extends the window so no span is dropped.
  assert.ok(at(overdue!.startDateKey) >= 0 && window_.startDate <= '2026-08-01');
});

test('a start after its own target is an anomaly and keeps only the real target', () => {
  const [row] = buildAuthoritativeRoadmapRows([
    spanItem('a', '2026-11-01T09:00:00Z', monitor('2026-10-15T12:00:00Z')),
  ]);
  assert.deepEqual(row.timeline, { kind: 'milestone', targetDateKey: '2026-10-15' });
});

test('the roadmap renders a span as one line from a hollow circle start to a diamond target, keeps Today, and an honest empty state', () => {
  const source = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx',
  );
  assert.match(source, /timeline\.kind === 'span'/);
  assert.match(source, /data-testid="span-line"/);
  assert.match(source, /data-testid="span-start-marker"/);
  assert.match(source, /data-testid="span-target-marker"/);
  // Start is a circle, target a rotated square: never the same marker.
  assert.match(source, /borderRadius: '50%'/);
  assert.match(source, /rotate\(45deg\)/);
  // The old standalone vertical-line start cap is gone.
  assert.equal(/Start cap|START_CAP_HEIGHT/.test(source), false);
  assert.match(source, /TODAY/);
  assert.match(source, /No active workflows with a target date\./);
  assert.match(source, /Started \$\{formatRoadmapDateLabel/);
  assert.match(source, /onOpenWorkItem\(row\.workItemId, row\.title\)/);
});

test('rows are ordered by Workflow.startedAt ascending regardless of target, title or input order', () => {
  const rows = buildAuthoritativeRoadmapRows([
    spanItem('c-late-start', '2026-09-04T09:00:00Z', monitor('2026-09-10T12:00:00Z')),
    spanItem('a-early-start-late-target', '2026-08-29T09:00:00Z', monitor('2026-12-20T12:00:00Z')),
    spanItem('b-mid', '2026-09-02T09:00:00Z', monitor('2026-09-05T12:00:00Z')),
  ]);
  assert.deepEqual(
    rows.map((r) => r.workItemId),
    ['a-early-start-late-target', 'b-mid', 'c-late-start'],
  );
});

test('order compares real instants, not day keys or strings', () => {
  const rows = buildAuthoritativeRoadmapRows([
    spanItem('later-same-day', '2026-09-02T18:00:00Z', monitor('2026-10-01T12:00:00Z')),
    spanItem('earlier-same-day', '2026-09-02T06:00:00Z', monitor('2026-10-01T12:00:00Z')),
  ]);
  assert.deepEqual(
    rows.map((r) => r.workItemId),
    ['earlier-same-day', 'later-same-day'],
  );
});

test('equal starts use a stable secondary order (title, then id) independent of input order', () => {
  const items = [
    spanItem('id-2', '2026-09-02T09:00:00Z', monitor('2026-10-01T12:00:00Z'), { title: 'Beta' }),
    spanItem('id-3', '2026-09-02T09:00:00Z', monitor('2026-10-01T12:00:00Z'), { title: 'Alpha' }),
    spanItem('id-1', '2026-09-02T09:00:00Z', monitor('2026-10-01T12:00:00Z'), { title: 'Alpha' }),
  ];
  const expected = ['id-1', 'id-3', 'id-2'];
  assert.deepEqual(
    buildAuthoritativeRoadmapRows(items).map((r) => r.workItemId),
    expected,
  );
  assert.deepEqual(
    buildAuthoritativeRoadmapRows([...items].reverse()).map((r) => r.workItemId),
    expected,
  );
});

test('an invalid start is skipped and never replaced by another date', () => {
  assert.deepEqual(
    buildAuthoritativeRoadmapRows([spanItem('bad', 'not-a-date', monitor('2026-10-01T12:00:00Z'))]),
    [],
  );
});

test('sorting adds no backend API or per-row read and geometry stays start-to-target', () => {
  const adapter = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts',
  );
  assert.equal(/wailsjs|GetWorkItemTracking|fetch\(/.test(adapter), false);
  const rows = buildAuthoritativeRoadmapRows([
    spanItem('x', '2026-08-29T09:00:00Z', monitor('2026-12-20T12:00:00Z')),
  ]);
  assert.deepEqual(rows[0].timeline, {
    kind: 'span',
    startDateKey: '2026-08-29',
    currentTargetDateKey: '2026-12-20',
  });
});

test('Dashboard shows an unavailable message, not an empty timeline, when Tracking cannot be read', () => {
  const dashboard = sourceOf('../src/pages/Dashboard.tsx');
  assert.match(dashboard, /Workflow timing is unavailable\./);
});

test('the Dashboard API carries the authoritative Workflow start without per-row Tracking reads', () => {
  const api = sourceOf('../src/api/dashboardApi.ts');
  assert.match(api, /workflowStartedAt/);
  assert.equal(/GetWorkItemTracking/.test(api + sourceOf('../src/pages/Dashboard.tsx')), false);
});
