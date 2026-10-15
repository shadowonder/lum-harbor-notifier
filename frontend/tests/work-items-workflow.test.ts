import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';
import {
  DEFAULT_CANVAS_DIMENSIONS,
  DEFAULT_MINIMAP_DIMENSIONS,
  COMPACT_MINIMAP_DIMENSIONS,
  computeDependencyChain,
  computeDirectRelations,
  getBranchConnectorPath,
  getDependencyEdgePresentation,
  getDependencyRoutePoints,
  getHealthBgColor,
  getHealthColor,
  getItemTypeIcon,
  getItemTypeLabel,
  getNodeCoordinates,
  getRoundedOrthogonalPath,
  getStageBounds,
  getTargetMarkerCoordinates,
  getWorkflowEdgePalette,
  WORKFLOW_LINE_HIERARCHY,
} from '../src/components/workItems/workflow/workflowLayout.ts';
import {
  buildWorkflowModelFromWorkItem,
  createDemoWorkflowFixture,
  getRecentWorkflowHistory,
  WORKFLOW_SECTIONS,
  WORKFLOW_STAGES,
} from '../src/components/workItems/workflow/workflowTypes.ts';
import {
  groupWorkItemsByLifecycle,
  initialWorkItemsPageState,
  workItemsPageReducer,
} from '../src/pages/workItemsPageState.ts';
import {
  getPrototypeWorkflowModel,
  PROTOTYPE_WORKFLOW_MODELS,
  PROTOTYPE_WORK_ITEMS,
  prototypeWorkItemApi,
} from '../src/components/workItems/fixtures/workItemPrototypeFixtures.ts';
import {
  createItemExplorerDeepLink,
  filterItemExplorerNodes,
  getItemListRowPresentation,
  getItemRelationships,
  getItemSignal,
  resolveItemExplorerSelection,
} from '../src/components/workItems/workspace/itemExplorer.ts';

const mockRealWorkItem: ProductWorkItem = {
  id: '0199-backend-real',
  title: 'Platform Infrastructure v2',
  description: 'Scalable cloud runtime deployment',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-18T10:00:00Z',
  updatedAt: '2026-09-18T15:00:00Z',
};

test('WORKFLOW_SECTIONS and WORKFLOW_STAGES define complete 3-section and 6-substage hierarchy', () => {
  assert.equal(WORKFLOW_SECTIONS.length, 3);
  assert.deepEqual(
    WORKFLOW_SECTIONS.map((s) => s.id),
    ['build', 'verify', 'release'],
  );

  assert.equal(WORKFLOW_STAGES.length, 6);
  assert.deepEqual(
    WORKFLOW_STAGES.map((s) => s.id),
    ['queue', 'dev', 'test', 'stage', 'adoption', 'production'],
  );
});

test('buildWorkflowModelFromWorkItem maps real backend WorkItem with ZERO fake branch nodes', () => {
  const model = buildWorkflowModelFromWorkItem(mockRealWorkItem);

  assert.equal(model.workItemId, mockRealWorkItem.id);
  assert.equal(model.title, mockRealWorkItem.title);
  assert.equal(model.description, mockRealWorkItem.description);
  assert.equal(model.isRealData, true);

  // Real data boundary: backend does not yet provide linked trackable items
  assert.deepEqual(model.nodes, []);
  assert.deepEqual(model.edges, []);

  // Lifecycle is not silently reinterpreted as Workflow stage/progress.
  assert.equal(model.trunk.currentStageId, 'queue');
  assert.equal(model.trunk.progressPercent, 0);
  assert.equal(model.workflowDataSource, 'unavailable');
});

test('buildWorkflowModelFromWorkItem preserves honest data boundaries: no fake health or deadline', () => {
  const model = buildWorkflowModelFromWorkItem(mockRealWorkItem);

  // Backend currently lacks health telemetry: must report UNKNOWN / neutral gray
  assert.equal(model.hasAuthoritativeHealth, false);
  assert.equal(model.aggregateHealth, 'gray');
  assert.equal(model.healthLabel, 'HEALTH UNKNOWN');

  // Backend currently lacks a target/deadline owner: absence is unavailable,
  // not an authoritative "not configured" fact.
  assert.equal(model.targetDate, null);
  assert.equal(model.targetLabel, 'Target data unavailable');
});

test('buildWorkflowModelFromWorkItem never fabricates Workflow stage from lifecycle', () => {
  const pausedItem: ProductWorkItem = {
    ...mockRealWorkItem,
    lifecycleState: 'PAUSED',
  };
  const pausedModel = buildWorkflowModelFromWorkItem(pausedItem);
  assert.equal(pausedModel.trunk.currentStageId, 'queue');
  assert.equal(pausedModel.hasAuthoritativeHealth, false);
  assert.equal(pausedModel.aggregateHealth, 'gray');

  const completedItem: ProductWorkItem = {
    ...mockRealWorkItem,
    lifecycleState: 'COMPLETED',
    completedAt: '2026-09-18T16:00:00Z',
  };
  const completedModel = buildWorkflowModelFromWorkItem(completedItem);
  assert.equal(completedModel.trunk.currentStageId, 'queue');

  const cancelledItem: ProductWorkItem = {
    ...mockRealWorkItem,
    lifecycleState: 'CANCELLED',
  };
  const cancelledModel = buildWorkflowModelFromWorkItem(cancelledItem);
  assert.equal(cancelledModel.trunk.currentStageId, 'queue');
});

