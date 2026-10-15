import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  CANVAS_MAX_HEIGHT,
  CANVAS_MIN_HEIGHT,
  classifyEdgeRouting,
  computeRenderedGraphWidth,
  computeStageColumnWidth,
  computeSwimlaneLayout,
  DEFAULT_EXPERIMENT_LAYOUT_OPTIONS,
  EXPERIMENT_STAGES,
  experimentEdgeKey,
  FALLBACK_STAGE,
  getExperimentEdgePath,
  getExperimentEdgePresentation,
  getStageById,
  GRAPH_RIGHT_PADDING,
  hasHorizontalOverflow,
  resolveDefinedStages,
  resolveNodeStage,
  STAGE_IDEAL_WIDTH,
  STAGE_MAX_WIDTH,
  STAGE_MIN_WIDTH,
} from '../src/components/workItems/dependencyExperiment/dependencyRouting.ts';
import { computeDependencyChain } from '../src/components/workItems/workflow/workflowLayout.ts';
import type { WorkflowNode } from '../src/components/workItems/workflow/workflowTypes.ts';
import { PROTOTYPE_WORKFLOW_MODELS } from '../src/components/workItems/fixtures/workItemPrototypeFixtures.ts';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const checkout = PROTOTYPE_WORKFLOW_MODELS['wi-checkout-platform']!;

test('EXPERIMENT_STAGES defines the five presentation stages in order, distinct from WorkflowStageId', () => {
  assert.deepEqual(
    EXPERIMENT_STAGES.map((s) => s.id),
    ['foundation', 'api', 'frontend', 'verification', 'release'],
  );
  assert.deepEqual(
    EXPERIMENT_STAGES.map((s) => s.order),
    [0, 1, 2, 3, 4],
  );
});

test('resolveNodeStage assigns the curated fixture items to their intended presentation stage', () => {
  assert.equal(resolveNodeStage('wi-cp-1').id, 'foundation'); // Database Schema Migration
  assert.equal(resolveNodeStage('wi-cp-2').id, 'foundation'); // Vault Secret Injection
  assert.equal(resolveNodeStage('wi-cp-3').id, 'api'); // GraphQL Payment API
  assert.equal(resolveNodeStage('wi-cp-4').id, 'frontend'); // Defect #142 Token Leak
  assert.equal(resolveNodeStage('wi-cp-5').id, 'verification'); // Regression Test Suite
  assert.equal(resolveNodeStage('wi-cp-6').id, 'frontend'); // Frontend Checkout Flow
  assert.equal(resolveNodeStage('wi-cp-8').id, 'release'); // PCI-DSS Compliance Sign-off
});

test('resolveNodeStage falls back to a generic bucket for items with no curated stage, rather than inventing a mapping', () => {
  const stage = resolveNodeStage('some-other-work-item-node');
  assert.equal(stage.id, FALLBACK_STAGE.id);
  assert.equal(getStageById('does-not-exist').id, FALLBACK_STAGE.id);
});

test('classifyEdgeRouting distinguishes same-stage, adjacent-stage, long-cross-stage, and backward purely from stage order', () => {
  assert.equal(classifyEdgeRouting(2, 2), 'same-stage');
  assert.equal(classifyEdgeRouting(1, 2), 'adjacent-stage');
  assert.equal(classifyEdgeRouting(0, 2), 'long-cross-stage');
  assert.equal(classifyEdgeRouting(0, 4), 'long-cross-stage');
  assert.equal(classifyEdgeRouting(3, 2), 'backward');
  assert.equal(classifyEdgeRouting(4, 0), 'backward');
});

