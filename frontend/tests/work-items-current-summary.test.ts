import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  computeStageBounds,
  computeStageItemLayouts,
  selectRepresentativeEdgeRoutes,
  DEFAULT_TOPOLOGY_DIMENSIONS,
  MAX_VISIBLE_ITEMS_PER_STAGE,
} from '../src/components/workItems/currentSummary/compactTopologyLayout.ts';
import {
  buildAttentionSummary,
  buildCompactTopologyModel,
  buildItemRollup,
  buildScheduleSummary,
  MAX_REPRESENTATIVE_EDGES,
} from '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts';
import {
  ROW_CENTER_RIGHT_TOLERANCE,
  ROW_COLUMN_FRACTIONS,
  buildRowGridTemplateColumns,
} from '../src/components/workItems/currentSummary/rowLayoutContract.ts';
import { PROTOTYPE_WORKFLOW_MODELS } from '../src/components/workItems/fixtures/workItemPrototypeFixtures.ts';
import { buildWorkflowModelFromWorkItem } from '../src/components/workItems/workflow/workflowTypes.ts';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const checkout = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;
const distSystems = PROTOTYPE_WORKFLOW_MODELS['wi-dist-systems']!;

// --- Row layout (retained from the prior round) ------------------------------

test('LEFT < CENTER ≈ RIGHT: the thumbnail matches the detail column, neither is an even split', () => {
  const { left, center, right } = ROW_COLUMN_FRACTIONS;
  assert.ok(left < center && left < right);
  assert.ok(Math.abs(center - right) <= ROW_CENTER_RIGHT_TOLERANCE);
  assert.ok(left >= 0.2 && left <= 0.26);
  assert.ok(center > 0.34 && right > 0.34);
  assert.ok(Math.abs(left + center + right - 1) < 1e-9);
});

test('WorkItemRow.tsx builds its grid template from the shared row layout contract', () => {
  const source = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  assert.ok(source.includes('buildRowGridTemplateColumns()'));
  assert.equal(buildRowGridTemplateColumns(), '0.23fr 0.385fr 0.385fr');
});

// 1. Permanent stage labels are not rendered ----------------------------------

