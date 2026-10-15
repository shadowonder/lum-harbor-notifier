import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type {
  TrackedItem,
  TrackingProgression,
  TrackingStep,
  TrackingStepState,
  TrackingWorkflow,
  WorkItemTracking,
} from '../src/api/tracking/trackingProductApi.ts';
import { buildWorkflowStructure, focusSet } from '../src/tracking/workflowStructure.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const step = (id: string, title: string, sequence: number, terminal = false): TrackingStep => ({
  id,
  key: id,
  title,
  sequence,
  terminal,
});

const workflow = (id: string, name: string, titles: string[]): TrackingWorkflow => {
  const steps = titles.map((t, i) => step(`${id}-${i + 1}`, t, i + 1, i === titles.length - 1));
  return {
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
      steps,
    },
    steps,
  };
};

const progression = (
  wf: TrackingWorkflow,
  index: number,
  state: TrackingStepState,
  complete = false,
): TrackingProgression => ({
  totalSteps: wf.steps.length,
  completedCount: index,
  skippedCount: 0,
  passedCount: index,
  progressPercent: Math.round((index / wf.steps.length) * 100),
  complete,
  currentStepId: complete ? null : wf.steps[index].id,
  currentStepKey: complete ? null : wf.steps[index].key,
  currentState: complete ? null : state,
});

const item = (id: string, over: Partial<TrackedItem> = {}): TrackedItem => ({
  id,
  workflowId: 'wf-d',
  parentItemId: null,
  title: id,
  itemType: 'TASK',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  stepStates: [],
  progression: null,
  ...over,
});

const delivery = workflow('wf-d', 'Delivery Flow', [
  'Discover',
  'Design',
  'Implement',
  'Verify',
  'Release',
]);

const tracking = (
  items: TrackedItem[],
  wf: TrackingWorkflow | null = delivery,
): WorkItemTracking => ({
  workItemId: 'wi',
  trackedItems: items,
  workflow: wf,
  monitorPoints: [],
});

const sample = () =>
  tracking([
    item('foundation', {
      progression: progression(delivery, 2, 'IN_PROGRESS'),
    }),
    item('api', {
      parentItemId: 'foundation',
      progression: progression(delivery, 3, 'BLOCKED'),
    }),
    item('token', {
      parentItemId: 'foundation',
      progression: progression(delivery, 3, 'WAITING'),
    }),
    item('frontend', {
      progression: progression(delivery, 2, 'IN_PROGRESS'),
    }),
    item('threeds', {
      parentItemId: 'frontend',
      progression: progression(delivery, 3, 'WAITING'),
    }),
    item('done-sub', {
      parentItemId: 'threeds',
      itemType: 'TASK',
      progression: progression(delivery, 4, 'COMPLETED', true),
    }),
  ]);

test('stage axis step headers come from the WorkItem single Workflow, never a hardcoded list', () => {
  const s = buildWorkflowStructure(sample());
  assert.equal(s.workflow?.name, 'Delivery Flow');
  assert.equal(s.workflow?.version, 1);
  assert.deepEqual(
    s.steps.map((x) => x.title),
    ['Discover', 'Design', 'Implement', 'Verify', 'Release'],
  );
});

test('step order follows the definition sequence even if the payload arrives unordered', () => {
  const shuffled: TrackingWorkflow = {
    ...delivery,
    steps: [...delivery.steps].reverse(),
    definition: {
      ...delivery.definition,
      steps: [...delivery.steps].reverse(),
    },
  };
  const s = buildWorkflowStructure(
    tracking(
      [
        item('a', {
          progression: progression(delivery, 3, 'BLOCKED'),
        }),
      ],
      shuffled,
    ),
  );
  assert.deepEqual(
    s.steps.map((x) => x.title),
    ['Discover', 'Design', 'Implement', 'Verify', 'Release'],
  );
  assert.equal(s.nodes[0].column, 3);
});

