import assert from 'node:assert/strict';
import test from 'node:test';
import type { app } from '../wailsjs/go/models.ts';
import {
  loadDashboardView,
  mapDashboardViewDTO,
  type DashboardViewApi,
  type DashboardViewRequest,
} from '../src/api/dashboardApi.ts';
import {
  consumeWorkItemNavigationIntent,
  createWorkItemNavigationIntent,
} from '../src/navigation/workItemNavigation.ts';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';

const request: DashboardViewRequest = {
  rangeStart: '2026-09-18',
  rangeEnd: '2026-09-19',
  today: '2026-09-18',
  timezone: 'America/Chicago',
};

function dashboardDTO(
  currentItems: Array<Record<string, unknown>> = [],
  allCurrentItems: Array<Record<string, unknown>> = currentItems,
) {
  return {
    planner: {
      calendarEvents: [],
      calendarItems: [],
      currentOpenItems: [
        {
          id: 'planner-current',
          type: 'TODO',
          title: 'Authoritative open task',
          status: 'OPEN',
          authority: 'LOCAL',
          contextStatus: 'COMPLETE',
          missingContext: [],
        },
      ],
      completedItems: [],
      completedTodayItems: [],
    },
    workItems: {
      currentCount: currentItems.length,
      pausedCount: currentItems.filter((item) => item.lifecycleState === 'PAUSED').length,
      completedCount: 3,
      cancelledCount: 1,
      closedCount: 4,
      currentItems,
      allCurrentItems,
    },
  } as unknown as app.DashboardViewDTO;
}

test('Dashboard DTO maps authoritative Planner and compact WorkItem summary', () => {
  const data = mapDashboardViewDTO(
    dashboardDTO([
      {
        id: 'active',
        title: 'Active release',
        description: 'Ship the authoritative release',
        lifecycleState: 'ACTIVE',
        updatedAt: '2026-09-18T12:00:00Z',
      },
      {
        id: 'paused',
        title: 'Paused migration',
        description: null,
        lifecycleState: 'PAUSED',
        updatedAt: '2026-09-18T13:00:00Z',
      },
    ]),
  );
  assert.equal(data.planner.currentOpenItems[0].title, 'Authoritative open task');
  assert.equal(data.workItems.currentCount, 2);
  assert.equal(data.workItems.pausedCount, 1);
  assert.equal(data.workItems.closedCount, 4);
  assert.deepEqual(
    data.workItems.currentItems.map((item) => [item.id, item.lifecycleState]),
    [
      ['active', 'ACTIVE'],
      ['paused', 'PAUSED'],
    ],
  );
  assert.equal(data.workItems.allCurrentItems[0]?.description, 'Ship the authoritative release');
});

test('Dashboard maps the complete current projection independently of the bounded Current Work list', () => {
  const currentItems = [
    {
      id: 'newest',
      title: 'Newest',
      description: null,
      lifecycleState: 'ACTIVE',
      updatedAt: '2026-09-18T15:00:00Z',
    },
  ];
  const older = {
    id: 'older',
    title: 'Older paused work',
    description: 'Still belongs in the roadmap',
    lifecycleState: 'PAUSED',
    updatedAt: '2026-09-17T12:00:00Z',
  };
  const data = mapDashboardViewDTO(dashboardDTO(currentItems, [...currentItems, older]));
  assert.deepEqual(
    data.workItems.currentItems.map((item) => item.id),
    ['newest'],
  );
  assert.deepEqual(
    data.workItems.allCurrentItems.map((item) => item.id),
    ['newest', 'older'],
  );
});

test('Dashboard empty WorkItem summary stays empty and never invents mock content', () => {
  const data = mapDashboardViewDTO(dashboardDTO());
  assert.equal(data.workItems.currentCount, 0);
  assert.deepEqual(data.workItems.currentItems, []);
  assert.deepEqual(data.workItems.allCurrentItems, []);
});

test('Dashboard loader uses only its authoritative API and propagates errors without fallback', async () => {
  let received: DashboardViewRequest | null = null;
  const expected = mapDashboardViewDTO(dashboardDTO());
  const api: DashboardViewApi = {
    async get(input) {
      received = input;
      return expected;
    },
  };
  assert.equal(await loadDashboardView(api, request), expected);
  assert.deepEqual(received, request);

  await assert.rejects(
    () =>
      loadDashboardView(
        {
          async get() {
            throw new Error('dashboard database unavailable');
          },
        },
        request,
      ),
    /database unavailable/,
  );
});

test('WorkItem navigation retains identity until authoritative collection resolves, then consumes', () => {
  const item: ProductWorkItem = {
    id: 'target-work-item',
    title: 'Target',
    description: null,
    lifecycleState: 'ACTIVE',
    completedAt: null,
    createdAt: '2026-09-18T12:00:00Z',
    updatedAt: '2026-09-18T12:00:00Z',
  };
  const intent = createWorkItemNavigationIntent(item.id);
  assert.equal(intent.workItemId, item.id);

  let selected: string | null = null;
  let consumed = 0;
  consumeWorkItemNavigationIntent(
    intent,
    [item],
    (id) => {
      selected = id;
    },
    () => {
      consumed += 1;
    },
  );
  assert.equal(selected, item.id);
  assert.equal(consumed, 1);
});

test('missing WorkItem navigation target clears intent without selecting fake state', () => {
  let selected = false;
  let consumed = false;
  consumeWorkItemNavigationIntent(
    createWorkItemNavigationIntent('missing'),
    [],
    () => {
      selected = true;
    },
    () => {
      consumed = true;
    },
  );
  assert.equal(selected, false);
  assert.equal(consumed, true);
});