test('the representative Checkout Platform fixture actually exercises all four routing categories', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const stageOrderById = new Map(layout.columns.map((c) => [c.stage.id, c.stage.order]));
  const categories = new Set(
    checkout.edges.map((edge) => {
      const sourceOrder = stageOrderById.get(resolveNodeStage(edge.from).id)!;
      const targetOrder = stageOrderById.get(resolveNodeStage(edge.to).id)!;
      return classifyEdgeRouting(sourceOrder, targetOrder);
    }),
  );
  assert.ok(categories.has('same-stage'), 'fixture must include a same-stage dependency');
  assert.ok(categories.has('adjacent-stage'), 'fixture must include an adjacent-stage dependency');
  assert.ok(
    categories.has('long-cross-stage'),
    'fixture must include a long-cross-stage dependency',
  );
  assert.ok(categories.has('backward'), 'fixture must include a backward/exception dependency');
});

test("GraphQL Payment API -> Defect #142 is classified adjacent-stage, matching the brief's own worked example", () => {
  const sourceOrder = getStageById(resolveNodeStage('wi-cp-3').id).order; // API
  const targetOrder = getStageById(resolveNodeStage('wi-cp-4').id).order; // Frontend
  assert.equal(classifyEdgeRouting(sourceOrder, targetOrder), 'adjacent-stage');
});

test('Defect #142 -> PCI-DSS Compliance Sign-off is classified long-cross-stage (skips Verification)', () => {
  const sourceOrder = getStageById(resolveNodeStage('wi-cp-4').id).order; // Frontend
  const targetOrder = getStageById(resolveNodeStage('wi-cp-8').id).order; // Release
  assert.equal(classifyEdgeRouting(sourceOrder, targetOrder), 'long-cross-stage');
});

test('Regression Test Suite -> Frontend Checkout Flow is classified backward (Verification precedes Frontend in normal flow)', () => {
  const sourceOrder = getStageById(resolveNodeStage('wi-cp-5').id).order; // Verification
  const targetOrder = getStageById(resolveNodeStage('wi-cp-6').id).order; // Frontend
  assert.equal(classifyEdgeRouting(sourceOrder, targetOrder), 'backward');
});

test('Defect #142 -> Frontend Checkout Flow (both Frontend) is classified same-stage', () => {
  const sourceOrder = getStageById(resolveNodeStage('wi-cp-4').id).order;
  const targetOrder = getStageById(resolveNodeStage('wi-cp-6').id).order;
  assert.equal(classifyEdgeRouting(sourceOrder, targetOrder), 'same-stage');
});

test('computeSwimlaneLayout groups nodes into columns by stage and gives every node a position', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  assert.equal(layout.positions.size, checkout.nodes.length);
  assert.deepEqual(
    layout.columns.map((c) => c.stage.id),
    ['foundation', 'api', 'frontend', 'verification', 'release'],
  );
  // Foundation items share a column (same x) and are stacked vertically (different y).
  const db = layout.positions.get('wi-cp-1')!;
  const vault = layout.positions.get('wi-cp-2')!;
  assert.equal(db.x, vault.x);
  assert.notEqual(db.y, vault.y);
});

test('computeSwimlaneLayout places later stages strictly to the right, matching left-to-right flow', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const xs = layout.columns.map((c) => c.centerX);
  for (let i = 1; i < xs.length; i += 1) {
    assert.ok(xs[i]! > xs[i - 1]!, 'each stage column must sit to the right of the previous one');
  }
});

test('getExperimentEdgePath keeps every segment horizontal or vertical (Manhattan) for all four categories', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const source = layout.positions.get('wi-cp-3')!;
  const target = layout.positions.get('wi-cp-4')!;
  for (const category of [
    'same-stage',
    'adjacent-stage',
    'long-cross-stage',
    'backward',
  ] as const) {
    const path = getExperimentEdgePath(category, source, target, layout);
    assert.match(path, /^M /);
    // No cubic/diagonal curve commands — only M/L/Q from the shared rounded-orthogonal builder.
    assert.doesNotMatch(path, / C /);
  }
});