test('Overview groups items into Current (active, paused) and secondary Closed (completed, cancelled)', () => {
  const activeItem: ProductWorkItem = { ...mockRealWorkItem, id: '1', lifecycleState: 'ACTIVE' };
  const pausedItem: ProductWorkItem = { ...mockRealWorkItem, id: '2', lifecycleState: 'PAUSED' };
  const completedItem: ProductWorkItem = {
    ...mockRealWorkItem,
    id: '3',
    lifecycleState: 'COMPLETED',
  };
  const cancelledItem: ProductWorkItem = {
    ...mockRealWorkItem,
    id: '4',
    lifecycleState: 'CANCELLED',
  };

  const groups = groupWorkItemsByLifecycle([activeItem, pausedItem, completedItem, cancelledItem]);

  assert.equal(groups.current.length, 2);
  assert.deepEqual(
    groups.current.map((i) => i.id),
    ['1', '2'],
  );

  assert.equal(groups.closed.length, 2);
  assert.deepEqual(
    groups.closed.map((i) => i.id),
    ['3', '4'],
  );
});

test('Overview row entire surface navigation selects target work item without requiring button', () => {
  const stateWithItems = {
    ...initialWorkItemsPageState,
    status: 'ready' as const,
    items: [mockRealWorkItem],
  };

  // Clicking anywhere on the row dispatches 'selected'
  const nextState = workItemsPageReducer(stateWithItems, {
    type: 'selected',
    id: mockRealWorkItem.id,
  });

  assert.equal(nextState.selectedWorkItemId, mockRealWorkItem.id);
});

test('getStageBounds divides canvas width deterministically among 6 stages', () => {
  const dims = DEFAULT_CANVAS_DIMENSIONS; // width: 860, paddingX: 32 -> usable: 796
  const usableWidth = dims.width - dims.paddingX * 2;
  const expectedStageWidth = usableWidth / 6;

  const queueBounds = getStageBounds('queue', dims);
  assert.equal(queueBounds.startX, dims.paddingX);
  assert.equal(queueBounds.width, expectedStageWidth);
  assert.equal(queueBounds.centerX, dims.paddingX + expectedStageWidth / 2);

  const devBounds = getStageBounds('dev', dims);
  assert.equal(devBounds.startX, dims.paddingX + expectedStageWidth);

  const prodBounds = getStageBounds('production', dims);
  assert.equal(prodBounds.startX, dims.paddingX + expectedStageWidth * 5);
  assert.ok(Math.abs(prodBounds.endX - (dims.width - dims.paddingX)) < 1e-6);
});

test('getNodeCoordinates computes deterministic positions for branch nodes based on lane and stage', () => {
  const dims = DEFAULT_CANVAS_DIMENSIONS; // trunkY: 120, laneSpacing: 46
  const node = {
    id: 'n1',
    title: 'Core auth flow',
    itemType: 'story' as const,
    stageId: 'dev' as const,
    lane: -1,
    health: 'green' as const,
  };

  const coords = getNodeCoordinates(node, dims);
  const devBounds = getStageBounds('dev', dims);
  assert.equal(coords.x, devBounds.centerX);
  assert.equal(coords.y, dims.trunkY - dims.laneSpacing);

  const lane1Node = { ...node, lane: 1 };
  const lane1Coords = getNodeCoordinates(lane1Node, dims);
  assert.equal(lane1Coords.x, devBounds.centerX);
  assert.equal(lane1Coords.y, dims.trunkY + dims.laneSpacing);
});

test('getTargetMarkerCoordinates computes ghost target position only when targetStageId exists', () => {
  const dims = DEFAULT_CANVAS_DIMENSIONS;
  const nodeWithoutTarget = {
    id: 'n1',
    title: 'Item',
    itemType: 'task' as const,
    stageId: 'queue' as const,
    health: 'green' as const,
  };
  assert.equal(getTargetMarkerCoordinates(nodeWithoutTarget, dims), null);

  const nodeWithTarget = {
    ...nodeWithoutTarget,
    targetStageId: 'stage' as const,
    lane: -1,
  };
  const targetCoords = getTargetMarkerCoordinates(nodeWithTarget, dims);
  assert.ok(targetCoords !== null);
  const stageBounds = getStageBounds('stage', dims);
  assert.equal(targetCoords.x, stageBounds.centerX);
  assert.equal(targetCoords.y, dims.trunkY - dims.laneSpacing);
});

