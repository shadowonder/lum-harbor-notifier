import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';
import type { app } from '../wailsjs/go/models';
import {
  mapMonitorPoint,
  mapWorkItemTracking,
  normalizeTrackingError,
} from '../src/api/tracking/trackingDtoAdapter.ts';
import {
  TrackingUnavailableError,
  type TrackedItem,
  type TrackingWorkflow,
  type WorkItemTracking,
} from '../src/api/tracking/trackingProductApi.ts';
import { runTrackingMutation } from '../src/tracking/trackingMutation.ts';
import {
  buildMonitorInput,
  buildTrackedItemRows,
  describeExpectation,
  describeTiming,
  findActiveFinalTarget,
  findNextExpectation,
  planStepCommands,
} from '../src/tracking/trackingPresentation.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const dto = (over: Partial<app.WorkItemTrackingDTO> = {}) =>
  ({
    workItemId: 'wi-1',
    trackedItems: [
      {
        id: 'ti-1',
        parentItemId: null,
        title: 'API',
        itemType: 'service',
        lifecycleState: 'ACTIVE',
        workflowId: 'wf-inst-1',
        completedAt: null,
        createdAt: '2026-09-20T10:00:00Z',
        updatedAt: '2026-09-20T10:05:00Z',
        stepStates: [
          {
            workflowStepId: 's1',
            state: 'COMPLETED',
            enteredAt: '2026-09-20T10:01:00Z',
            completedAt: '2026-09-20T10:02:00Z',
          },
          {
            workflowStepId: 's2',
            state: 'IN_PROGRESS',
            enteredAt: '2026-09-20T10:03:00Z',
            completedAt: null,
          },
        ],
        progression: {
          totalSteps: 2,
          completedCount: 1,
          skippedCount: 0,
          passedCount: 1,
          progressPercent: 50,
          complete: false,
          currentStepId: 's2',
          currentStepKey: 'ship',
          currentState: 'IN_PROGRESS',
        },
      },
      {
        id: 'ti-2',
        parentItemId: 'ti-1',
        title: 'Docs',
        itemType: null,
        lifecycleState: 'ACTIVE',
        workflowId: 'wf-inst-1',
        completedAt: null,
        createdAt: '2026-09-20T10:10:00Z',
        updatedAt: '2026-09-20T10:10:00Z',
        stepStates: [],
        progression: null,
      },
    ],
    workflow: {
      id: 'wf-inst-1',
      workItemId: 'wi-1',
      workflowDefinitionId: 'wf-1',
      createdAt: '2026-09-20T09:00:00Z',
      definition: {
        id: 'wf-1',
        name: 'Release',
        version: 2,
        active: true,
        createdAt: '2026-09-20T09:00:00Z',
        steps: [
          { id: 's1', key: 'plan', title: 'Plan', sequence: 1, terminal: false },
          { id: 's2', key: 'ship', title: 'Ship', sequence: 2, terminal: true },
        ],
      },
    },
    monitorPoints: [
      {
        id: 'm-1',
        subjectKind: 'TRACKED_ITEM',
        subjectId: 'ti-1',
        kind: 'CHECKPOINT',
        title: 'API shipped',
        dueAt: '2026-10-01T00:00:00Z',
        timezone: 'UTC',
        expectationKind: 'TRACKED_ITEM_STEP_COMPLETED',
        expectedWorkItemLifecycle: null,
        expectedStepId: 's2',
        state: 'ACTIVE',
        satisfiedAt: null,
        createdAt: '2026-09-20T10:20:00Z',
        updatedAt: '2026-09-20T10:20:00Z',
      },
      {
        id: 'm-2',
        subjectKind: 'WORK_ITEM',
        subjectId: 'wi-1',
        kind: 'FINAL_TARGET',
        title: 'Ship it',
        dueAt: '2026-10-15T22:00:00Z',
        timezone: 'America/Chicago',
        expectationKind: 'WORK_ITEM_LIFECYCLE',
        expectedWorkItemLifecycle: 'COMPLETED',
        expectedStepId: null,
        state: 'ACTIVE',
        satisfiedAt: null,
        createdAt: '2026-09-20T10:21:00Z',
        updatedAt: '2026-09-20T10:21:00Z',
      },
    ],
    ...over,
  }) as unknown as app.WorkItemTrackingDTO;

