import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createProductWorkItem,
  type ProductWorkItem,
  type ProductWorkItemApi,
  setProductWorkItemLifecycle,
} from '../src/api/workItems/productApi.ts';
import {
  groupWorkItemsByLifecycle,
  initialWorkItemsPageState,
  selectedWorkItem,
  workItemLifecycleActions,
  workItemsPageReducer,
} from '../src/pages/workItemsPageState.ts';
import { mapWorkItemDTO } from '../src/api/workItems/workItemDtoAdapter.ts';
import { buildWorkItemSurfaceModel } from '../src/presentation/workItems/workItemTruthBoundary.ts';
import { PROTOTYPE_WORKFLOW_MODELS } from '../src/components/workItems/fixtures/workItemPrototypeFixtures.ts';
import { readFileSync } from 'node:fs';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const backendItem: ProductWorkItem = {
  id: '0199-backend-generated',
  title: 'Production Release',
  description: 'Coordinate release readiness',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-18T12:00:00Z',
  updatedAt: '2026-09-18T12:00:00Z',
};

test('generated WorkItem DTO maps once into the authoritative frontend product model', () => {
  const mapped = mapWorkItemDTO({
    id: 'backend-id',
    title: 'Backend title',
    description: 'Backend description',
    lifecycleState: 'PAUSED',
    completedAt: undefined,
    createdAt: '2026-09-18T12:00:00Z',
    updatedAt: '2026-09-19T01:15:00Z',
  });

  assert.deepEqual(mapped, {
    id: 'backend-id',
    title: 'Backend title',
    description: 'Backend description',
    lifecycleState: 'PAUSED',
    completedAt: null,
    createdAt: '2026-09-18T12:00:00Z',
    updatedAt: '2026-09-19T01:15:00Z',
  });
});

test('production Work Items defaults to the Wails adapter and has no prototype truth fallback', () => {
  const source = sourceOf('../src/pages/WorkItems.tsx');
  assert.match(source, /api = productWorkItemApi/);
  assert.ok(!source.includes('prototypeWorkItemApi'));
  assert.ok(!source.includes('PROTOTYPE_WORKFLOW_MODELS'));
  assert.ok(!source.includes('getPrototypeWorkflowModel'));
});

test('prototype workflow metadata cannot override authoritative WorkItem shell fields', () => {
  const prototype = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const surface = buildWorkItemSurfaceModel(backendItem, prototype);

  assert.equal(surface.authoritative, backendItem);
  assert.equal(surface.workflowOwnership, 'PROTOTYPE_FUTURE_SUBSYSTEMS');
  assert.equal(surface.workflow.workItemId, backendItem.id);
  assert.equal(surface.workflow.title, backendItem.title);
  assert.equal(surface.workflow.description, backendItem.description);
  assert.notEqual(surface.workflow.title, prototype.title);
  assert.equal(surface.authoritative.lifecycleState, 'ACTIVE');
  assert.equal(surface.authoritative.updatedAt, '2026-09-18T12:00:00Z');
});

test('production surface marks unavailable future-subsystem visualization explicitly', () => {
  const surface = buildWorkItemSurfaceModel(backendItem);
  assert.equal(surface.workflowOwnership, 'FUTURE_SUBSYSTEMS_UNAVAILABLE');
  assert.equal(surface.workflow.nodes.length, 0);
  assert.equal(surface.workflow.edges.length, 0);
  assert.equal(surface.workflow.hasAuthoritativeHealth, false);
  assert.equal(surface.workflow.targetDate, null);
});

test('React WorkItem surfaces depend on the product model rather than sqlc or domain models', () => {
  for (const relativePath of [
    '../src/pages/WorkItems.tsx',
    '../src/components/workItems/WorkItemRow.tsx',
    '../src/components/workItems/workspace/WorkItemWorkspace.tsx',
  ]) {
    const source = sourceOf(relativePath);
    assert.ok(!source.includes('internal/database/generated'));
    assert.ok(!source.includes('internal/workitem'));
    assert.ok(!source.includes('wailsjs/go/models'));
  }
});

test('Dashboard roadmap uses authoritative truth while prototype rows remain isolated for visual tests', () => {
  const dashboard = sourceOf('../src/pages/Dashboard.tsx');
  const adapter = sourceOf(
    '../src/components/dashboard/activeWorkflowsRoadmap/roadmapPrototypeAdapter.ts',
  );
  assert.ok(dashboard.includes('buildAuthoritativeRoadmapRows'));
  assert.ok(!dashboard.includes('buildPrototypeRoadmapRows'));
  assert.ok(!dashboard.includes('roadmapPrototypeAdapter'));
  assert.ok(adapter.includes('frontend PROTOTYPE data adapter'));
  assert.ok(!adapter.includes('wailsjs'));
});

