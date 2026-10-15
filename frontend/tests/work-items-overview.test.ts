import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type { app } from '../wailsjs/go/models';
import { mapWorkItemOverviewDTO } from '../src/api/workItems/workItemDtoAdapter.ts';
import type {
  ProductAttentionSummary,
  ProductTrackingSummary,
  ProductWorkItemSummary,
} from '../src/api/workItems/productApi.ts';
import { buildOverviewRowView } from '../src/presentation/workItems/workItemOverviewPresentation.ts';
import {
  groupOverviewItemsByType,
  overviewItemTypeGroup,
  overviewMarkerTooltip,
} from '../src/presentation/workItems/workItemOverviewPresentation.ts';
import {
  initialWorkItemsPageState,
  workItemsPageReducer,
} from '../src/pages/workItemsPageState.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');
const now = new Date('2026-09-20T12:00:00Z');
const workflow = {
  id: 'wf',
  definitionId: 'def',
  stages: [
    { id: 's1', key: 'plan', title: 'Plan', sequence: 0, terminal: false },
    { id: 's2', key: 'ship', title: 'Ship', sequence: 1, terminal: true },
  ],
};
const distribution = {
  notStarted: 0,
  inProgress: 1,
  waiting: 0,
  blocked: 0,
  failed: 0,
  unknown: 0,
  skipped: 0,
  completed: 0,
};
const item = (
  id: string,
  itemType: string | null = 'TASK',
  over: Partial<ProductTrackingSummary['items'][number]> = {},
): ProductTrackingSummary['items'][number] => ({
  id,
  title: id,
  itemType,
  parentItemId: null,
  lifecycle: 'ACTIVE',
  currentStepId: 's1',
  currentState: 'IN_PROGRESS',
  complete: false,
  resolved: false,
  signal: {
    state: 'NEUTRAL',
    hasActiveTrackingTodo: false,
    governingTodoDueAt: null,
    actionableAttentionCount: 0,
    highestAttentionSeverity: null,
    hasActiveExpectation: false,
    hasSatisfiedExpectation: false,
    hasOverdueUnmetExpectation: false,
  },
  ...over,
});
const tracking = (over: Partial<ProductTrackingSummary> = {}): ProductTrackingSummary => ({
  trackedItemCount: 1,
  workflow,
  items: [item('t1')],
  stateDistribution: distribution,
  activeFinalTarget: null,
  ...over,
});
const attention = (over: Partial<ProductAttentionSummary> = {}): ProductAttentionSummary => ({
  openIssueCount: 0,
  actionableIssueCount: 0,
  snoozedIssueCount: 0,
  highestActionableSeverity: null,
  ...over,
});
const view = (summary: ProductWorkItemSummary) => buildOverviewRowView(summary, now);

test('workflow summary becomes one dynamic mini-map with backend item positions', () => {
  const result = view({ tracking: tracking(), attention: attention() });
  assert.equal(result.tracking.status, 'available');
  if (result.tracking.status !== 'available') return;
  assert.equal(result.tracking.workflow.definitionId, 'def');
  assert.equal(result.tracking.items[0].currentStepId, 's1');
  assert.deepEqual(result.tracking.stateDistribution, distribution);
});

test('no workflow, workflow with no items, unavailable tracking, and real items stay distinct', () => {
  assert.equal(
    view({
      tracking: tracking({ workflow: null, items: [], trackedItemCount: 0 }),
      attention: attention(),
    }).tracking.status,
    'no-workflow',
  );
  assert.equal(
    view({ tracking: tracking({ items: [], trackedItemCount: 0 }), attention: attention() })
      .tracking.status,
    'workflow-empty',
  );
  assert.equal(view({ tracking: null, attention: attention() }).tracking.status, 'unavailable');
  assert.equal(view({ tracking: tracking(), attention: attention() }).tracking.status, 'available');
});

