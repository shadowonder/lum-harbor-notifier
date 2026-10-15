import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import type {
  TrackedItem,
  TrackingProgression,
  TrackingWorkflow,
  WorkItemTracking,
} from '../src/api/tracking/trackingProductApi.ts';
import { buildTrackedItemRows } from '../src/tracking/trackingPresentation.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const progression = (over: Partial<TrackingProgression> = {}): TrackingProgression => ({
  totalSteps: 4,
  completedCount: 1,
  skippedCount: 0,
  passedCount: 1,
  progressPercent: 25,
  complete: false,
  currentStepId: 's2',
  currentStepKey: 'build',
  currentState: 'NOT_STARTED',
  ...over,
});

const item = (id: string, over: Partial<TrackedItem> = {}): TrackedItem => ({
  id,
  workflowId: 'wf1',
  parentItemId: null,
  title: id,
  itemType: null,
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  stepStates: [],
  progression: null,
  ...over,
});

const workflow = (id: string, name: string): TrackingWorkflow => ({
  id,
  workItemId: 'wi',
  workflowDefinitionId: `${id}-def`,
  name,
  version: 1,
  createdAt: '2026-09-01T00:00:00Z',
  definition: {
    id: `${id}-def`,
    name,
    version: 1,
    active: true,
    createdAt: '2026-09-01T00:00:00Z',
    steps: [],
  },
  steps: [],
});

const tracking = (
  trackedItems: TrackedItem[],
  wf: TrackingWorkflow | null = null,
): WorkItemTracking => ({
  workItemId: 'wi',
  trackedItems,
  workflow: wf,
  monitorPoints: [],
});

test('parent/child renders as hierarchy only: real parent identity drives depth and "Under"', () => {
  const rows = buildTrackedItemRows(
    tracking([
      item('root'),
      item('child', { parentItemId: 'root' }),
      item('grandchild', { parentItemId: 'child' }),
    ]),
  );
  assert.deepEqual(
    rows.map((r) => [r.item.title, r.depth, r.parentTitle]),
    [
      ['root', 0, null],
      ['child', 1, 'root'],
      ['grandchild', 2, 'child'],
    ],
  );
});

test('cycles in parentItemId do not loop forever and terminate safely', () => {
  const rows = buildTrackedItemRows(
    tracking([item('a', { parentItemId: 'b' }), item('b', { parentItemId: 'a' })]),
  );
  assert.equal(rows.length, 2);
  assert.ok(rows[0].depth >= 0 && rows[1].depth >= 0);
});

test('row models carry no synthetic dependency, blocked-by or topology fields', () => {
  const rows = buildTrackedItemRows(tracking([item('a')]));
  for (const row of rows) {
    assert.deepEqual(
      Object.keys(row).sort(),
      ['depth', 'item', 'parentTitle', 'workflow'],
      'a row exposes hierarchy and workflow only — no dependency/blocks fields',
    );
  }
});

test('workflow assignment is the WorkItem single workflow; no workflow is an honest null', () => {
  const withWf = buildTrackedItemRows(
    tracking(
      [item('bound', { title: 'Release plan' }), item('child-of-bound', { parentItemId: 'bound' })],
      workflow('wf1', 'Release'),
    ),
  );
  assert.equal(withWf[0].workflow?.name, 'Release');
  assert.equal(withWf[1].workflow?.name, 'Release');

  const withoutWf = buildTrackedItemRows(
    tracking([item('plain', { title: 'Release plan copy' })], null),
  );
  assert.equal(withoutWf[0].workflow, null);
});

test('backend progression is carried through unchanged and never merged into a WorkItem stage', () => {
  const a = progression({ passedCount: 3, progressPercent: 75 });
  const b = progression({ passedCount: 1, progressPercent: 25 });
  const rows = buildTrackedItemRows(
    tracking(
      [item('a', { progression: a }), item('b', { progression: b })],
      workflow('wf1', 'Release'),
    ),
  );
  assert.equal(rows[0].item.progression, a);
  assert.equal(rows[1].item.progression, b);
  assert.equal('stage' in rows[0] || 'currentStage' in rows[0], false);
});

test('closed WorkItems keep readable tracking: row building has no lifecycle input', () => {
  assert.equal(buildTrackedItemRows.length, 1);
  const rows = buildTrackedItemRows(tracking([item('done', { lifecycleState: 'COMPLETED' })]));
  assert.equal(rows.length, 1);
});

function productionSources(): string[] {
  const dirs = [
    'components/workItems/tracking',
    'components/workItems/overview',
    'tracking',
    'presentation/workItems/workItemOverviewPresentation.ts',
  ];
  const files: string[] = [];
  const walk = (rel: string) => {
    try {
      for (const entry of readdirSync(new URL(`../src/${rel}`, import.meta.url), {
        withFileTypes: true,
      })) {
        if (entry.isDirectory()) walk(`${rel}/${entry.name}`);
        else if (/\.tsx?$/.test(entry.name)) files.push(`${rel}/${entry.name}`);
      }
    } catch {
      files.push(rel);
    }
  };
  dirs.forEach(walk);
  return files;
}

test('production Tracking uses the bounded DEPENDS_ON truth and no prototype relation engine', () => {
  const api = source('api/tracking/trackingProductApi.ts');
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  assert.match(api, /relationType: 'DEPENDS_ON'/);
  assert.match(panels, /Depends On/);
  assert.match(panels, /Blocks/);
  for (const file of productionSources())
    assert.doesNotMatch(source(file), /dependencyExperiment|@antv|x6/i, file);
});

test('production surfaces do not import the fixture explorer, topology or dependency prototype', () => {
  const production = [
    'pages/WorkItems.tsx',
    'components/workItems/overview/OverviewSummaryBlocks.tsx',
    'components/workItems/tracking/TrackingPanels.tsx',
    'components/workItems/tracking/TrackingDialogs.tsx',
    'components/workItems/workspace/WorkItemScopedHistory.tsx',
  ];
  for (const file of production) {
    const text = source(file);
    assert.equal(
      /dependencyExperiment|itemExplorer|WorkflowCanvas|compactTopology|CompactWorkflowTopology|workItemPrototypeFixtures|mockData/.test(
        text,
      ),
      false,
      file,
    );
  }
});

test('modelOverride (the prototype dependency/topology fixture path) has no production caller', () => {
  const pages = ['pages/WorkItems.tsx', 'pages/Dashboard.tsx', 'components/AppShell.tsx'];
  for (const file of pages) assert.equal(source(file).includes('modelOverride'), false, file);
});

test('no generic health assessment is shown for real WorkItems (row or Workspace)', () => {
  const row = source('components/workItems/WorkItemRow.tsx');
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  assert.equal(/HEALTH UNKNOWN|aggregateHealth|healthLabel/.test(row), false);
  assert.match(workspace, /!realTracking && \(\s*<Chip/);
  assert.equal((workspace.match(/HEALTH UNKNOWN/g) ?? []).length, 1);
});

test('Overview left identity region no longer carries an Updated metadata line', () => {
  const row = source('components/workItems/WorkItemRow.tsx');
  assert.equal(/formatRelativeTime\(item\.updatedAt\)|>Updated /.test(row), false);
});

test('Tracking unavailable is a distinct state from empty; no fixture substitution on failure', () => {
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  assert.match(panels, /TrackingStateNotice/);
  assert.equal(/prototype|fixture/i.test(panels), false);
});