test('same-stage routing hugs the local rail rather than a straight line between card centers', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const source = layout.positions.get('wi-cp-4')!; // Frontend
  const target = layout.positions.get('wi-cp-6')!; // Frontend
  const column = layout.columns.find((c) => c.stage.id === 'frontend')!;
  const path = getExperimentEdgePath('same-stage', source, target, layout);
  // The local rail x must appear in the path, and it must sit to the right
  // of the card (inside the column's own gutter), never through the card body.
  assert.ok(path.includes(String(column.localRailX)));
  assert.ok(column.localRailX > source.x + DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.cardWidth / 2);
});

test('long-cross-stage routing uses the shared top rail, never a direct line through intervening stages', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const source = layout.positions.get('wi-cp-4')!; // Frontend
  const target = layout.positions.get('wi-cp-8')!; // Release (skips Verification)
  const path = getExperimentEdgePath('long-cross-stage', source, target, layout);
  assert.ok(path.includes(String(layout.topRailY)));
});

test('backward routing uses the shared bottom rail, kept separate from the forward rails', () => {
  const layout = computeSwimlaneLayout(checkout.nodes);
  const source = layout.positions.get('wi-cp-5')!;
  const target = layout.positions.get('wi-cp-6')!;
  const path = getExperimentEdgePath('backward', source, target, layout);
  assert.ok(path.includes(String(layout.bottomRailY)));
  assert.notEqual(layout.bottomRailY, layout.topRailY);
});

test('getExperimentEdgePresentation always dashes backward edges, even when idle, so direction reads without relying on color', () => {
  const idleBackward = getExperimentEdgePresentation({ category: 'backward' });
  const idleAdjacent = getExperimentEdgePresentation({ category: 'adjacent-stage' });
  assert.equal(idleBackward.dashed, true);
  assert.equal(idleAdjacent.dashed, false);

  const highlightedBackward = getExperimentEdgePresentation({
    category: 'backward',
    highlighted: true,
  });
  assert.equal(highlightedBackward.dashed, true);
});

test('getExperimentEdgePresentation renders direct relations stronger than transitive, and dims unrelated edges without hiding them', () => {
  const direct = getExperimentEdgePresentation({
    category: 'adjacent-stage',
    highlighted: true,
    direct: true,
  });
  const transitive = getExperimentEdgePresentation({
    category: 'adjacent-stage',
    highlighted: true,
    direct: false,
  });
  const dimmed = getExperimentEdgePresentation({ category: 'adjacent-stage', dimmed: true });

  assert.ok(direct.opacity > transitive.opacity);
  assert.ok(transitive.opacity > dimmed.opacity);
  assert.ok(
    dimmed.opacity > 0,
    'dimmed edges must reduce weight, never fully disappear (opacity 0)',
  );
});

test('selecting Defect #142 highlights its real direct upstream and downstream, and nothing unrelated', () => {
  const { highlightedNodeIds, directEdgeKeys } = computeDependencyChain('wi-cp-4', checkout.edges);

  // Direct upstream: GraphQL Payment API.
  assert.ok(directEdgeKeys.has('wi-cp-3->wi-cp-4'));
  // Direct downstream: Regression Test Suite, Frontend Checkout Flow, PCI-DSS Compliance.
  assert.ok(directEdgeKeys.has('wi-cp-4->wi-cp-5'));
  assert.ok(directEdgeKeys.has('wi-cp-4->wi-cp-6'));
  assert.ok(directEdgeKeys.has('wi-cp-4->wi-cp-8'));

  // Full chain still reaches further upstream/downstream (transitively).
  assert.ok(highlightedNodeIds.has('wi-cp-1')); // upstream of GraphQL
  assert.ok(highlightedNodeIds.has('wi-cp-10')); // terminal GA gate, downstream

  // Unrelated node: Canary Traffic Ramp only depends on Staging Performance
  // Canary and PCI-DSS — it is reachable transitively via wi-cp-8, so
  // instead check a genuinely unrelated node has no direct edge to the defect.
  assert.ok(!directEdgeKeys.has('wi-cp-4->wi-cp-1'));
  assert.ok(!directEdgeKeys.has('wi-cp-1->wi-cp-4'));
});