test('attention stays a bounded aggregate for the left-side overall signal', () => {
  const result = view({
    tracking: tracking(),
    attention: attention({
      openIssueCount: 2,
      actionableIssueCount: 1,
      snoozedIssueCount: 1,
      highestActionableSeverity: 'WARNING',
    }),
  });
  assert.equal(result.attention.status, 'open');
  if (result.attention.status !== 'open') return;
  assert.equal(result.attention.actionableCount, 1);
  assert.equal(result.attention.highestActionableSeverity, 'WARNING');
});

test('marker grouping is stable by semantic shape and keeps same types contiguous', () => {
  const input = [
    item('s1', 'STORY'),
    item('t1', 'TASK'),
    item('s2', 'USER STORY'),
    item('d1', 'BUG'),
    item('t2', 'TASK'),
    item('g1', 'MILESTONE'),
    item('s3', 'STORY'),
    item('t3', 'TASK'),
    item('t4', 'TASK'),
    item('d2', 'DEFECT'),
  ];
  assert.deepEqual(
    groupOverviewItemsByType(input).map(([group, items]) => [
      group,
      items.map((entry) => entry.id),
    ]),
    [
      ['story', ['s1', 's2', 's3']],
      ['task', ['t1', 't2', 't3', 't4']],
      ['defect', ['d1', 'd2']],
      ['gate', ['g1']],
    ],
  );
  assert.equal(overviewItemTypeGroup('RESEARCH'), 'other');
});

test('dense same-type groups retain every marker for multi-row wrapping', () => {
  const input = [
    ...Array.from({ length: 7 }, (_, index) => item(`s${index}`, 'STORY')),
    ...Array.from({ length: 7 }, (_, index) => item(`t${index}`, 'TASK')),
    ...Array.from({ length: 5 }, (_, index) => item(`d${index}`, 'DEFECT')),
  ];
  const result = view({
    tracking: tracking({ trackedItemCount: input.length, items: input }),
    attention: attention(),
  });
  assert.equal(result.tracking.status, 'available');
  if (result.tracking.status !== 'available') return;
  assert.equal(result.tracking.items.length, 19);
  assert.deepEqual(
    groupOverviewItemsByType(result.tracking.items).map(([, items]) => items.length),
    [7, 7, 5],
  );
});

test('a real active FINAL_TARGET renders a real target date', () => {
  const result = view({
    tracking: tracking({
      activeFinalTarget: {
        id: 'mp',
        subjectKind: 'WORK_ITEM',
        subjectId: 'wi',
        kind: 'FINAL_TARGET',
        title: 'T',
        dueAt: '2026-10-15T12:00:00Z',
        timezone: 'UTC',
        expectationKind: 'WORK_ITEM_LIFECYCLE',
        expectedWorkItemLifecycle: 'COMPLETED',
        expectedStepId: null,
        state: 'ACTIVE',
        satisfiedAt: null,
        createdAt: '2026-09-01T00:00:00Z',
        updatedAt: '2026-09-01T00:00:00Z',
      },
    }),
    attention: attention(),
  });
  assert.equal(result.target.status, 'set');
  if (result.target.status === 'set') assert.equal(result.target.dateLabel, 'Oct 15');
});

test('DTO adapter preserves availability and maps the semantic overview contract', () => {
  const dto = {
    items: [
      {
        workItem: {
          id: 'a',
          title: 'A',
          lifecycleState: 'ACTIVE',
          createdAt: '2026-09-01T00:00:00Z',
          updatedAt: '2026-09-01T00:00:00Z',
        },
        tracking: {
          trackedItemCount: 0,
          workflow: null,
          items: [],
          stateDistribution: {
            notStarted: 0,
            inProgress: 0,
            waiting: 0,
            blocked: 0,
            failed: 0,
            unknown: 0,
            skipped: 0,
            completed: 0,
          },
        },
        attention: { openIssueCount: 0, actionableIssueCount: 0, snoozedIssueCount: 0 },
      },
    ],
    trackingAvailable: false,
    attentionAvailable: true,
  } as unknown as app.WorkItemOverviewDTO;
  const mapped = mapWorkItemOverviewDTO(dto);
  assert.equal(mapped.items[0].tracking, null);
  assert.equal(mapped.items[0].attention?.actionableIssueCount, 0);
});

