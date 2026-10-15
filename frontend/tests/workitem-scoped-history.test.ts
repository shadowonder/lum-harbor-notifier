import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type { HistoryEventDto, HistoryEventListDto } from '../src/api/history/contracts.ts';
import { loadWorkItemHistory } from '../src/history/workItemHistoryLoad.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const event = (
  id: string,
  workItemId: string,
  extra: Partial<HistoryEventDto> = {},
): HistoryEventDto => ({
  id,
  occurredAt: '2026-09-20T12:00:00Z',
  eventType: 'WORK_ITEM_CREATED',
  scope: 'workItems',
  canonicalScope: 'WORK_ITEM',
  actor: { label: 'User', kind: 'user' },
  subject: { id: workItemId, type: 'WORK_ITEM', label: 'x' },
  workItemId,
  title: 'Created',
  summary: 'Created',
  meaningful: true,
  ...extra,
});

// A fake authoritative backend keyed strictly by workItemId (never by text).
function fakeApi(byId: Record<string, HistoryEventDto[]>, truncated = false) {
  const calls: string[] = [];
  return {
    calls,
    api: {
      async listWorkItemEvents(id: string): Promise<HistoryEventListDto> {
        calls.push(id);
        return { asOf: '2026-09-20T13:00:00Z', events: byId[id] ?? [], truncated };
      },
    },
  };
}

test('scoped load asks the backend by authoritative workItemId and returns only that WorkItem', async () => {
  const { api, calls } = fakeApi({
    a: [
      event('a2', 'a', { eventType: 'MONITOR_CREATED', canonicalScope: 'TRACKING' }),
      event('a1', 'a'),
    ],
    b: [event('b1', 'b')],
  });
  const state = await loadWorkItemHistory(api, 'a');
  assert.deepEqual(calls, ['a']);
  assert.equal(state.status, 'ready');
  if (state.status !== 'ready') return;
  assert.deepEqual(
    state.events.map((e) => e.id),
    ['a2', 'a1'],
  );
  assert.ok(state.events.every((e) => e.workItemId === 'a'));
});

test('a WorkItem with no linked events is a valid empty page, not an error', async () => {
  const state = await loadWorkItemHistory(fakeApi({}).api, 'none');
  assert.equal(state.status, 'ready');
  if (state.status === 'ready') assert.equal(state.events.length, 0);
});

test('a failing query is an error state with no fixture fallback', async () => {
  const state = await loadWorkItemHistory(
    {
      async listWorkItemEvents() {
        throw new Error('boom');
      },
    },
    'a',
  );
  assert.deepEqual(state, { status: 'error' });
});

test('backend truncation is preserved honestly', async () => {
  const state = await loadWorkItemHistory(fakeApi({ a: [event('a1', 'a')] }, true).api, 'a');
  assert.equal(state.status === 'ready' && state.truncated, true);
});

test('provenance fields pass through unchanged; absent fields stay absent (no inferred causation)', async () => {
  const state = await loadWorkItemHistory(
    fakeApi({ a: [event('a1', 'a', { proposalId: 'p-1' })] }).api,
    'a',
  );
  if (state.status !== 'ready') throw new Error('expected ready');
  assert.equal(state.events[0].proposalId, 'p-1');
  assert.equal(state.events[0].causationId, undefined);
  assert.equal(state.events[0].aiRunId, undefined);
});

test('the Workspace History tab uses scoped M12 History for real WorkItems, with closed items included', () => {
  const workspace = source('components/workItems/workspace/WorkItemWorkspace.tsx');
  assert.match(
    workspace,
    /realTracking && activeTab === 'history' && <WorkItemScopedHistory workItemId=\{item\.id\}/,
  );
  // no lifecycle gate on the History tab: terminal WorkItems keep their durable explanation
  assert.doesNotMatch(workspace, /lifecycleState[^\n]*activeTab === 'history'/);
});

test('scoped History reuses the shared timeline, hides redundant WorkItem navigation and adds no second subsystem', () => {
  const tab = source('components/workItems/workspace/WorkItemScopedHistory.tsx');
  const timeline = source('components/history/HistoryTimeline.tsx');
  const api = source('api/history/wailsHistoryApi.ts');
  assert.match(tab, /HistoryTimeline/);
  assert.match(tab, /showWorkItem=\{false\}/);
  assert.equal(/onOpenWorkItem|onViewTrace/.test(tab), false);
  assert.match(timeline, /onViewTrace && event\.traceId/);
  assert.match(api, /QueryWorkItemHistory\(workItemId\)/);
  assert.equal(
    /title|correlation/i.test(
      source('history/workItemHistoryLoad.ts').replace(/\/\*[\s\S]*?\*\//g, ''),
    ),
    false,
  );
  assert.match(tab, /History could not be loaded\./);
  assert.match(tab, /No History has been recorded for this Work Item\./);
});

test('the prototype History API is gone and the global page shares the same timeline component', () => {
  assert.throws(() => source('api/history/mockHistoryApi.ts'));
  assert.match(source('pages/History.tsx'), /HistoryTimeline/);
});