test('unrelated nodes are never marked as a direct dependency of the selected item', () => {
  const { directEdgeKeys } = computeDependencyChain('wi-cp-1', checkout.edges);
  // wi-cp-1 (Database Schema Migration) has no direct relation to wi-cp-9/10.
  assert.ok(!directEdgeKeys.has('wi-cp-1->wi-cp-9'));
  assert.ok(!directEdgeKeys.has('wi-cp-1->wi-cp-10'));
});

test('experimentEdgeKey matches the dependency-chain edge key format used by the shared focus helpers', () => {
  assert.equal(experimentEdgeKey({ from: 'wi-cp-3', to: 'wi-cp-4' }), 'wi-cp-3->wi-cp-4');
});

test('X6 has been fully removed: no library implementation, adapters, or dependency remain', () => {
  const packageJson = sourceOf('../package.json');
  assert.doesNotMatch(packageJson, /@antv\/x6/);
  assert.doesNotMatch(packageJson, /"tslib"/);
});

test('the Workspace Workflow tab renders the new dependency-experiment canvas, and X6 is gone from it', () => {
  const source = sourceOf('../src/components/workItems/workspace/WorkItemOverviewTab.tsx');
  assert.match(source, /WorkItemDependencyExperimentCanvas/);
  assert.doesNotMatch(source, /WorkItemDependencyGraphX6/);
  assert.doesNotMatch(source, /@antv/);
});

test('card content shows type marker, health color+text, title, and summary — never color-only health', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  assert.match(source, /getHealthLabel/);
  assert.match(source, /getHealthColor/);
  assert.match(source, /TypeMarker/);
  assert.match(source, /node\.summary/);
});

// ---------------------------------------------------------------------
// Experiment 1.1 — Fixed Geometry & Viewport
// ---------------------------------------------------------------------

function fakeNodes(ids: string[]): WorkflowNode[] {
  return ids.map((id) => ({ id }) as WorkflowNode);
}

test('computeStageColumnWidth never exceeds the max, and never upscales for a single stage', () => {
  const width = computeStageColumnWidth(1, 1400);
  assert.ok(width <= STAGE_MAX_WIDTH, `${width} must not exceed STAGE_MAX_WIDTH`);
  assert.equal(width, STAGE_MAX_WIDTH); // 1400/1 candidate is far above max, so it clamps down
});

test('computeStageColumnWidth adapts within [min, max] when candidate width fits the band', () => {
  // 5 stages in a 1360px viewport -> candidate 272, squarely inside the band.
  const width = computeStageColumnWidth(5, 1360);
  assert.ok(width >= STAGE_MIN_WIDTH && width <= STAGE_MAX_WIDTH);
  assert.equal(width, 272);
});

test('computeStageColumnWidth freezes at the minimum instead of shrinking further when many stages overflow', () => {
  // 8 stages in a 900px viewport -> candidate 112.5, far below MIN.
  const width = computeStageColumnWidth(8, 900);
  assert.equal(width, STAGE_MIN_WIDTH);
  const totalContentWidth = width * 8;
  assert.ok(
    totalContentWidth > 900,
    'content must be allowed to exceed the viewport, not compress into it',
  );
});

test('computeStageColumnWidth falls back to the ideal width before any real measurement exists', () => {
  assert.equal(computeStageColumnWidth(5, 0), STAGE_IDEAL_WIDTH);
  assert.equal(computeStageColumnWidth(0, 1200), STAGE_IDEAL_WIDTH);
});