function apiWith(overrides: Partial<ProductWorkItemApi>): ProductWorkItemApi {
  return {
    list: async () => [],
    create: async () => {
      throw new Error('create should not be called');
    },
    setLifecycle: async () => {
      throw new Error('setLifecycle should not be called');
    },
    ...overrides,
  };
}

test('Work Items page starts in an explicit loading state', () => {
  assert.equal(initialWorkItemsPageState.status, 'loading');
  assert.deepEqual(initialWorkItemsPageState.items, []);
});

test('backend list success becomes the authoritative visible collection', () => {
  const state = workItemsPageReducer(initialWorkItemsPageState, {
    type: 'loadSucceeded',
    items: [backendItem],
  });
  assert.equal(state.status, 'ready');
  assert.deepEqual(state.items, [backendItem]);
});

test('an empty backend list produces a ready empty state', () => {
  const state = workItemsPageReducer(initialWorkItemsPageState, {
    type: 'loadSucceeded',
    items: [],
  });
  assert.equal(state.status, 'ready');
  assert.equal(state.items.length, 0);
});

test('backend read error is visible and never falls back to demo WorkItems', () => {
  const state = workItemsPageReducer(
    { ...initialWorkItemsPageState, items: [backendItem] },
    { type: 'loadFailed', message: 'database unavailable' },
  );
  assert.equal(state.status, 'error');
  assert.equal(state.readError, 'database unavailable');
  assert.deepEqual(state.items, []);
});

test('valid create calls the mutation abstraction with user-authored fields only', async () => {
  let request: unknown;
  const api = apiWith({
    create: async (input) => {
      request = input;
      return backendItem;
    },
  });
  const result = await createProductWorkItem(api, {
    title: '  Production Release  ',
    description: '  Coordinate release readiness  ',
  });
  assert.deepEqual(request, {
    title: 'Production Release',
    description: 'Coordinate release readiness',
  });
  assert.equal(Object.hasOwn(request as object, 'id'), false);
  assert.equal(Object.hasOwn(request as object, 'lifecycleState'), false);
  assert.equal(result, backendItem);
});

test('blank title is rejected before frontend submits', async () => {
  let calls = 0;
  const api = apiWith({
    create: async () => {
      calls += 1;
      return backendItem;
    },
  });
  await assert.rejects(() => createProductWorkItem(api, { title: '  \t ' }), /title is required/i);
  assert.equal(calls, 0);
});

test('create success displays the authoritative backend result, not user input', () => {
  const state = workItemsPageReducer(
    { ...initialWorkItemsPageState, status: 'ready', submitting: true },
    { type: 'createSucceeded', item: backendItem },
  );
  assert.deepEqual(state.items, [backendItem]);
  assert.equal(state.items[0].id, '0199-backend-generated');
  assert.equal(state.items[0].lifecycleState, 'ACTIVE');
  assert.equal(state.submitting, false);
});

test('create backend failure remains a visible create error', async () => {
  const api = apiWith({
    create: async () => {
      throw new Error('create WorkItem: persistence failed');
    },
  });
  await assert.rejects(
    () => createProductWorkItem(api, { title: 'Production Release' }),
    /persistence failed/,
  );
  const state = workItemsPageReducer(
    { ...initialWorkItemsPageState, status: 'ready', submitting: true },
    { type: 'createFailed', message: 'create WorkItem: persistence failed' },
  );
  assert.equal(state.createError, 'create WorkItem: persistence failed');
  assert.equal(state.submitting, false);
});

test('selecting a WorkItem opens the correct authoritative detail fields', () => {
  const state = workItemsPageReducer(
    { ...initialWorkItemsPageState, status: 'ready', items: [backendItem] },
    { type: 'selected', id: backendItem.id },
  );
  const detail = selectedWorkItem(state);
  assert.equal(detail, backendItem);
  assert.equal(detail?.title, 'Production Release');
  assert.equal(detail?.description, 'Coordinate release readiness');
  assert.equal(detail?.lifecycleState, 'ACTIVE');
  assert.equal(detail?.createdAt, '2026-09-18T12:00:00Z');
  assert.equal(detail?.updatedAt, '2026-09-18T12:00:00Z');
});