test('node column is the backend currentStepId position; React never searches for a first unfinished step', () => {
  const s = buildWorkflowStructure(sample());
  const byId = new Map(s.nodes.map((n) => [n.item.id, n]));
  assert.equal(byId.get('foundation')?.column, 2);
  assert.equal(byId.get('api')?.column, 3);
  assert.equal(byId.get('api')?.state, 'BLOCKED');
  assert.equal(byId.get('token')?.column, 3);
  assert.equal(byId.get('token')?.state, 'WAITING');

  // Step states that contradict the backend position are ignored: only progression decides.
  const contradictory = item('x', {
    progression: progression(delivery, 4, 'IN_PROGRESS'),
    stepStates: delivery.steps.map((st) => ({
      workflowStepId: st.id,
      state: 'NOT_STARTED' as const,
      enteredAt: null,
      completedAt: null,
    })),
  });
  const n = buildWorkflowStructure(tracking([contradictory])).nodes[0];
  assert.equal(n.column, 4);
  const logic = source('tracking/workflowStructure.ts');
  assert.equal(/stepStates|\.find\(\(?s(tep)?\)? => .*(NOT_STARTED|state)/.test(logic), false);
});

test('items on the same stage render independently with their own states', () => {
  const s = buildWorkflowStructure(sample());
  const byId = new Map(s.nodes.map((n) => [n.item.id, n]));
  const api = byId.get('api')!;
  const token = byId.get('token')!;
  const threeds = byId.get('threeds')!;

  // All three items sit at column 3 ("Verify")
  assert.equal(api.column, 3);
  assert.equal(token.column, 3);
  assert.equal(threeds.column, 3);

  // But each maintains its own independent state
  assert.equal(api.state, 'BLOCKED');
  assert.equal(token.state, 'WAITING');
  assert.equal(threeds.state, 'WAITING');

  // And separate vertical row positions
  assert.notEqual(api.row, token.row);
  assert.notEqual(token.row, threeds.row);
});

test('a complete workflow renders at the terminal step as COMPLETED', () => {
  const s = buildWorkflowStructure(sample());
  const done = s.nodes.find((n) => n.item.id === 'done-sub')!;
  assert.equal(done.column, 4);
  assert.equal(done.state, 'COMPLETED');
  assert.equal(done.step?.terminal, true);
});

test('parent/child hierarchy comes from parentItemId in tree preorder across the single workflow', () => {
  const s = buildWorkflowStructure(sample());
  assert.deepEqual(
    s.nodes.map((n) => [n.item.id, n.depth, n.parentId]),
    [
      ['foundation', 0, null],
      ['api', 1, 'foundation'],
      ['token', 1, 'foundation'],
      ['frontend', 0, null],
      ['threeds', 1, 'frontend'],
      ['done-sub', 2, 'threeds'],
    ],
  );
});

test('a work item without a workflow returns empty structure', () => {
  const s = buildWorkflowStructure(tracking([item('plain')], null));
  assert.equal(s.workflow, null);
  assert.deepEqual(s.steps, []);
  assert.deepEqual(s.nodes, []);
});

test('every real StepState maps to a node state without throwing', () => {
  const states: TrackingStepState[] = [
    'NOT_STARTED',
    'IN_PROGRESS',
    'COMPLETED',
    'WAITING',
    'BLOCKED',
    'FAILED',
    'UNKNOWN',
    'SKIPPED',
  ];
  const items = states.map((st, i) => item(`i${i}`, { progression: progression(delivery, 1, st) }));
  const nodes = buildWorkflowStructure(tracking(items)).nodes;
  assert.deepEqual(
    nodes.map((n) => n.state),
    states,
  );
});

test('an item with no backend current step is shown without inventing a position', () => {
  const n = buildWorkflowStructure(tracking([item('n', { progression: null })])).nodes[0];
  assert.equal(n.step, null);
  assert.equal(n.state, null);
  assert.equal(n.column, 0);
});

test('selection focuses the real ancestor chain and every descendant, never siblings', () => {
  const t = sample();
  assert.deepEqual([...focusSet(t, 'threeds')].sort(), ['done-sub', 'frontend', 'threeds']);
  assert.deepEqual([...focusSet(t, 'foundation')].sort(), ['api', 'foundation', 'token']);
  assert.deepEqual([...focusSet(t, 'api')].sort(), ['api', 'foundation']);
});

test('hierarchy and dependency use distinct truthful connector treatments', () => {
  const map = source('components/workItems/tracking/WorkflowStructureMap.tsx');
  assert.match(map, /data-testid="hierarchy-edge"/);
  assert.match(map, /data-testid="dependency-edge"/);
  assert.match(map, /strokeDasharray/);
  assert.match(map, /DEPENDS ON/);
  assert.match(map, /BLOCKS/);
});

test('the structure view never imports old prototype dependency/topology/fixture code', () => {
  for (const file of [
    'tracking/workflowStructure.ts',
    'components/workItems/tracking/WorkflowStructureMap.tsx',
  ]) {
    assert.equal(
      /dependencyExperiment|itemExplorer|WorkflowCanvas|compactTopology|CompactWorkflowTopology|workItemPrototypeFixtures|mockData|@antv|x6/.test(
        source(file),
      ),
      false,
      file,
    );
  }
});

test('the surrounding Workflow tab is read-first and history sits directly below the graph', () => {
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  assert.match(panels, /<WorkflowStructureMap tracking=\{tracking\} \/>/);
  for (const marker of ['Tracking context', 'Workflow History']) {
    assert.ok(panels.includes(marker), marker);
  }
  assert.doesNotMatch(panels, /Workflow progress|planStepCommands|Set Target|Add Tracked Item/);
  assert.ok(panels.indexOf('WorkflowStructureMap') < panels.indexOf('Workflow History'));
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  for (const marker of ['value="workflow"', 'value="items"', 'value="history"']) {
    assert.ok(workspace.includes(marker), marker);
  }
});