const tracking = (): WorkItemTracking => mapWorkItemTracking(dto());

test('the adapter maps the real Tracking DTO into semantic models with explicit nullables', () => {
  const view = tracking();
  assert.equal(view.workItemId, 'wi-1');
  assert.equal(view.trackedItems[0].progression?.progressPercent, 50);
  assert.equal(view.trackedItems[0].progression?.currentState, 'IN_PROGRESS');
  assert.equal(view.trackedItems[1].progression, null);
  assert.equal(view.trackedItems[1].parentItemId, 'ti-1');
  assert.equal(view.workflow?.steps[1].terminal, true);
  assert.equal(view.monitorPoints[1].expectedWorkItemLifecycle, 'COMPLETED');
  assert.equal(view.monitorPoints[0].expectedStepId, 's2');
});

test('valid empty tracking maps to empty collections (not an error)', () => {
  const view = mapWorkItemTracking(
    dto({ trackedItems: [], workflow: undefined, monitorPoints: [] }),
  );
  assert.deepEqual(
    [view.trackedItems.length, view.workflow, view.monitorPoints.length],
    [0, null, 0],
  );
});

test('unknown enum values and invalid instants are rejected, never guessed', () => {
  assert.throws(() =>
    mapMonitorPoint({ ...dto().monitorPoints[0], state: 'OVERDUE' } as app.MonitorPointDTO),
  );
  assert.throws(() =>
    mapMonitorPoint({ ...dto().monitorPoints[0], kind: 'DEPLOYMENT' } as app.MonitorPointDTO),
  );
  assert.throws(() =>
    mapMonitorPoint({ ...dto().monitorPoints[0], dueAt: 'soon' } as app.MonitorPointDTO),
  );
});

test('an unavailable WorkItem error stays distinct from empty tracking and from other errors', () => {
  assert.ok(
    normalizeTrackingError('the work item is not available') instanceof TrackingUnavailableError,
  );
  const other = normalizeTrackingError(new Error('load work item tracking failed'));
  assert.ok(!(other instanceof TrackingUnavailableError));
  assert.match(other.message, /failed/);
});

test('hierarchy is presented from real parent identity in backend order', () => {
  const rows = buildTrackedItemRows(tracking());
  assert.deepEqual(
    rows.map((r) => [r.item.title, r.depth, r.parentTitle]),
    [
      ['API', 0, null],
      ['Docs', 1, 'API'],
    ],
  );
  assert.equal(rows[0].workflow?.name, 'Release');
  assert.equal(rows[1].workflow?.name, 'Release');
});

test('step commands come only from the backend current step and never invent a step ID', () => {
  const view = tracking();
  const workflow = view.workflow;
  const plan = planStepCommands(view.trackedItems[0], workflow);
  assert.equal(plan?.workflowStepId, 's2');
  assert.deepEqual(
    plan?.commands.map((c) => c.target),
    ['COMPLETED', 'WAITING', 'BLOCKED'],
  );

  const withState = (
    state: NonNullable<TrackedItem['progression']>['currentState'],
    complete = false,
  ): TrackedItem => ({
    ...view.trackedItems[0],
    progression: { ...view.trackedItems[0].progression!, currentState: state, complete },
  });
  assert.deepEqual(
    planStepCommands(withState('NOT_STARTED'), workflow)?.commands.map((c) => c.target),
    ['IN_PROGRESS'],
  );
  assert.deepEqual(
    planStepCommands(withState('BLOCKED'), workflow)?.commands.map((c) => c.target),
    ['IN_PROGRESS', 'COMPLETED'],
  );
  assert.equal(
    planStepCommands(withState(null, true), workflow),
    null,
    'complete workflow offers no step command',
  );
  assert.equal(planStepCommands(view.trackedItems[1], null), null, 'no workflow, no command');
  const noCurrent: TrackedItem = {
    ...view.trackedItems[0],
    progression: { ...view.trackedItems[0].progression!, currentStepId: null },
  };
  assert.equal(planStepCommands(noCurrent, workflow), null, 'no backend current step, no command');
  const ghost: TrackedItem = {
    ...view.trackedItems[0],
    progression: { ...view.trackedItems[0].progression!, currentStepId: 'not-in-workflow' },
  };
  assert.equal(planStepCommands(ghost, workflow as TrackingWorkflow), null);
});