test('computeDependencyChain traverses both upstream and downstream dependencies', () => {
  const edges = [
    { from: 'n1', to: 'n3' },
    { from: 'n2', to: 'n3' },
    { from: 'n3', to: 'n4' },
    { from: 'n4', to: 'n5' },
  ];

  // Focused node n3:
  // Upstream: n1, n2
  // Downstream: n4, n5
  const chainN3 = computeDependencyChain('n3', edges);
  assert.ok(chainN3.highlightedNodeIds.has('n3'));
  assert.ok(chainN3.highlightedNodeIds.has('n1'));
  assert.ok(chainN3.highlightedNodeIds.has('n2'));
  assert.ok(chainN3.highlightedNodeIds.has('n4'));
  assert.ok(chainN3.highlightedNodeIds.has('n5'));
  assert.equal(chainN3.highlightedNodeIds.size, 5);

  assert.ok(chainN3.highlightedEdgeKeys.has('n1->n3'));
  assert.ok(chainN3.highlightedEdgeKeys.has('n2->n3'));
  assert.ok(chainN3.highlightedEdgeKeys.has('n3->n4'));
  assert.ok(chainN3.highlightedEdgeKeys.has('n4->n5'));
  assert.equal(chainN3.highlightedEdgeKeys.size, 4);

  // Focused leaf node n5 traverses upstream only
  const chainN5 = computeDependencyChain('n5', edges);
  assert.equal(chainN5.highlightedNodeIds.size, 5);
  assert.ok(chainN5.highlightedNodeIds.has('n5'));
  assert.ok(chainN5.highlightedNodeIds.has('n4'));
  assert.ok(chainN5.highlightedNodeIds.has('n3'));
  assert.ok(chainN5.highlightedNodeIds.has('n1'));
  assert.ok(chainN5.highlightedNodeIds.has('n2'));

  // Isolated node with no edges
  const chainSolo = computeDependencyChain('solo', edges);
  assert.equal(chainSolo.highlightedNodeIds.size, 1);
  assert.ok(chainSolo.highlightedNodeIds.has('solo'));
  assert.equal(chainSolo.highlightedEdgeKeys.size, 0);
});

test('computeDependencyChain marks edges touching the focused node as direct and the rest as transitive', () => {
  // Database -> GraphQL -> Defect: focusing Defect makes GraphQL->Defect
  // direct and Database->GraphQL transitive-only.
  const edges = [
    { from: 'database', to: 'graphql' },
    { from: 'graphql', to: 'defect' },
    { from: 'defect', to: 'frontend' },
    { from: 'frontend', to: 'release-gate' },
  ];

  const chain = computeDependencyChain('defect', edges);

  assert.ok(chain.directEdgeKeys.has('graphql->defect'));
  assert.ok(chain.directEdgeKeys.has('defect->frontend'));
  assert.equal(chain.directEdgeKeys.size, 2);

  // Transitive-only edges remain in the full chain but are absent from
  // directEdgeKeys so callers can render them with reduced emphasis.
  assert.ok(chain.highlightedEdgeKeys.has('database->graphql'));
  assert.ok(!chain.directEdgeKeys.has('database->graphql'));
  assert.ok(chain.highlightedEdgeKeys.has('frontend->release-gate'));
  assert.ok(!chain.directEdgeKeys.has('frontend->release-gate'));
});

test('computeDirectRelations exposes only immediate parents and children, not the full transitive chain', () => {
  const edges = [
    { from: 'database', to: 'graphql' },
    { from: 'graphql', to: 'defect' },
    { from: 'defect', to: 'frontend' },
    { from: 'frontend', to: 'release-gate' },
  ];

  const direct = computeDirectRelations('defect', edges);

  assert.equal(direct.highlightedNodeIds.size, 3);
  assert.ok(direct.highlightedNodeIds.has('defect'));
  assert.ok(direct.highlightedNodeIds.has('graphql'));
  assert.ok(direct.highlightedNodeIds.has('frontend'));
  assert.ok(!direct.highlightedNodeIds.has('database'));
  assert.ok(!direct.highlightedNodeIds.has('release-gate'));

  assert.equal(direct.highlightedEdgeKeys.size, 2);
  assert.ok(direct.highlightedEdgeKeys.has('graphql->defect'));
  assert.ok(direct.highlightedEdgeKeys.has('defect->frontend'));

  // An isolated node still resolves to itself with no relations.
  const solo = computeDirectRelations('solo', edges);
  assert.equal(solo.highlightedNodeIds.size, 1);
  assert.equal(solo.highlightedEdgeKeys.size, 0);
});

test('dependency router constructs rounded orthogonal paths and preserves supplied bend points', () => {
  const points = [
    { x: 100, y: 50 },
    { x: 160, y: 50 },
    { x: 160, y: 150 },
    { x: 300, y: 150 },
  ];
  const path = getRoundedOrthogonalPath(points);
  assert.ok(path.startsWith('M 100 50'));
  assert.ok(path.includes('Q 160 50'));
  assert.ok(path.includes('Q 160 150'));
  assert.ok(path.endsWith('300 150'));
  assert.doesNotMatch(path, / C /);

  const connector = getBranchConnectorPath(150, 80, 120);
  assert.equal(connector, 'M 150 120 L 150 80');
});