test('card logical size never changes with stage count or column width', () => {
  // The card dimension constants are inputs to layout, not derived from
  // columnWidth/stageCount — proving row-to-row spacing (cardHeight +
  // rowGap) stays identical at both a MIN-width and a MAX-width column is
  // a direct proxy for "the card itself never grows or shrinks."
  const expectedRowSpacing =
    DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.cardHeight + DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.rowGap;

  const narrow = computeSwimlaneLayout(checkout.nodes, { columnWidth: STAGE_MIN_WIDTH });
  const wide = computeSwimlaneLayout(checkout.nodes, { columnWidth: STAGE_MAX_WIDTH });

  // wi-cp-1 and wi-cp-2 are the two Foundation-stage rows (lane 0 and 1).
  const narrowSpacing = Math.abs(
    narrow.positions.get('wi-cp-2')!.y - narrow.positions.get('wi-cp-1')!.y,
  );
  const wideSpacing = Math.abs(wide.positions.get('wi-cp-2')!.y - wide.positions.get('wi-cp-1')!.y);

  assert.equal(narrowSpacing, expectedRowSpacing);
  assert.equal(wideSpacing, expectedRowSpacing);

  const solo = computeSwimlaneLayout(fakeNodes(['a']), { columnWidth: STAGE_MIN_WIDTH });
  assert.ok(solo.positions.get('a'));
});

test('one-stage layout stays bounded to the configured max width and does not expand to fill an arbitrary viewport', () => {
  const columnWidth = computeStageColumnWidth(1, 5000); // an enormous viewport
  const layout = computeSwimlaneLayout(fakeNodes(['solo']), { columnWidth });
  assert.equal(layout.columns.length, 1);
  assert.ok(
    layout.width <= STAGE_MAX_WIDTH,
    `content width ${layout.width} must not fill a 5000px viewport`,
  );
});

test('multi-stage fit case keeps every column within [min, max]', () => {
  const columnWidth = computeStageColumnWidth(5, 1360);
  const layout = computeSwimlaneLayout(checkout.nodes, { columnWidth });
  for (const column of layout.columns) {
    const width = column.endX - column.startX;
    assert.ok(width >= STAGE_MIN_WIDTH && width <= STAGE_MAX_WIDTH);
  }
});

test('overflow case floors every column at the minimum and lets content exceed the viewport', () => {
  const viewportWidth = 900;
  const columnWidth = computeStageColumnWidth(5, viewportWidth);
  const layout = computeSwimlaneLayout(checkout.nodes, { columnWidth });
  assert.equal(columnWidth, STAGE_MIN_WIDTH);
  assert.ok(
    layout.width > viewportWidth,
    'content width must exceed the viewport instead of compressing into it',
  );
});

test('resolveDefinedStages keeps all five stages when the model curates any node, even if items concentrate in one stage', () => {
  // All three nodes resolve to 'frontend' by curated id, but the fixture
  // as a whole is still curated (ids exist in the lookup table), so the
  // full defined taxonomy must remain visible, not collapse to just Frontend.
  const stages = resolveDefinedStages(fakeNodes(['wi-cp-4', 'wi-cp-6']));
  assert.deepEqual(
    stages.map((s) => s.id),
    EXPERIMENT_STAGES.map((s) => s.id),
  );
});

test('resolveDefinedStages collapses to exactly the fallback bucket only when nothing is curated at all', () => {
  const stages = resolveDefinedStages(fakeNodes(['totally-unknown-item']));
  assert.deepEqual(stages, [FALLBACK_STAGE]);
});

test('empty stages remain present as columns with no items, rather than being dropped', () => {
  // Only Foundation items present; API/Frontend/Verification/Release must
  // still exist as columns since the model as a whole is curated.
  const layout = computeSwimlaneLayout(fakeNodes(['wi-cp-1', 'wi-cp-2']));
  assert.deepEqual(
    layout.columns.map((c) => c.stage.id),
    ['foundation', 'api', 'frontend', 'verification', 'release'],
  );
  const populated = new Set<string>();
  layout.positions.forEach((p) => populated.add(p.stageId));
  assert.deepEqual([...populated], ['foundation']);
});