test('production WorkItem surface exposes no free-form detail edit path', () => {
  const page = sourceOf('../src/pages/WorkItems.tsx');
  const workspace = sourceOf('../src/components/workItems/workspace/WorkItemWorkspace.tsx');
  const productApi = sourceOf('../src/api/workItems/productApi.ts');
  const wailsAdapter = sourceOf('../src/api/workItems/wailsProductWorkItemApi.ts');
  assert.doesNotMatch(`${page}\n${workspace}`, /Edit Info|WorkItemEditDialog|onSaveEdit/);
  assert.doesNotMatch(productApi, /UpdateProductWorkItemInput|updateProductWorkItem|update\(/);
  assert.doesNotMatch(wailsAdapter, /UpdateWorkItem|async update/);
});

test('generated Wails surface exposes no direct WorkItem details update', () => {
  const declarations = sourceOf('../wailsjs/go/app/App.d.ts');
  const bindings = sourceOf('../wailsjs/go/app/App.js');
  const models = sourceOf('../wailsjs/go/models.ts');
  assert.doesNotMatch(`${declarations}\n${bindings}`, /UpdateWorkItem/);
  assert.doesNotMatch(models, /class UpdateWorkItemRequestDTO/);
});

test('lifecycle actions expose only valid product behaviors for each state', () => {
  assert.deepEqual(workItemLifecycleActions('ACTIVE'), [
    { label: 'Pause', target: 'PAUSED' },
    { label: 'Complete', target: 'COMPLETED' },
    { label: 'Cancel', target: 'CANCELLED' },
  ]);
  assert.deepEqual(workItemLifecycleActions('PAUSED'), [
    { label: 'Resume', target: 'ACTIVE' },
    { label: 'Complete', target: 'COMPLETED' },
    { label: 'Cancel', target: 'CANCELLED' },
  ]);
  assert.deepEqual(workItemLifecycleActions('COMPLETED'), [{ label: 'Reopen', target: 'ACTIVE' }]);
  assert.deepEqual(workItemLifecycleActions('CANCELLED'), [{ label: 'Reopen', target: 'ACTIVE' }]);
});

test('lifecycle mutation calls adapter with ID and target only', async () => {
  const committed: ProductWorkItem = {
    ...backendItem,
    lifecycleState: 'PAUSED',
    updatedAt: '2026-09-18T13:00:00Z',
  };
  let request: unknown;
  const api = apiWith({
    setLifecycle: async (input) => {
      request = input;
      return committed;
    },
  });
  const result = await setProductWorkItemLifecycle(api, {
    id: `  ${backendItem.id}  `,
    targetLifecycleState: 'PAUSED',
  });
  assert.deepEqual(request, { id: backendItem.id, targetLifecycleState: 'PAUSED' });
  assert.equal(Object.hasOwn(request as object, 'completedAt'), false);
  assert.equal(Object.hasOwn(request as object, 'updatedAt'), false);
  assert.equal(result, committed);
});

test('lifecycle success updates list and selected detail from the committed DTO', () => {
  const committed: ProductWorkItem = {
    ...backendItem,
    lifecycleState: 'COMPLETED',
    completedAt: '2026-09-18T13:00:00Z',
    updatedAt: '2026-09-18T13:00:00Z',
  };
  const state = workItemsPageReducer(
    {
      ...initialWorkItemsPageState,
      status: 'ready',
      items: [backendItem],
      selectedWorkItemId: backendItem.id,
      lifecycleSaving: true,
    },
    { type: 'lifecycleSucceeded', item: committed },
  );
  assert.equal(state.items[0], committed);
  assert.equal(selectedWorkItem(state), committed);
  assert.equal(state.lifecycleSaving, false);
});

test('lifecycle failure preserves authoritative state and exposes the error', () => {
  const state = workItemsPageReducer(
    {
      ...initialWorkItemsPageState,
      status: 'ready',
      items: [backendItem],
      selectedWorkItemId: backendItem.id,
      lifecycleSaving: true,
    },
    { type: 'lifecycleFailed', message: 'transition rejected' },
  );
  assert.equal(state.items[0], backendItem);
  assert.equal(selectedWorkItem(state), backendItem);
  assert.equal(state.lifecycleError, 'transition rejected');
  assert.equal(state.lifecycleSaving, false);
});

test('pending lifecycle mutation prevents duplicate lifecycle commands', () => {
  const base = {
    ...initialWorkItemsPageState,
    status: 'ready' as const,
    items: [backendItem],
    selectedWorkItemId: backendItem.id,
  };
  const pending = workItemsPageReducer(base, { type: 'lifecycleStarted' });
  assert.equal(pending.lifecycleSaving, true);
  assert.equal(workItemsPageReducer(pending, { type: 'lifecycleStarted' }), pending);
});

test('lifecycle-aware list keeps active and paused current while closed states are secondary', () => {
  const paused = { ...backendItem, id: 'paused', lifecycleState: 'PAUSED' as const };
  const completed = { ...backendItem, id: 'completed', lifecycleState: 'COMPLETED' as const };
  const cancelled = { ...backendItem, id: 'cancelled', lifecycleState: 'CANCELLED' as const };
  const groups = groupWorkItemsByLifecycle([backendItem, paused, completed, cancelled]);
  assert.deepEqual(
    groups.current.map((item) => item.id),
    [backendItem.id, 'paused'],
  );
  assert.deepEqual(
    groups.closed.map((item) => item.id),
    ['completed', 'cancelled'],
  );
});