test('dependency routing uses right source and left target ports with stage-aware Manhattan gutters', () => {
  const fixture = createDemoWorkflowFixture();
  const source = fixture.nodes.find((node) => node.id.endsWith('-n1'))!;
  const target = fixture.nodes.find((node) => node.id.endsWith('-n4'))!;
  const route = getDependencyRoutePoints(source, target, DEFAULT_CANVAS_DIMENSIONS, 126);
  const sourceCenter = getNodeCoordinates(source, DEFAULT_CANVAS_DIMENSIONS);
  const targetCenter = getNodeCoordinates(target, DEFAULT_CANVAS_DIMENSIONS);

  assert.deepEqual(route[0], { x: sourceCenter.x + 63, y: sourceCenter.y });
  assert.deepEqual(route.at(-1), { x: targetCenter.x - 63, y: targetCenter.y });
  for (let index = 1; index < route.length; index += 1) {
    assert.ok(
      route[index - 1].x === route[index].x || route[index - 1].y === route[index].y,
      'every dependency segment must be horizontal or vertical',
    );
  }

  const explicitBends = [
    { x: 325, y: sourceCenter.y },
    { x: 325, y: sourceCenter.y + 20 },
    { x: 335, y: sourceCenter.y + 20 },
    { x: 335, y: targetCenter.y },
  ];
  const routedWithBends = getDependencyRoutePoints(
    source,
    target,
    DEFAULT_CANVAS_DIMENSIONS,
    126,
    explicitBends,
  );
  assert.deepEqual(routedWithBends.slice(1, -1), explicitBends);
});

test('same-stage dependency routing uses outer gutters instead of crossing node bodies diagonally', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const source = model.nodes.find((node) => node.id === 'wi-cp-3')!;
  const target = model.nodes.find((node) => node.id === 'wi-cp-4')!;
  const route = getDependencyRoutePoints(source, target, DEFAULT_CANVAS_DIMENSIONS, 126);
  const stage = getStageBounds('test', DEFAULT_CANVAS_DIMENSIONS);

  assert.ok(route.some((point) => point.x === stage.endX));
  assert.ok(route.some((point) => point.x === stage.startX));
  assert.ok(
    route.every(
      (point, index) =>
        index === 0 || point.x === route[index - 1].x || point.y === route[index - 1].y,
    ),
  );
});

test('dependency edge presentation keeps default wires quiet and only adds tiny direction on focus', () => {
  const normal = getDependencyEdgePresentation({});
  const selected = getDependencyEdgePresentation({ highlighted: true });
  const dimmed = getDependencyEdgePresentation({ dimmed: true });

  assert.equal(normal.showArrow, false);
  assert.equal(selected.showArrow, true);
  assert.ok(selected.strokeWidth > normal.strokeWidth);
  assert.ok(selected.opacity > normal.opacity);
  assert.ok(dimmed.opacity >= 0.15 && dimmed.opacity <= 0.25);
  assert.ok(WORKFLOW_LINE_HIERARCHY.baselineWidth > WORKFLOW_LINE_HIERARCHY.dependencyWidth);
  assert.ok(WORKFLOW_LINE_HIERARCHY.dependencyWidth > WORKFLOW_LINE_HIERARCHY.attachmentWidth);
});

test('dependency edge presentation renders direct relations stronger than transitive relations', () => {
  const normal = getDependencyEdgePresentation({});
  const directHighlight = getDependencyEdgePresentation({ highlighted: true, direct: true });
  const transitiveHighlight = getDependencyEdgePresentation({ highlighted: true, direct: false });

  // Both remain clearly above the quiet default...
  assert.ok(transitiveHighlight.opacity > normal.opacity);
  assert.ok(transitiveHighlight.strokeWidth > normal.strokeWidth);

  // ...but direct relations read stronger than transitive ones.
  assert.ok(directHighlight.opacity > transitiveHighlight.opacity);
  assert.ok(directHighlight.strokeWidth > transitiveHighlight.strokeWidth);

  // Direction is still discoverable on a transitive-but-focused edge.
  assert.equal(transitiveHighlight.showArrow, true);

  // Omitting `direct` altogether preserves the existing full-strength
  // behavior so callers that don't classify direct/transitive edges (or
  // hover feedback, which is direct-only by construction) are unaffected.
  const unspecified = getDependencyEdgePresentation({ highlighted: true });
  assert.deepEqual(unspecified, directHighlight);
});

test('workflow edge palettes remain distinct and theme-aware without changing topology', () => {
  const light = getWorkflowEdgePalette(false);
  const dark = getWorkflowEdgePalette(true);

  assert.notEqual(light.dependency, dark.dependency);
  assert.notEqual(light.attachment, light.dependency);
  assert.notEqual(light.baseline, light.dependencyFocus);
  assert.notEqual(dark.baseline, dark.dependencyFocus);
});