test('progression is used directly: the frontend never recomputes it', () => {
  const files = [
    'tracking/trackingPresentation.ts',
    'tracking/useWorkItemTracking.ts',
    'components/workItems/tracking/TrackingPanels.tsx',
    'components/workItems/tracking/TrackingDialogs.tsx',
  ];
  for (const file of files) {
    const text = source(file);
    assert.doesNotMatch(text, /Math\.(round|floor)\([^)]*(passed|completed|steps)/i, file);
    assert.doesNotMatch(text, /filter\([^)]*(COMPLETED|SKIPPED)/, file);
    assert.doesNotMatch(text, /ComputeProgression|computeProgression/, file);
  }
  assert.match(
    source('components/workItems/tracking/TrackingPanels.tsx'),
    /progression\?\.progressPercent/,
  );
  assert.match(source('components/workItems/tracking/TrackingPanels.tsx'), /currentState/);
});

test('monitor wording is derived purely and never invents risk/health', () => {
  const view = tracking();
  const [checkpoint, target] = view.monitorPoints;
  const now = new Date('2026-09-20T00:00:00Z');
  assert.equal(describeTiming(checkpoint, now), 'Due in 11 days');
  assert.match(describeTiming(checkpoint, new Date('2026-10-05T00:00:00Z')), /Overdue by 4 days/);
  assert.equal(describeTiming({ ...checkpoint, state: 'SATISFIED' }, now), 'Satisfied');
  assert.equal(describeTiming({ ...checkpoint, state: 'CANCELLED' }, now), 'Cancelled');
  for (const monitor of view.monitorPoints) {
    assert.doesNotMatch(describeTiming(monitor, now), /risk|health|attention|critical/i);
  }
  assert.equal(describeExpectation(target, view), 'Work Item expected to be completed');
  assert.equal(describeExpectation(checkpoint, view), '“Ship” expected to be completed for API');
  assert.equal(findActiveFinalTarget(view)?.id, 'm-2');
  assert.equal(findNextExpectation(view)?.id, 'm-1');
  assert.equal(findActiveFinalTarget({ ...view, monitorPoints: [] }), null);
});

test('the target form becomes a typed backend command with the chosen zone applied', () => {
  const input = buildMonitorInput(
    'wi-1',
    {
      kind: 'FINAL_TARGET',
      title: '  Ship  ',
      dueLocal: '2026-10-15T17:00',
      expectedLifecycle: 'COMPLETED',
    },
    'America/Chicago',
    -300,
  );
  assert.deepEqual(input, {
    workItemId: 'wi-1',
    kind: 'FINAL_TARGET',
    title: 'Ship',
    dueAt: '2026-10-15T22:00:00.000Z',
    timezone: 'America/Chicago',
    expectedWorkItemLifecycle: 'COMPLETED',
  });
  assert.throws(() =>
    buildMonitorInput(
      'wi-1',
      { kind: 'FINAL_TARGET', title: 'x', dueLocal: '', expectedLifecycle: 'COMPLETED' },
      'UTC',
      0,
    ),
  );
  assert.throws(() =>
    buildMonitorInput(
      'wi-1',
      {
        kind: 'FINAL_TARGET',
        title: ' ',
        dueLocal: '2026-10-15T17:00',
        expectedLifecycle: 'COMPLETED',
      },
      'UTC',
      0,
    ),
  );
  assert.deepEqual(Object.keys(input).sort(), [
    'dueAt',
    'expectedWorkItemLifecycle',
    'kind',
    'timezone',
    'title',
    'workItemId',
  ]);
});

test('a successful mutation runs once and then reloads the authoritative view', async () => {
  const calls: string[] = [];
  const outcome = await runTrackingMutation(
    async () => void calls.push('mutate'),
    async () => void calls.push('reload'),
  );
  assert.deepEqual(calls, ['mutate', 'reload']);
  assert.equal(outcome.status, 'ok');
});