test('overview uses one read, no production fixture path, no stale progress wording, and keyboard navigation', () => {
  const page = source('pages/WorkItems.tsx');
  const row = source('components/workItems/WorkItemRow.tsx');
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  assert.match(page, /api\.overview\(\)/);
  assert.equal(page.includes('api.list()'), false);
  assert.equal(
    /modelOverride|CompactWorkflowTopology|workItemPrototypeFixtures|buildWorkflowModelFromWorkItem/.test(
      row,
    ),
    false,
  );
  assert.equal(/with workflow|workflow complete|LinearProgress/.test(row + blocks), false);
  assert.match(row, /event\.key === 'Enter' \|\| event\.key === ' '/);
  assert.match(blocks, /overview-mini-map/);
  assert.match(blocks, /WorkflowNodeShape/);
  assert.match(blocks, /item\.resolved/);
  assert.match(blocks, /item\.signal\.state/);
  assert.match(blocks, /Todo due within 3 days/);
  assert.match(blocks, /no open linked Todo with a due target/);
  assert.equal(blocks.includes('satisfied monitoring'), false);
  assert.equal(
    /stateColor|currentState.*primary\.main|hiddenItemCount|more items/.test(blocks),
    false,
  );
  assert.equal(
    /definitionName|definitionVersion|recommendedAction|detail\.reason/.test(blocks),
    false,
  );
  assert.match(row, /WebkitLineClamp: 3/);
  assert.match(row, /OverviewAttentionText/);
  assert.match(row, /TRACKED WORK/);
});

test('reducer stores summaries by identity and clears them on failure', () => {
  const summary: ProductWorkItemSummary = { tracking: null, attention: null };
  const loaded = workItemsPageReducer(initialWorkItemsPageState, {
    type: 'loadSucceeded',
    items: [],
    summaries: { a: summary },
  });
  assert.deepEqual(loaded.summaries, { a: summary });
  assert.deepEqual(
    workItemsPageReducer(loaded, { type: 'loadFailed', message: 'x' } as never).summaries,
    {},
  );
});

// ---- Iteration 3: scan-first hierarchy -------------------------------------

test('LEFT carries exactly one tag, sourced from the WorkItem lifecycle, and no dynamic tags', () => {
  const row = source('components/workItems/WorkItemRow.tsx');
  assert.equal((row.match(/<Chip/g) ?? []).length, 1);
  assert.match(row, /label=\{item\.lifecycleState\}/);
  assert.equal(
    /OverviewAttentionSignal|NEEDS ACTION|NEED ACTION|WARNING|OVERDUE|AT RISK|HEALTH/.test(row),
    false,
  );
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  assert.equal(/<Chip/.test(blocks), false);
});

test('RIGHT is the primary reading area: SUMMARY, exact TRACKED WORK counts and compact ATTENTION text', () => {
  const row = source('components/workItems/WorkItemRow.tsx');
  const jsx = row.slice(row.indexOf('return ('));
  const [left, rest] = jsx.split('OverviewTrackingCenter tracking');
  const right = rest.slice(rest.indexOf('SUMMARY'));
  assert.match(right, /SUMMARY[\s\S]*TRACKED WORK[\s\S]*ATTENTION/);
  assert.match(right, /WebkitLineClamp: 4/);
  assert.match(right, /OverviewRollup/);
  assert.match(right, /OverviewAttentionText/);
  assert.equal(/OverviewRollup|OverviewAttentionText|TRACKED WORK/.test(left), false);
});

test('exact counts live only in the RIGHT rollup; the center thumbnail renders no counts or long text', () => {
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  const center = blocks.slice(
    blocks.indexOf('export function OverviewTrackingCenter'),
    blocks.indexOf('/** Compact Attention text fact'),
  );
  assert.equal(
    /stateDistribution|trackedItemCount|\{[\w.]*[cC]ount\}/.test(
      center.replace(/tracking\.items/g, ''),
    ),
    false,
  );
  assert.equal(/definitionName|definitionVersion|stage\.count|\+\d|more items/.test(center), false);
  assert.match(blocks, /tracking\.stateDistribution\[key\]/);
});