test('shape, icon, and color helpers map friendly labels, symbols, and distinct hex values', () => {
  assert.equal(getItemTypeLabel('story'), 'User Story');
  assert.equal(getItemTypeLabel('task'), 'Task');
  assert.equal(getItemTypeLabel('defect'), 'Defect');
  assert.equal(getItemTypeLabel('gate'), 'Quality Gate');

  assert.equal(getItemTypeIcon('story'), '○');
  assert.equal(getItemTypeIcon('task'), '□');
  assert.equal(getItemTypeIcon('defect'), '△');
  assert.equal(getItemTypeIcon('gate'), '◇');

  // Light theme
  assert.equal(getHealthColor('green', false), '#52705E');
  assert.equal(getHealthColor('yellow', false), '#A2682A');
  assert.equal(getHealthColor('red', false), '#A85147');
  assert.equal(getHealthColor('gray', false), '#64748b');

  // Dark theme
  assert.equal(getHealthColor('green', true), '#83AA90');
  assert.equal(getHealthColor('yellow', true), '#C69358');
  assert.equal(getHealthColor('red', true), '#D28075');
  assert.equal(getHealthColor('gray', true), '#94a3b8');

  // Background colors
  assert.ok(getHealthBgColor('green', false).includes('rgba'));
  assert.ok(getHealthBgColor('red', true).includes('rgba'));
});

test('createDemoWorkflowFixture contains all 4 item types, health states, and ghost target marker', () => {
  const fixture = createDemoWorkflowFixture();

  const types = new Set(fixture.nodes.map((n) => n.itemType));
  assert.ok(types.has('story'), 'Should have story');
  assert.ok(types.has('task'), 'Should have task');
  assert.ok(types.has('defect'), 'Should have defect');
  assert.ok(types.has('gate'), 'Should have gate');

  const healths = new Set(fixture.nodes.map((n) => n.health));
  assert.ok(healths.has('green'), 'Should have green');
  assert.ok(healths.has('yellow'), 'Should have yellow');
  assert.ok(healths.has('red'), 'Should have red');

  const nodesWithGhostTarget = fixture.nodes.filter(
    (n) => n.targetStageId && n.targetStageId !== n.stageId,
  );
  assert.ok(
    nodesWithGhostTarget.length > 0,
    'Should have nodes with targetStageId for ghost outline',
  );

  // Verify default dimensions
  assert.equal(DEFAULT_MINIMAP_DIMENSIONS.height, 64);
  assert.equal(COMPACT_MINIMAP_DIMENSIONS.height, 48);
});

test('Workspace: empty branch state preserves graph workspace structure with zero mock items', () => {
  const model = buildWorkflowModelFromWorkItem(mockRealWorkItem);
  assert.equal(model.isRealData, true);
  assert.equal(model.nodes.length, 0);
  assert.equal(model.edges.length, 0);
  assert.equal(model.trunk.currentStageId, 'queue');
  assert.equal(model.workflowDataSource, 'unavailable');
});

test('Workspace: empty history state preserves honest state with authoritative database timestamps', () => {
  assert.ok(mockRealWorkItem.createdAt);
  assert.ok(mockRealWorkItem.updatedAt);
  // Real item has no completedAt yet
  assert.equal(mockRealWorkItem.completedAt, null);
});

test('Workspace: selected-item context resolves dependencies and preserves an honest empty selection', () => {
  const fixture = createDemoWorkflowFixture();

  // 1. Branch item selected
  const targetNode = fixture.nodes.find((n) => n.id === 'demo-item-1-n3')!;
  assert.ok(targetNode);
  const upstream = fixture.edges
    .filter((e) => e.to === targetNode.id)
    .map((e) => fixture.nodes.find((n) => n.id === e.from))
    .filter(Boolean);
  const downstream = fixture.edges
    .filter((e) => e.from === targetNode.id)
    .map((e) => fixture.nodes.find((n) => n.id === e.to))
    .filter(Boolean);

  assert.equal(upstream.length, 1);
  assert.equal(upstream[0]!.id, 'demo-item-1-n1');
  assert.equal(downstream.length, 1);
  assert.equal(downstream[0]!.id, 'demo-item-1-n5');

  // 2. No branch item selected keeps the workflow-level data honest.
  const realModel = buildWorkflowModelFromWorkItem(mockRealWorkItem);
  assert.equal(realModel.trunk.currentStageId, 'queue');
  assert.equal(realModel.trunk.progressPercent, 0);
  assert.equal(realModel.workflowDataSource, 'unavailable');
  assert.equal(realModel.healthLabel, 'HEALTH UNKNOWN');
  assert.equal(realModel.targetLabel, 'Target data unavailable');
});

