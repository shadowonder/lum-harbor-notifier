import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { existsSync } from 'node:fs';
import test from 'node:test';
import type { ProductProposal } from '../src/api/proposals/proposalProductApi.ts';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';
import { projectWorkItemGhost } from '../src/proposals/workItemGhostProjection.ts';

const item: ProductWorkItem = {
  id: 'wi-1',
  title: 'Authoritative title',
  description: 'Authoritative description',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-18T10:00:00Z',
  updatedAt: '2026-09-18T10:00:00Z',
};

function proposal(overrides: Partial<ProductProposal> = {}): ProductProposal {
  return {
    id: 'proposal-1',
    targetWorkItemId: 'wi-1',
    operationType: 'work_item.update_details',
    title: 'Update details',
    summary: null,
    reason: null,
    status: 'PENDING',
    baseTitle: item.title,
    baseDescription: item.description,
    proposedTitle: 'Proposed title',
    proposedDescription: 'Proposed description',
    createdAt: '2026-09-19T10:00:00Z',
    updatedAt: '2026-09-19T10:00:00Z',
    resolvedAt: null,
    ...overrides,
  };
}

test('authoritative A plus proposal B renders B as proposed without mutating truth', () => {
  const before = structuredClone(item);
  const projected = projectWorkItemGhost(item, proposal());
  assert.deepEqual(projected.title, { value: 'Proposed title', isProposed: true });
  assert.deepEqual(projected.description, { value: 'Proposed description', isProposed: true });
  assert.deepEqual(item, before);
});

test('Exit Preview restores authoritative fields immediately', () => {
  assert.deepEqual(projectWorkItemGhost(item, null), {
    title: { value: item.title, isProposed: false },
    description: { value: item.description, isProposed: false },
    supported: true,
  });
});

test('only a changed title gets proposed treatment', () => {
  const projected = projectWorkItemGhost(item, proposal({ proposedDescription: item.description }));
  assert.equal(projected.title.isProposed, true);
  assert.equal(projected.description.isProposed, false);
});

test('only a changed description gets proposed treatment', () => {
  const projected = projectWorkItemGhost(item, proposal({ proposedTitle: item.title }));
  assert.equal(projected.title.isProposed, false);
  assert.equal(projected.description.isProposed, true);
});

test('unchanged fields render as normal authoritative presentation', () => {
  const projected = projectWorkItemGhost(
    item,
    proposal({ proposedTitle: item.title, proposedDescription: item.description }),
  );
  assert.equal(projected.title.isProposed, false);
  assert.equal(projected.description.isProposed, false);
});

test('an unrelated selected WorkItem never receives ghost values', () => {
  const other = { ...item, id: 'wi-other' };
  const projected = projectWorkItemGhost(other, proposal());
  assert.equal(projected.title.value, item.title);
  assert.equal(projected.title.isProposed, false);
  assert.equal(projected.description.isProposed, false);
});

test('returning to the target derives the same ghost from the same active proposal', () => {
  const active = proposal();
  assert.equal(projectWorkItemGhost({ ...item, id: 'wi-other' }, active).title.isProposed, false);
  assert.deepEqual(projectWorkItemGhost(item, active), projectWorkItemGhost(item, active));
});

test('unsupported target operation renders authoritative values and reports unsupported', () => {
  const unsupported = {
    ...proposal(),
    operationType: 'planner.update' as ProductProposal['operationType'],
  };
  const projected = projectWorkItemGhost(item, unsupported);
  assert.equal(projected.supported, false);
  assert.deepEqual(projected.title, { value: item.title, isProposed: false });
  assert.deepEqual(projected.description, { value: item.description, isProposed: false });
});

test('production workspace renders changed fields in-place and old review panel is retired', () => {
  const workspaceURL = new URL(
    '../src/components/workItems/workspace/WorkItemWorkspace.tsx',
    import.meta.url,
  );
  const workspace = readFileSync(workspaceURL, 'utf8');
  assert.match(workspace, /projectWorkItemGhost\(item, activeProposal\)/);
  assert.match(workspace, /data-testid="work-item-title-preview"/);
  assert.match(workspace, /data-testid="work-item-description-preview"/);
  assert.match(workspace, /label="PROPOSED"/);
  assert.doesNotMatch(workspace, /WorkItemProposalReviewPanel|onApplyProposal|onRejectProposal/);
  assert.equal(
    existsSync(
      new URL(
        '../src/components/workItems/workspace/WorkItemProposalReviewPanel.tsx',
        import.meta.url,
      ),
    ),
    false,
  );
});