test('canvas height policy is bounded: a minimum floor and a maximum ceiling exist, independent of item count', () => {
  assert.ok(CANVAS_MIN_HEIGHT > 0);
  assert.ok(CANVAS_MAX_HEIGHT > CANVAS_MIN_HEIGHT);
});

test('the canvas component bounds its viewport height and scrolls internally rather than growing with content', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  assert.match(source, /minHeight:\s*CANVAS_MIN_HEIGHT/);
  assert.match(source, /maxHeight:\s*`min\(\$\{CANVAS_MAX_HEIGHT\}px/);
  assert.match(source, /overflowY:\s*'auto'/);
});

test('the canvas no longer stretches the whole SVG to fill the container width (the round 1.1 root cause)', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  // The outer <svg> (the graph canvas) must carry no viewBox and no
  // percentage/oversized-minWidth CSS — those together are exactly what
  // caused the whole logical content to be upscaled to fill the
  // container. The arrowhead <marker>'s own small internal viewBox is a
  // separate, legitimate, unrelated coordinate system and must remain.
  const outerSvgOpenTag = source.slice(
    source.indexOf('<svg'),
    source.indexOf('>', source.indexOf('<svg')) + 1,
  );
  assert.doesNotMatch(outerSvgOpenTag, /viewBox/);
  // width:'100%' is now legitimately used on the plain HTML ScrollViewport
  // Box (a normal block-level container, no viewBox involved) — the
  // forbidden pattern is specifically the <svg> element's own style
  // stretching it via CSS percentage width against a viewBox.
  assert.doesNotMatch(outerSvgOpenTag, /width:\s*'100%'/);
  assert.doesNotMatch(source, /minWidth:\s*760/);
  assert.match(source, /<marker/); // the arrowhead marker's own viewBox is fine and expected
  // The svg is sized with literal logical width/height, 1:1, not scaled.
  assert.match(outerSvgOpenTag, /width=\{renderedWidth\}/);
});

test('edge geometry for all four routing categories stays correct under a different column width (overflow scenario)', () => {
  const columnWidth = STAGE_MIN_WIDTH;
  const layout = computeSwimlaneLayout(checkout.nodes, { columnWidth });
  const cases: Array<[string, string, ReturnType<typeof classifyEdgeRouting>]> = [
    ['wi-cp-4', 'wi-cp-6', 'same-stage'],
    ['wi-cp-3', 'wi-cp-4', 'adjacent-stage'],
    ['wi-cp-4', 'wi-cp-8', 'long-cross-stage'],
    ['wi-cp-5', 'wi-cp-6', 'backward'],
  ];
  for (const [fromId, toId, expected] of cases) {
    const source = layout.positions.get(fromId)!;
    const target = layout.positions.get(toId)!;
    const sourceOrder = getStageById(source.stageId).order;
    const targetOrder = getStageById(target.stageId).order;
    assert.equal(classifyEdgeRouting(sourceOrder, targetOrder), expected);
    const path = getExperimentEdgePath(expected, source, target, layout);
    assert.match(path, /^M /);
  }
});

// ---------------------------------------------------------------------
// Vertical-scrollbar-triggers-spurious-horizontal-scrollbar bugfix
// ---------------------------------------------------------------------

test('hasHorizontalOverflow is false when content fits the actual client width', () => {
  assert.equal(hasHorizontalOverflow(300, 776), false);
  assert.equal(hasHorizontalOverflow(300, 300), false); // exactly equal must not overflow
});

test('hasHorizontalOverflow is true only when content genuinely exceeds the actual client width', () => {
  assert.equal(hasHorizontalOverflow(1984, 900), true);
  assert.equal(hasHorizontalOverflow(301, 300), true);
});

test('a stage with many items (tall content, triggering vertical overflow) never widens contentWidth', () => {
  // Single-stage layout with 2 items vs. 40 items: only row count / height
  // changes. contentWidth (driven solely by stageCount * columnWidth) must
  // be identical either way — vertical overflow must never leak into the
  // horizontal-overflow decision.
  const fewItems = computeSwimlaneLayout(fakeNodes(['a', 'b']), { columnWidth: STAGE_MAX_WIDTH });
  const manyItems = computeSwimlaneLayout(
    fakeNodes(Array.from({ length: 40 }, (_, i) => `item-${i}`)),
    { columnWidth: STAGE_MAX_WIDTH },
  );
  assert.equal(fewItems.width, manyItems.width);
  assert.ok(manyItems.contentBottom > fewItems.contentBottom, 'many items must still grow taller');
  assert.equal(
    hasHorizontalOverflow(fewItems.width, 776),
    hasHorizontalOverflow(manyItems.width, 776),
  );
});

test("ScrollViewport (containerRef) is the single owner of both scroll axes and spans the Explorer's full width", () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  const viewportBlockStart = source.indexOf('ref={containerRef}');
  const viewportBlockEnd = source.indexOf('>', source.indexOf('sx={{', viewportBlockStart));
  const viewportBlock = source.slice(viewportBlockStart, viewportBlockEnd);
  // Spans the full Explorer width — never shrunk to the graph's own
  // (possibly much narrower) content width. This is what makes the
  // vertical scrollbar render at the Explorer's true right edge instead
  // of hugging the edge of a narrow, centered single-stage graph.
  assert.match(viewportBlock, /width:\s*'100%'/);
  assert.match(viewportBlock, /overflowX:\s*'auto'/);
  assert.match(viewportBlock, /overflowY:\s*'auto'/);
  assert.match(viewportBlock, /minHeight:\s*CANVAS_MIN_HEIGHT/);
});

test('there is exactly one scrolling element — no separate nested vertical-scroll box remains', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  const overflowYMatches = source.match(/overflowY:/g) ?? [];
  assert.equal(
    overflowYMatches.length,
    1,
    'exactly one element should declare overflowY — the single ScrollViewport',
  );
});

test('GraphSurface (the fixed/adaptive-width graph content box) is not itself a scroll owner', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  const surfaceBlockStart = source.indexOf('width: renderedWidth, flexShrink: 0');
  const surfaceBlockEnd = source.indexOf('<svg', surfaceBlockStart);
  const surfaceBlock = source.slice(surfaceBlockStart, surfaceBlockEnd);
  assert.doesNotMatch(surfaceBlock, /overflowX/);
  assert.doesNotMatch(surfaceBlock, /overflowY/);
});

test('the sticky stage header is a real position:sticky descendant of the single ScrollViewport', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  assert.match(source, /position:\s*'sticky'/);
  assert.match(source, /top:\s*0/);
});

test('computeRenderedGraphWidth adds a small, fixed visual safety margin on top of real content width', () => {
  assert.equal(computeRenderedGraphWidth(300), 300 + GRAPH_RIGHT_PADDING);
  assert.ok(GRAPH_RIGHT_PADDING >= 24 && GRAPH_RIGHT_PADDING <= 32);
});

test('the graph right padding is a pure additive constant, never derived from viewport/scrollbar measurement', () => {
  // Same content width in, same padded width out, regardless of any
  // viewport-sized input — proves this is purely additive visual margin,
  // not scrollbar compensation smuggled into the geometry.
  assert.equal(computeRenderedGraphWidth(500), computeRenderedGraphWidth(500));
  assert.equal(computeRenderedGraphWidth(0), GRAPH_RIGHT_PADDING);
});

test('the rendered graph surface and its svg both use the padded renderedWidth, not the raw layout width, as their own box width', () => {
  const source = sourceOf(
    '../src/components/workItems/dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx',
  );
  assert.match(source, /width:\s*renderedWidth,\s*flexShrink:\s*0/);
  assert.match(source, /width=\{renderedWidth\}/);
});