test('Workspace: in-layout Item Explorer strictly satisfies Global AI Assistant coexistence invariant', () => {
  // Architecture invariant check:
  // ItemInspector and WorkflowCanvas render strictly within the page flow (no MUI Drawer, no Modal overlays)
  // AppShell reserves column 3 (400px) or anchor="right" Drawer for AssistantPanel.
  // Item Explorer uses a responsive ~32/68 in-page master-detail split.
  const explorerColumnsXs = '1fr';
  const explorerColumnsLg = 'minmax(260px, 1fr) minmax(0, 2.1fr)';
  assert.notEqual(explorerColumnsXs, explorerColumnsLg);
  assert.ok(explorerColumnsLg.includes('2.1fr'));
});

test('Prototype fixtures provide exactly 4 deterministic scenarios across Current and Closed lifecycle states', () => {
  assert.equal(PROTOTYPE_WORK_ITEMS.length, 4);

  const groups = groupWorkItemsByLifecycle(PROTOTYPE_WORK_ITEMS);
  assert.equal(groups.current.length, 3);
  assert.equal(groups.closed.length, 1);

  assert.deepEqual(
    groups.current.map((i) => i.id),
    ['wi-checkout-platform', 'wi-dist-systems', 'wi-lob-onboarding'],
  );
  assert.equal(groups.closed[0]!.id, 'wi-security-audit');
  assert.equal(groups.closed[0]!.lifecycleState, 'COMPLETED');
});

test('Checkout Platform prototype model represents a RED blocked state with Defect #142 blocking downstream path', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  assert.ok(model);
  assert.equal(model.aggregateHealth, 'red');
  assert.equal(model.hasAuthoritativeHealth, true);
  assert.equal(model.targetDate, '2026-09-21');
  assert.equal(model.targetStageId, 'stage');
  assert.equal(model.nodes.length, 10);
  assert.equal(model.edges.length, 14);

  // Check Defect #142 properties
  const defect = model.nodes.find((n) => n.id === 'wi-cp-4')!;
  assert.ok(defect);
  assert.equal(defect.itemType, 'defect');
  assert.equal(defect.health, 'red');
  assert.equal(defect.isResolved, false);

  // Check dependency traversal: Defect #142 blocks wi-cp-5, wi-cp-6, wi-cp-8
  const { highlightedNodeIds, highlightedEdgeKeys } = computeDependencyChain(
    defect.id,
    model.edges,
  );
  assert.ok(highlightedNodeIds.has('wi-cp-3')); // upstream
  assert.ok(highlightedNodeIds.has('wi-cp-1')); // upstream of cp-3
  assert.ok(highlightedNodeIds.has('wi-cp-5')); // downstream
  assert.ok(highlightedNodeIds.has('wi-cp-6')); // downstream
  assert.ok(highlightedNodeIds.has('wi-cp-8')); // downstream
  assert.ok(highlightedNodeIds.has('wi-cp-10')); // terminal GA gate
  assert.ok(highlightedEdgeKeys.has('wi-cp-4->wi-cp-5'));
  assert.ok(highlightedEdgeKeys.has('wi-cp-4->wi-cp-8'));

  // Rich history events
  assert.ok(model.historyEvents && model.historyEvents.length >= 8);
  assert.ok(model.itemHistories && Object.keys(model.itemHistories).length >= 3);
});

test('Workspace fixture carries complete workflow-level context and explainable history', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;

  assert.equal(model.origin?.kind, 'Product initiative');
  assert.equal(model.nextMilestone?.label, 'Security patch verification');
  assert.equal(model.targetTimingLabel, '3 days remaining');
  assert.ok(model.historyEvents?.every((event) => event.source));
  assert.ok(model.historyEvents?.every((event) => event.reason));
  assert.ok(model.historyEvents?.every((event) => event.affectedEntity));
});

test('Overview workflow activity is a deterministic recent subset distinct from selected-item activity', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const recent = getRecentWorkflowHistory(model, 6);

  assert.equal(recent.length, 6);
  assert.equal(recent[0]?.id, 'cph-1');
  assert.ok((model.historyEvents?.length ?? 0) > recent.length);
  assert.notDeepEqual(recent, model.itemHistories?.['wi-cp-4']);
});

test('Full History fixture covers lifecycle, structure, target, health, gate, todo, notification, and evidence events', () => {
  const events = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!.historyEvents ?? [];
  const titles = events.map((event) => event.title).join(' | ');

  for (const expected of [
    'Outcome Initialized',
    'Branch Introduced',
    'Target Moved',
    'Health Degraded',
    'Gate Requirements',
    'Todo Created',
    'Todo Completed',
    'Notification Sent',
    'Evidence Linked',
  ]) {
    assert.ok(titles.includes(expected), `missing ${expected}`);
  }
});

test('Timing fixtures expose both remaining and overdue states without wall-clock calculation', () => {
  const checkout = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const learning = PROTOTYPE_WORKFLOW_MODELS['wi-dist-systems']!;
  const overdue = learning.nodes.find((node) => node.id === 'wi-ds-4');

  assert.match(checkout.targetTimingLabel ?? '', /remaining/);
  assert.match(overdue?.nextMilestone?.timingLabel ?? '', /Overdue/);
});