test('Attention renders as compact text, never notification-style detail', () => {
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  const text = blocks.slice(blocks.indexOf('export function OverviewAttentionText'));
  assert.match(text, /needs|need/);
  assert.match(text, /No open attention/);
  assert.match(text, /Attention unavailable/);
  assert.equal(/recommendedAction|reason|message|notification/i.test(text), false);
});

test('long titles wrap to three lines with the full title available, in LEFT', () => {
  const row = source('components/workItems/WorkItemRow.tsx');
  assert.match(row, /WebkitLineClamp: 3/);
  assert.match(row, /title=\{item\.title\}/);
});

// ---- Iteration 4A: marker hover ---------------------------------------------

test('marker tooltip line 1 is TYPE · Title and line 2 is the authoritative execution state', () => {
  const tip = overviewMarkerTooltip(
    item('a', 'USER_STORY', { title: 'GraphQL Payment API', currentState: 'IN_PROGRESS' }),
    'Implement',
  );
  assert.equal(tip.headline, 'USER STORY · GraphQL Payment API');
  assert.equal(tip.state, 'IN PROGRESS');
  const defect = overviewMarkerTooltip(
    item('b', 'DEFECT', { title: 'Token Leak', currentState: 'BLOCKED' }),
    'Fix',
  );
  assert.equal(defect.headline, 'DEFECT · Token Leak');
  assert.equal(defect.state, 'BLOCKED');
});

test('tooltip state comes from the backend fact for every state, never from color or stage', () => {
  for (const state of [
    'NOT_STARTED',
    'IN_PROGRESS',
    'WAITING',
    'BLOCKED',
    'FAILED',
    'UNKNOWN',
    'SKIPPED',
  ] as const) {
    const neutral = overviewMarkerTooltip(item('x', 'TASK', { currentState: state }), 'Any');
    const problem = overviewMarkerTooltip(
      item('x', 'TASK', {
        currentState: state,
        signal: { ...item('y').signal, state: 'PROBLEM' },
      }),
      'Other stage',
    );
    assert.equal(neutral.state, state.replace('_', ' '));
    assert.equal(problem.state, neutral.state);
  }
  assert.equal(
    overviewMarkerTooltip(item('c', 'TASK', { complete: true, resolved: true }), 'Done').state,
    'COMPLETED',
  );
  assert.equal(
    overviewMarkerTooltip(item('n', null, { currentState: null }), 'S').headline,
    'ITEM · n',
  );
});

test('tooltip exposes no id, dependency, notification or attention detail', () => {
  const tip = overviewMarkerTooltip(item('id-123', 'TASK', { title: 'Title' }), 'Stage');
  assert.deepEqual(Object.keys(tip).sort(), ['headline', 'stage', 'state']);
  assert.equal(/id-123/.test(tip.headline + tip.state), false);
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  const marker = blocks.slice(
    blocks.indexOf('function OverviewTypeMarker'),
    blocks.indexOf('export function OverviewTrackingCenter'),
  );
  assert.match(marker, /<Tooltip/);
  assert.match(marker, /maxWidth: 260/);
  assert.match(marker, /overflowWrap: 'anywhere'/);
  assert.match(marker, /tabIndex=\{0\}/);
  assert.match(marker, /aria-label=\{label\}/);
  assert.equal(/parentItemId|dependency|notification|recommended|description/i.test(marker), false);
});

test('marker accessible label still carries type, title, state and stage; semantics untouched', () => {
  const blocks = source('components/workItems/overview/OverviewSummaryBlocks.tsx');
  assert.match(
    blocks,
    /\$\{item\.title\} · \$\{item\.itemType \?\? 'unknown type'\} · \$\{execution\} · \$\{signalLabel\(item\)\} · \$\{stageTitle\}/,
  );
  assert.match(blocks, /signalColor\(theme, item\.signal\.state\)/);
  assert.match(blocks, /isResolved=\{item\.resolved\}/);
  assert.match(blocks, /itemType=\{group\}/);
});