test('a failed mutation does not reload and is reported as a mutation failure', async () => {
  const calls: string[] = [];
  const outcome = await runTrackingMutation(
    async () => {
      calls.push('mutate');
      throw new Error('rejected');
    },
    async () => void calls.push('reload'),
  );
  assert.deepEqual(calls, ['mutate']);
  assert.deepEqual(outcome, { status: 'mutationFailed', message: 'rejected' });
});

test('mutation success + refresh failure never resends the mutation', async () => {
  let mutations = 0;
  const outcome = await runTrackingMutation(
    async () => void (mutations += 1),
    async () => {
      throw new Error('view refresh failed');
    },
  );
  assert.equal(mutations, 1);
  assert.deepEqual(outcome, { status: 'refreshFailed', message: 'view refresh failed' });
});

test('production Workflow and Items surfaces expose no direct Tracking mutation controls', () => {
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  for (const marker of [
    'setStepState',
    'setMonitorState',
    'createTrackedItem',
    'createWorkflow',
    'createMonitor',
    'Complete “',
    'Set Target',
    'Add Tracked Item',
  ]) {
    assert.doesNotMatch(workspace + panels, new RegExp(marker), marker);
  }
  assert.match(panels, /Ask the AI Assistant/);
});

test('production Tracking has no prototype or fixture facts', () => {
  const dirs = ['tracking', 'api/tracking', 'components/workItems/tracking'];
  for (const dir of dirs) {
    for (const file of readdirSync(new URL(`../src/${dir}`, import.meta.url))) {
      const text = source(`${dir}/${file}`);
      assert.doesNotMatch(
        text,
        /createDemoWorkflowFixture|PROTOTYPE_|workItemPrototypeFixtures|mockData|MockWorkItemApi/,
        `${dir}/${file}`,
      );
    }
  }
  const page = source('pages/WorkItems.tsx');
  assert.doesNotMatch(
    page,
    /modelOverride=\{resolveModel\(selected\)\}/,
    'the workspace must not receive the unavailable/prototype model in production',
  );
});

test('React components never import generated Wails models', () => {
  for (const file of readdirSync(
    new URL('../src/components/workItems/tracking', import.meta.url),
  )) {
    const text = source(`components/workItems/tracking/${file}`);
    assert.doesNotMatch(text, /wailsjs/, file);
  }
  assert.doesNotMatch(source('components/workItems/workspace/WorkItemWorkspace.tsx'), /wailsjs/);
});

test('the workspace keeps its layout and honest states; M9 Ghost and lifecycle stay intact', () => {
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  for (const marker of [
    'WorkItem workspace sections',
    'Workflow</ToggleButton>',
    'History</ToggleButton>',
    'projectWorkItemGhost',
    'work-item-title-preview',
    'workItemLifecycleActions',
  ]) {
    assert.ok(workspace.includes(marker), marker);
  }
  assert.doesNotMatch(workspace, /Edit Info|WorkItemEditDialog/);
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  assert.match(panels, /Loading tracking data/);
  assert.match(panels, /no longer available/);
  assert.match(panels, /Tracking data could not be loaded/);
  assert.match(panels, /No tracked items yet/);
  assert.match(panels, /Workflow History/);
  assert.match(panels, /Selected Item Detail/);
});

test('the Assistant frontend never calls Tracking mutation APIs directly', () => {
  for (const file of [
    'api/assistant/assistantApi.ts',
    'api/assistant/wailsAssistantApi.ts',
    'assistant/assistantConversation.ts',
    'components/AssistantPanel.tsx',
    'proposals/proposalHandoff.ts',
  ]) {
    assert.doesNotMatch(source(file), /tracking|Tracking/, file);
  }
});

test('Items uses an in-page master/detail layout with compact selectable rows', () => {
  const panels = source('components/workItems/tracking/TrackingPanels.tsx');
  assert.match(panels, /gridTemplateColumns/);
  assert.match(panels, /Selected Item Detail/);
  assert.match(panels, /setSelectedId\(item\.id\)/);
  assert.doesNotMatch(panels, /Drawer/);
});