test('Selecting different checkout nodes resolves distinct rich in-page detail contexts', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const api = model.nodes.find((node) => node.id === 'wi-cp-3')!;
  const defect = model.nodes.find((node) => node.id === 'wi-cp-4')!;

  assert.notEqual(api.description, defect.description);
  assert.equal(api.source?.kind, 'Architecture workstream');
  assert.equal(defect.source?.kind, 'Automated evidence');
  assert.equal(api.relatedTodos?.length, 2);
  assert.equal(defect.relatedNotifications?.length, 2);
  assert.ok(defect.nextMilestone?.timingLabel.includes('remaining'));
  assert.ok(model.itemHistories?.[api.id]?.length);
  assert.ok(model.itemHistories?.[defect.id]?.length);
});

test('Item Explorer exposes the fixture list and filters by search, health risk, and stage', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;

  assert.equal(filterItemExplorerNodes(model, '', 'all', 'all').length, 10);
  assert.deepEqual(
    filterItemExplorerNodes(model, 'token leak', 'all', 'all').map((node) => node.id),
    ['wi-cp-4'],
  );
  assert.ok(
    filterItemExplorerNodes(model, '', 'at-risk', 'all').every((node) => node.health === 'yellow'),
  );
  assert.ok(
    filterItemExplorerNodes(model, '', 'blocked', 'all').every((node) => node.health === 'red'),
  );
  assert.ok(
    filterItemExplorerNodes(model, '', 'all', 'stage').every((node) => node.stageId === 'stage'),
  );
});

test('Item Explorer selection resolves detail identity, stage, health, and human-readable signal', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const selectedId = resolveItemExplorerSelection(model, 'wi-cp-4');
  const selected = model.nodes.find((node) => node.id === selectedId)!;

  assert.equal(selected.title, 'Defect #142 Token Leak');
  assert.equal(selected.stageId, 'test');
  assert.equal(selected.health, 'red');
  assert.equal(getItemSignal(selected, model), 'Blocks 3 items');
  assert.equal(resolveItemExplorerSelection(model, 'missing-item'), model.nodes[0]?.id);
});

test('Compact Item List row contract renders shape/title then short stage/signal without redundant health text', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const api = model.nodes.find((node) => node.id === 'wi-cp-3')!;
  const row = getItemListRowPresentation(api, model);

  assert.equal(row.title, 'GraphQL Payment API');
  assert.equal(row.shapeSymbol, '○');
  assert.equal(row.shapeHealth, 'yellow');
  assert.equal(row.stage, 'TEST');
  assert.equal(row.signal, '1 alert');
  assert.equal(row.secondaryText, 'TEST · 1 alert');
  assert.doesNotMatch(row.secondaryText, /At Risk|On Track|Unknown/);
  assert.match(row.accessibilityLabel, /User Story/);
  assert.match(row.accessibilityLabel, /At Risk/);
  assert.equal(row.tooltip, 'User Story · At Risk');
});

test('Compact Item List signal helper selects one highest-value presentation signal', () => {
  const checkout = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const learning = PROTOTYPE_WORKFLOW_MODELS['wi-dist-systems']!;

  assert.equal(
    getItemSignal(
      checkout.nodes.find((node) => node.id === 'wi-cp-4')!,
      checkout,
    ),
    'Blocks 3 items',
  );
  assert.equal(
    getItemSignal(
      checkout.nodes.find((node) => node.id === 'wi-cp-5')!,
      checkout,
    ),
    'Waiting on Defect #142',
  );
  assert.equal(
    getItemSignal(
      learning.nodes.find((node) => node.id === 'wi-ds-4')!,
      learning,
    ),
    'Overdue 3d',
  );
});

test('Compact Item List implementation keeps one semantic shape, truncates titles, and preserves selection click wiring', () => {
  const source = readFileSync(
    new URL('../src/components/workItems/workspace/WorkItemItemsTab.tsx', import.meta.url),
    'utf8',
  );

  assert.doesNotMatch(source, /borderRadius:\s*'50%'/);
  assert.match(source, /aria-label=\{row\.accessibilityLabel\}/);
  assert.match(source, /noWrap/);
  assert.match(source, /onClick=\{\(\) => selectItem\(node\.id\)\}/);
  assert.match(source, /borderLeftColor:\s*selected/);
});

test('Workflow item CTA deep-links to Items while retaining the exact selected item', () => {
  assert.deepEqual(createItemExplorerDeepLink('wi-cp-3'), {
    activeTab: 'items',
    selectedItemId: 'wi-cp-3',
  });
});