test('the compact topology renderer never renders a permanent Stage name on the canvas', () => {
  const source = sourceOf('../src/components/workItems/currentSummary/CompactWorkflowTopology.tsx');
  // The only place a stage/item label may appear is inside a `title` prop
  // (the SVG native hover tooltip) — never as a rendered <text> element
  // showing a stage's own label.
  assert.ok(
    !/<text[^>]*>\s*\{stage\.label/.test(source),
    'must not render stage.label as permanent SVG text',
  );
  assert.ok(
    source.includes('stageLabelById.get(item.stageId)'),
    'stage label must still be available on hover',
  );
});

test('real WorkItem fixtures never surface their stage vocabulary as permanent on-canvas text', () => {
  const source = sourceOf('../src/components/workItems/currentSummary/CompactWorkflowTopology.tsx');
  for (const forbidden of [
    'IMPLEMENTATION',
    'VERIFICATION',
    'COMPLIANCE',
    'STAGING',
    'REVIEW',
    'DEFENSE',
    'PRODUCTION',
    'BUILD',
    'RELEASE',
  ]) {
    assert.ok(!source.includes(forbidden), `must not hardcode "${forbidden}" as rendered text`);
  }
});

// 2. Stage presentation data still drives count/ordering/bands/placement -----

test('stage data still drives stage count, ordering, and band boundaries', () => {
  const topology = buildCompactTopologyModel(checkout);
  assert.equal(topology.stages.length, 5);
  const layouts = computeStageItemLayouts(topology, DEFAULT_TOPOLOGY_DIMENSIONS);
  assert.equal(layouts.length, topology.stages.length);
  for (let i = 1; i < layouts.length; i += 1) {
    assert.equal(layouts[i]!.bounds.startX, layouts[i - 1]!.bounds.endX);
  }
});

// 3. No global current-stage visual contract ----------------------------------

test('CompactTopologyModel exposes no global current-stage field', () => {
  const topology = buildCompactTopologyModel(checkout);
  assert.ok(!('currentStageId' in topology));
  assert.ok(!('currentStageLabel' in topology));
});

test('the renderer contains no current-stage band highlight or trunk-position marker', () => {
  const source = sourceOf('../src/components/workItems/currentSummary/CompactWorkflowTopology.tsx');
  assert.ok(!source.includes('currentStageId'));
  assert.ok(!source.includes('isCurrent'));
});

// 4. RIGHT no longer renders CURRENT/NEXT/RECENT ------------------------------

test('RIGHT no longer renders CURRENT, NEXT, or RECENT sections', () => {
  const source = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  assert.ok(!/>CURRENT<|>NEXT<|>RECENT</.test(source));
  assert.ok(source.includes('OverviewAttentionText'));
  assert.ok(source.includes('TRACKED WORK'));
});

test('the adapter no longer exports a Current/Next/Recent builder', () => {
  const source = sourceOf(
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
  );
  assert.ok(!source.includes('buildCurrentNextRecentSummary'));
});

// 5-7. LEFT item rollup ---------------------------------------------------------

test('LEFT shows the total item count', () => {
  const source = sourceOf('../src/components/workItems/overview/OverviewSummaryBlocks.tsx');
  assert.ok(source.includes('tracking.trackedItemCount'));
});

test('item rollup buckets are a deterministic aggregation of existing node health/isResolved fields', () => {
  const rollup = buildItemRollup(checkout);
  // Recomputed from the fixture's own known composition (see fixtures file):
  // 2 resolved (done), 2 unresolved-yellow (at risk), 3 unresolved-red (blocked), 3 unresolved-gray (queued).
  const byBucket = Object.fromEntries(rollup.entries.map((e) => [e.bucket, e.count]));
  assert.equal(byBucket.done, 2);
  assert.equal(byBucket.atRisk, 2);
  assert.equal(byBucket.blocked, 3);
  assert.equal(byBucket.queued, 3);
  assert.equal(byBucket.active, undefined, 'checkout fixture has no unresolved-green node');
});

test('rollup entries always sum to totalItems, and zero-count buckets are omitted', () => {
  for (const model of [checkout, distSystems]) {
    const rollup = buildItemRollup(model);
    const sum = rollup.entries.reduce((total, entry) => total + entry.count, 0);
    assert.equal(sum, rollup.totalItems);
    assert.equal(rollup.totalItems, model.nodes.length);
    for (const entry of rollup.entries) assert.ok(entry.count > 0);
  }
});

// 8. Blocked/at-risk attention counts -------------------------------------------

test('attention blocked/at-risk counts match unresolved red/yellow node counts', () => {
  const attention = buildAttentionSummary(checkout);
  const expectedBlocked = checkout.nodes.filter((n) => !n.isResolved && n.health === 'red').length;
  const expectedAtRisk = checkout.nodes.filter(
    (n) => !n.isResolved && n.health === 'yellow',
  ).length;
  assert.equal(attention.blockedCount, expectedBlocked);
  assert.equal(attention.atRiskCount, expectedAtRisk);
});

// 9. Overdue is pure date arithmetic on a real target date, never createdAt/updatedAt --

test('overdue count is computed only from a real per-item target date, never createdAt/updatedAt', () => {
  const source = sourceOf(
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
  );
  assert.ok(
    !/isNodeOverdue[\s\S]{0,200}\.createdAt|isNodeOverdue[\s\S]{0,200}\.updatedAt/.test(source),
  );

  const attention = buildAttentionSummary(checkout);
  // wi-cp-4 (Defect #142) has a real per-node targetDate of 'Sep 18', which
  // is in the past relative to today (2026-09-19 in this session) — a
  // genuine, presentation-sourced overdue item.
  assert.ok(attention.overdueCount >= 1);
});

test('an item with no target date is never counted as overdue', () => {
  const noDateModel = {
    ...checkout,
    nodes: checkout.nodes.map((n) => ({ ...n, targetDate: undefined })),
  };
  const attention = buildAttentionSummary(noDateModel);
  assert.equal(attention.overdueCount, 0);
});

// 10. Notifications sourced from existing presentation fixture only -----------

test("notification count and highlights come from the WorkItem nodes' existing relatedNotifications field", () => {
  const attention = buildAttentionSummary(checkout);
  const expectedActive = checkout.nodes
    .flatMap((n) => n.relatedNotifications ?? [])
    .filter((n) => n.status !== 'RESOLVED');
  assert.equal(attention.notificationCount, expectedActive.length);
  assert.ok(
    attention.notificationCount > 0,
    'checkout fixture should have at least one active notification',
  );
});

test('the adapter never imports a Notification/MonitorPoint/Attention backend', () => {
  const source = sourceOf(
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
  );
  for (const fragment of ['NotificationBackend', 'MonitorPoint', 'AttentionBackend', 'wailsjs']) {
    assert.ok(!source.includes(fragment));
  }
});

// 11. Risk item lists are bounded ------------------------------------------------

test('blocked and at-risk item lists are bounded, with a "has more" signal instead of silent truncation', () => {
  const attention = buildAttentionSummary(checkout);
  assert.ok(attention.blockedItems.length <= 2);
  assert.ok(attention.atRiskItems.length <= 2);
  if (attention.blockedCount > attention.blockedItems.length) {
    assert.ok(attention.hasMoreBlocked);
  }
});

// 12. Every center node still attaches to the trunk (retained) ----------------

test('every visible item is still positioned directly above or below the trunk', () => {
  const topology = buildCompactTopologyModel(checkout);
  const layouts = computeStageItemLayouts(topology, DEFAULT_TOPOLOGY_DIMENSIONS);
  for (const { bounds, visible } of layouts) {
    for (const positioned of visible) {
      assert.equal(positioned.x, bounds.centerX);
    }
  }
});

test('items within the same stage never share the same visual slot', () => {
  const topology = buildCompactTopologyModel(checkout);
  const layouts = computeStageItemLayouts(topology, DEFAULT_TOPOLOGY_DIMENSIONS);
  for (const { visible } of layouts) {
    const slots = visible.map((positioned) => `${positioned.x},${positioned.y}`);
    assert.equal(new Set(slots).size, slots.length);
  }
});

test('a stage with more items than the visible cap reports the overflow instead of silently dropping items', () => {
  const layouts = computeStageItemLayouts(
    {
      stages: [{ id: 's1', label: 'Stage One' }],
      trunkHealth: 'gray',
      items: Array.from({ length: MAX_VISIBLE_ITEMS_PER_STAGE + 2 }, (_, i) => ({
        id: `item-${i}`,
        title: `Item ${i}`,
        itemType: 'task' as const,
        health: 'gray' as const,
        stageId: 's1',
      })),
      edges: [],
      hasMoreItems: false,
    },
    DEFAULT_TOPOLOGY_DIMENSIONS,
  );
  assert.equal(layouts[0]!.visible.length, MAX_VISIBLE_ITEMS_PER_STAGE);
  assert.equal(layouts[0]!.overflowCount, 2);
});

// 13. Center does not depend on one global current stage (retained/expanded) --

test('an item early in the workflow and an item late in the workflow can coexist without any global current-stage inference', () => {
  // dist-systems fixture: wi-ds-1 (queue, resolved) is early; wi-ds-8 (production, unresolved) is late.
  const topology = buildCompactTopologyModel(distSystems);
  const early = topology.items.find((i) => i.id === 'wi-ds-1')!;
  const late = topology.items.find((i) => i.id === 'wi-ds-8')!;
  assert.ok(early && late);
  assert.notEqual(early.stageId, late.stageId);
  // No field on the model claims either of these is "the" current stage.
  assert.ok(!('currentStageId' in topology));
});

// Dependency simplification / import boundaries (retained) --------------------

test('the compact renderer/layout do not import the Dependency Explorer routing implementation', () => {
  for (const file of [
    '../src/components/workItems/currentSummary/compactTopologyLayout.ts',
    '../src/components/workItems/currentSummary/CompactWorkflowTopology.tsx',
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
  ]) {
    const source = sourceOf(file);
    assert.ok(!source.includes('dependencyExperiment'));
    assert.ok(!source.includes('dependencyRouting'));
  }
});

test('representative edge selection is capped and produces a simple orthogonal detour', () => {
  const topology = buildCompactTopologyModel(checkout);
  const routes = selectRepresentativeEdgeRoutes(
    topology,
    DEFAULT_TOPOLOGY_DIMENSIONS,
    MAX_REPRESENTATIVE_EDGES,
  );
  assert.ok(routes.length <= MAX_REPRESENTATIVE_EDGES);
  for (const route of routes) assert.equal(route.points.length, 4);
});

test('WorkItemRow reuses the existing onOpenWorkspace navigation callback', () => {
  const source = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  assert.ok(source.includes('onOpenWorkspace(item.id)'));
  assert.ok(!/window\.location|history\.push|useNavigate/.test(source));
});

test('the current-summary module never imports backend-generated models, the roadmap authority, Workflow backend, or Monitor backend', () => {
  for (const file of [
    '../src/components/workItems/currentSummary/compactTopologyTypes.ts',
    '../src/components/workItems/currentSummary/compactTopologyLayout.ts',
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
    '../src/components/workItems/currentSummary/CompactWorkflowTopology.tsx',
    '../src/components/workItems/currentSummary/rowLayoutContract.ts',
    '../src/components/workItems/WorkItemRow.tsx',
  ]) {
    const source = sourceOf(file);
    for (const fragment of [
      'wailsjs/go/models',
      'dashboardApi',
      'activeWorkflowsRoadmap',
      'MonitorPoint',
    ]) {
      assert.ok(!source.includes(fragment), `${file} must not reference "${fragment}"`);
    }
  }
});

test('RIGHT renders the WorkItem summary/description from existing prototype presentation text', () => {
  const source = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  assert.ok(source.includes('SUMMARY'));
  assert.ok(source.includes('item.description'));
});

test('LEFT no longer renders the WorkItem feature description/summary text', () => {
  const source = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  assert.ok(source.indexOf('OverviewRollup') < source.indexOf('item.description'));
});

test('column dividers reuse the existing divider design token but softened', () => {
  const contractSource = sourceOf(
    '../src/components/workItems/currentSummary/rowLayoutContract.ts',
  );
  assert.ok(contractSource.includes('alpha(theme.palette.divider'));
  const rowSource = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  const dividerUsages = rowSource.match(/buildColumnDividerBorderColor/g) ?? [];
  assert.equal(dividerUsages.length, 3);
});

test('a WorkItem with no stage-label override still avoids the removed BUILD/VERIFY/RELEASE taxonomy', () => {
  const realItem: ProductWorkItem = {
    id: 'wi-some-real-item',
    title: 'Real backend item',
    description: null,
    lifecycleState: 'ACTIVE',
    completedAt: null,
    createdAt: '2026-09-18T12:00:00Z',
    updatedAt: '2026-09-18T12:00:00Z',
  };
  const model = buildWorkflowModelFromWorkItem(realItem);
  const topology = buildCompactTopologyModel(model);
  assert.equal(topology.stages.length, 0);
});

test('schedule remaining-day wording is pure date arithmetic, never fabricated health language', () => {
  const source = sourceOf(
    '../src/components/workItems/currentSummary/compactTopologyPrototypeAdapter.ts',
  );
  const describeRemainingMatch = /function describeRemaining[\s\S]*?\n}/.exec(source);
  assert.ok(describeRemainingMatch, 'expected to find describeRemaining()');
  const fn = describeRemainingMatch![0];
  for (const forbidden of ['BLOCKED', 'AT RISK', 'CRITICAL']) {
    assert.ok(!fn.includes(forbidden), `describeRemaining() must not use "${forbidden}"`);
  }
});

test('a WorkItem with a real target date produces schedule arithmetic consistent with today', () => {
  const schedule = buildScheduleSummary(checkout);
  assert.equal(schedule.targetLabel, 'Sep 21');
  assert.match(schedule.remainingLabel!, /remaining|Overdue|Due today/);
});

test('computeStageBounds divides the timeline width evenly among however many stages this WorkItem actually has', () => {
  const stages = [
    { id: 'a', label: 'A' },
    { id: 'b', label: 'B' },
  ];
  const first = computeStageBounds('a', stages, DEFAULT_TOPOLOGY_DIMENSIONS);
  const second = computeStageBounds('b', stages, DEFAULT_TOPOLOGY_DIMENSIONS);
  assert.equal(first.width, second.width);
  assert.equal(first.endX, second.startX);
});
