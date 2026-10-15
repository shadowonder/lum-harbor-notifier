import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { mapWorkflow } from '../src/api/tracking/trackingDtoAdapter.ts';
import {
  defaultScheduleActionDueDate,
  reminderOffsetsForPolicy,
  scheduleActionDue,
  scheduleActionWorkflowStep,
} from '../src/components/workItems/workspace/scheduleAction.ts';

test('Schedule Action default due date is the local calendar day, not the UTC day', () => {
  // 23:30 local time: in any timezone behind UTC this instant is already tomorrow in UTC.
  const lateEvening = new Date(2026, 8, 25, 23, 30);
  assert.equal(defaultScheduleActionDueDate(lateEvening), '2026-09-25');
  const earlyMorning = new Date(2026, 8, 25, 0, 15);
  assert.equal(defaultScheduleActionDueDate(earlyMorning), '2026-09-25');
});

test('Schedule Action reminder presets map to backend threshold offsets', () => {
  assert.deepEqual(reminderOffsetsForPolicy('standard'), [259200, 172800, 86400]);
  assert.deepEqual(reminderOffsetsForPolicy('urgent'), [7200, 1800]);
  assert.deepEqual(reminderOffsetsForPolicy('near'), [900]);
  assert.deepEqual(reminderOffsetsForPolicy('1d'), [86400]);
  assert.deepEqual(reminderOffsetsForPolicy('due'), [0]);
  assert.equal(reminderOffsetsForPolicy('none'), undefined);
});

test('Schedule Action sends exactly one of dueAt / dueDate (backend rejects both)', () => {
  assert.deepEqual(scheduleActionDue('', ''), {});
  assert.deepEqual(scheduleActionDue('2026-09-25', ''), { dueDate: '2026-09-25' });

  const timed = scheduleActionDue('2026-09-25', '19:45');
  assert.deepEqual(Object.keys(timed), ['dueAt']);
  assert.equal(timed.dueAt, new Date(2026, 8, 25, 19, 45).toISOString());
});

test('Schedule Action Workflow Step provenance is the Workflow current step, not TrackedItem progression', () => {
  const steps = [
    { id: 'implement', key: 'IMPLEMENT', title: 'Implement', sequence: 3, terminal: false },
    { id: 'verify', key: 'VERIFY', title: 'Verify', sequence: 4, terminal: false },
  ];
  const workflow = {
    id: 'wf',
    workItemId: 'wi',
    workflowDefinitionId: 'def',
    createdAt: '2026-09-01T09:00:00Z',
    currentStepId: 'verify',
    definition: { id: 'def', name: 'Delivery Flow', version: 1, active: true, createdAt: '2026-09-01T09:00:00Z', steps },
    name: 'Delivery Flow',
    version: 1,
    steps,
  };
  assert.equal(scheduleActionWorkflowStep({ workflow })?.title, 'Verify');
  assert.equal(scheduleActionWorkflowStep({ workflow: { ...workflow, currentStepId: null } }), null);
  assert.equal(scheduleActionWorkflowStep({ workflow: null }), null);
  assert.equal(scheduleActionWorkflowStep(null), null);
});

test('Tracking adapter keeps the Workflow current step from the Wails DTO', () => {
  const step = { id: 'verify', key: 'VERIFY', title: 'Verify', sequence: 4, terminal: false };
  const mapped = mapWorkflow({
    id: 'wf',
    workItemId: 'wi',
    workflowDefinitionId: 'def',
    startedAt: '2026-09-01T09:00:00Z',
    currentStep: step,
    createdAt: '2026-09-01T09:00:00Z',
    updatedAt: '2026-09-01T09:00:00Z',
    definition: { id: 'def', name: 'Delivery Flow', version: 1, active: true, createdAt: '2026-09-01T09:00:00Z', steps: [step] },
  } as unknown as Parameters<typeof mapWorkflow>[0]);
  assert.equal(mapped.currentStepId, 'verify');
});

test('Schedule Action can carry one exact optional TrackedItem relation', () => {
  const source = readFileSync(
    new URL('../src/components/workItems/workspace/WorkItemWorkspace.tsx', import.meta.url),
    'utf8',
  );
  assert.match(source, /label="Tracked Item"/);
  assert.match(source, /trackedItemId: todoTrackedItemId \|\| undefined/);
});