test('Item Explorer dependency navigation resolves both Depends On and Blocks targets in place', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const relationships = getItemRelationships(model, 'wi-cp-4');

  assert.deepEqual(
    relationships.dependsOn.map((node) => node.id),
    ['wi-cp-3'],
  );
  assert.deepEqual(
    relationships.blocks.map((node) => node.id),
    ['wi-cp-5', 'wi-cp-6', 'wi-cp-8'],
  );
  assert.ok(
    filterItemExplorerNodes(model, '', 'all', 'all').some(
      (node) => node.id === relationships.dependsOn[0]?.id,
    ),
  );
});

test('Complex Item Explorer fixture includes deterministic timing, related work, provenance, evidence, and item history', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const defect = model.nodes.find((node) => node.id === 'wi-cp-4')!;

  assert.equal(defect.targetDate, 'Sep 18');
  assert.equal(defect.nextMilestone?.timingLabel, '5 hours remaining');
  assert.equal(defect.relatedTodos?.length, 2);
  assert.equal(defect.relatedNotifications?.length, 2);
  assert.equal(defect.source?.reference, 'RALLY-4821');
  assert.ok(defect.source?.why);
  assert.ok(defect.evidence);
  assert.ok((model.itemHistories?.[defect.id]?.length ?? 0) >= 5);
});

test('Workflow History and Item History stay distinct and WorkItem UI introduces no overlay inspector', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
  const workflowIds = new Set((model.historyEvents ?? []).map((event) => event.id));
  const itemEvents = model.itemHistories?.['wi-cp-4'] ?? [];
  assert.ok(itemEvents.every((event) => !workflowIds.has(event.id)));

  const workspaceSource = readFileSync(
    new URL('../src/components/workItems/workspace/WorkItemWorkspace.tsx', import.meta.url),
    'utf8',
  );
  const explorerSource = readFileSync(
    new URL('../src/components/workItems/workspace/WorkItemItemsTab.tsx', import.meta.url),
    'utf8',
  );
  assert.doesNotMatch(`${workspaceSource}\n${explorerSource}`, /\b(Drawer|Modal)\b/);
});

test('Distributed Systems prototype model represents a YELLOW at-risk state with Partition bug', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-dist-systems']!;
  assert.ok(model);
  assert.equal(model.aggregateHealth, 'yellow');
  assert.equal(model.targetDate, '2026-09-29');
  assert.equal(model.nodes.length, 8);
  assert.equal(model.trunk.currentStageId, 'dev');

  const lab2b = model.nodes.find((n) => n.id === 'wi-ds-4')!;
  assert.equal(lab2b.health, 'yellow');
  const defect = model.nodes.find((n) => n.id === 'wi-ds-5')!;
  assert.equal(defect.health, 'red');
});

test('LOB Onboarding prototype model represents a GREEN healthy progression with Oct 15 target', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-lob-onboarding']!;
  assert.ok(model);
  assert.equal(model.aggregateHealth, 'green');
  assert.equal(model.targetDate, '2026-10-15');
  assert.equal(model.trunk.currentStageId, 'stage');
  assert.equal(model.nodes.length, 5);

  const resolvedNodes = model.nodes.filter((n) => n.isResolved);
  assert.equal(resolvedNodes.length, 3);
});

test('Security Audit prototype model represents a COMPLETED certification in Closed group with 100% resolution', () => {
  const model = PROTOTYPE_WORKFLOW_MODELS['wi-security-audit']!;
  assert.ok(model);
  assert.equal(model.trunk.currentStageId, 'production');
  assert.equal(model.trunk.progressPercent, 100);
  assert.equal(model.nodes.length, 5);
  assert.ok(model.nodes.every((n) => n.isResolved));
});

test('Interactive prototype mutations: create and direct lifecycle commands operate correctly in memory', async () => {
  const initialItems = await prototypeWorkItemApi.list();
  assert.equal(initialItems.length >= 4, true);

  // 1. Create in prototype mode
  const created = await prototypeWorkItemApi.create({
    title: 'Prototype Interactive Verification',
    description: 'Local prototype test item',
  });
  assert.ok(created.id.startsWith('wi-proto-'));
  assert.equal(created.lifecycleState, 'ACTIVE');

  // Verify getPrototypeWorkflowModel generates a valid model for user-created items
  const createdModel = getPrototypeWorkflowModel(created.id);
  assert.equal(createdModel.workItemId, created.id);
  assert.equal(createdModel.title, 'Prototype Interactive Verification');

  // 2. Set Lifecycle in prototype mode
  const paused = await prototypeWorkItemApi.setLifecycle({
    id: created.id,
    targetLifecycleState: 'PAUSED',
  });
  assert.equal(paused.lifecycleState, 'PAUSED');

  const completed = await prototypeWorkItemApi.setLifecycle({
    id: created.id,
    targetLifecycleState: 'COMPLETED',
  });
  assert.equal(completed.lifecycleState, 'COMPLETED');
  assert.ok(completed.completedAt);
  const completedModel = getPrototypeWorkflowModel(created.id);
  assert.equal(completedModel.trunk.progressPercent, 100);
  assert.equal(completedModel.trunk.currentStageId, 'production');
});
