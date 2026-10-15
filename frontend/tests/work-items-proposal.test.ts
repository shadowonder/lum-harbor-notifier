import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';
import {
  mapProposalDTO,
  mapApplyProposalResultDTO,
} from '../src/api/proposals/proposalDtoAdapter.ts';
import {
  proposeWorkItemUpdate,
  applyProposal,
  rejectProposal,
  type ProductProposal,
  type ProposalProductApi,
} from '../src/api/proposals/proposalProductApi.ts';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const mockWorkItem: ProductWorkItem = {
  id: '0199-backend-item',
  title: 'Current Authoritative Title',
  description: 'Current Authoritative Description',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-18T10:00:00Z',
  updatedAt: '2026-09-18T10:00:00Z',
};

const mockProposalDTO = {
  id: 'prop-123',
  targetWorkItemId: '0199-backend-item',
  operationType: 'work_item.update_details',
  title: 'Update work item details',
  summary: 'Suggested title refinement',
  reason: 'Clarify scope for sprint planning',
  status: 'PENDING',
  baseTitle: 'Initial Base Title',
  baseDescription: 'Initial Base Description',
  proposedTitle: 'Proposed Better Title',
  proposedDescription: 'Proposed Better Description',
  createdAt: '2026-09-19T10:00:00Z',
  updatedAt: '2026-09-19T10:00:00Z',
  resolvedAt: undefined,
};

test('generated Proposal DTO maps cleanly into ProductProposal model with date validation', () => {
  const proposal = mapProposalDTO(mockProposalDTO);

  assert.equal(proposal.id, 'prop-123');
  assert.equal(proposal.targetWorkItemId, '0199-backend-item');
  assert.equal(proposal.operationType, 'work_item.update_details');
  assert.equal(proposal.status, 'PENDING');
  assert.equal(proposal.baseTitle, 'Initial Base Title');
  assert.equal(proposal.baseDescription, 'Initial Base Description');
  assert.equal(proposal.proposedTitle, 'Proposed Better Title');
  assert.equal(proposal.proposedDescription, 'Proposed Better Description');
  assert.equal(proposal.reason, 'Clarify scope for sprint planning');
  assert.equal(proposal.resolvedAt, null);
  assert.equal(proposal.createdAt, '2026-09-19T10:00:00Z');
  assert.equal(proposal.updatedAt, '2026-09-19T10:00:00Z');
});

test('mapProposalDTO rejects unsupported proposal status or invalid ISO dates', () => {
  assert.throws(
    () => mapProposalDTO({ ...mockProposalDTO, status: 'INVALID_STATUS' }),
    /Unsupported Proposal status: INVALID_STATUS/,
  );
  assert.throws(
    () => mapProposalDTO({ ...mockProposalDTO, createdAt: 'not-a-date' }),
    /Invalid Proposal createdAt: not-a-date/,
  );
});

test('mapApplyProposalResultDTO maps APPLIED outcome with authoritative updated workItem', () => {
  const result = mapApplyProposalResultDTO({
    outcome: 'APPLIED',
    proposal: { ...mockProposalDTO, status: 'APPLIED', resolvedAt: '2026-09-19T12:00:00Z' },
    workItem: {
      id: '0199-backend-item',
      title: 'Proposed Better Title',
      description: 'Proposed Better Description',
      lifecycleState: 'ACTIVE',
      completedAt: undefined,
      createdAt: '2026-09-18T10:00:00Z',
      updatedAt: '2026-09-19T12:00:00Z',
    },
  });

  assert.equal(result.outcome, 'APPLIED');
  assert.equal(result.proposal.status, 'APPLIED');
  assert.equal(result.proposal.resolvedAt, '2026-09-19T12:00:00Z');
  assert.ok(result.workItem);
  assert.equal(result.workItem.title, 'Proposed Better Title');
  assert.equal(result.workItem.description, 'Proposed Better Description');
});

test('mapApplyProposalResultDTO handles STALE outcome with null workItem safely', () => {
  const result = mapApplyProposalResultDTO({
    outcome: 'STALE',
    proposal: { ...mockProposalDTO, status: 'STALE', resolvedAt: '2026-09-19T12:00:00Z' },
    workItem: undefined,
  });

  assert.equal(result.outcome, 'STALE');
  assert.equal(result.proposal.status, 'STALE');
  assert.equal(result.workItem, null);
});

test('mapApplyProposalResultDTO handles ALREADY_APPLIED outcome cleanly', () => {
  const result = mapApplyProposalResultDTO({
    outcome: 'ALREADY_APPLIED',
    proposal: { ...mockProposalDTO, status: 'APPLIED', resolvedAt: '2026-09-19T12:00:00Z' },
    workItem: undefined,
  });

  assert.equal(result.outcome, 'ALREADY_APPLIED');
  assert.equal(result.proposal.status, 'APPLIED');
  assert.equal(result.workItem, null);
});

test('proposals association and counting by targetWorkItemId', () => {
  const proposal1: ProductProposal = {
    ...mapProposalDTO(mockProposalDTO),
    id: 'prop-1',
    targetWorkItemId: 'wi-alpha',
  };
  const proposal2: ProductProposal = {
    ...mapProposalDTO(mockProposalDTO),
    id: 'prop-2',
    targetWorkItemId: 'wi-alpha',
  };
  const proposal3: ProductProposal = {
    ...mapProposalDTO(mockProposalDTO),
    id: 'prop-3',
    targetWorkItemId: 'wi-beta',
  };

  const allProposals = [proposal1, proposal2, proposal3];
  const countMap = new Map<string, number>();
  for (const p of allProposals) {
    countMap.set(p.targetWorkItemId, (countMap.get(p.targetWorkItemId) ?? 0) + 1);
  }

  assert.equal(countMap.get('wi-alpha'), 2);
  assert.equal(countMap.get('wi-beta'), 1);
  assert.equal(countMap.get('wi-gamma') ?? 0, 0);

  const alphaProposals = allProposals.filter((p) => p.targetWorkItemId === 'wi-alpha');
  assert.equal(alphaProposals.length, 2);
  assert.deepEqual(
    alphaProposals.map((p) => p.id),
    ['prop-1', 'prop-2'],
  );
});

test('WorkItemRow renders suggestion indicator only when count > 0 (zero false indicators)', () => {
  const rowSource = sourceOf('../src/components/workItems/WorkItemRow.tsx');
  // Confirm count check strictly requires pendingProposalCount > 0
  assert.match(rowSource, /pendingProposalCount !== undefined && pendingProposalCount > 0/);
  assert.match(rowSource, /1 suggestion/);
  assert.match(rowSource, /\$\{pendingProposalCount\} suggestions/);
});

test('proposeWorkItemUpdate client validator rejects blank proposed title', async () => {
  const api: ProposalProductApi = {
    getPendingProposals: async () => [],
    proposeWorkItemUpdate: async () => mapProposalDTO(mockProposalDTO),
    applyProposal: async () => ({
      outcome: 'APPLIED',
      proposal: mapProposalDTO(mockProposalDTO),
      workItem: mockWorkItem,
    }),
    rejectProposal: async () => mapProposalDTO(mockProposalDTO),
  };

  await assert.rejects(
    () => proposeWorkItemUpdate(api, { targetWorkItemId: 'wi-1', proposedTitle: '   ' }),
    /Proposed title is required/,
  );
  await assert.rejects(
    () => proposeWorkItemUpdate(api, { targetWorkItemId: '   ', proposedTitle: 'Title' }),
    /Target Work Item ID is required/,
  );
});

test('applyProposal and rejectProposal validate proposal ID before delegating', async () => {
  const api: ProposalProductApi = {
    getPendingProposals: async () => [],
    proposeWorkItemUpdate: async () => mapProposalDTO(mockProposalDTO),
    applyProposal: async () => ({
      outcome: 'APPLIED',
      proposal: mapProposalDTO(mockProposalDTO),
      workItem: mockWorkItem,
    }),
    rejectProposal: async () => mapProposalDTO(mockProposalDTO),
  };

  await assert.rejects(() => applyProposal(api, '   '), /Proposal ID is required/);
  await assert.rejects(() => rejectProposal(api, '   '), /Proposal ID is required/);
});

test('WorkItemWorkspace has no direct edit and exposes Proposal creation only as a dev tool', () => {
  const workspaceSource = sourceOf('../src/components/workItems/workspace/WorkItemWorkspace.tsx');
  const pageSource = sourceOf('../src/pages/WorkItems.tsx');
  assert.ok(!workspaceSource.includes('Edit Info'));
  assert.ok(!workspaceSource.includes('Suggest Change'));
  assert.ok(workspaceSource.includes('Create Test Proposal (DEV)'));
  assert.match(pageSource, /import\.meta\.env\.DEV && Boolean\(onCreateProposal\)/);
  // Retired M7 review panel must not remain as a second product surface.
  assert.ok(!workspaceSource.includes('WorkItemProposalReviewPanel'));
  // Invariant: no Drawer or Modal
  assert.doesNotMatch(workspaceSource, /\b(Drawer|Modal)\b/);
});

test('WorkItems consumes AppShell Proposal truth and retains a dev-only real Proposal producer', () => {
  const pageSource = sourceOf('../src/pages/WorkItems.tsx');
  assert.ok(!pageSource.includes('wailsProposalProductApi'));
  assert.ok(!pageSource.includes('proposalApi.getPendingProposals()'));
  assert.ok(pageSource.includes('proposals = []'));
  assert.ok(pageSource.includes('onCreateProposal'));
  // Independent error isolation: failure to load proposals sets warning without wiping WorkItems
  assert.ok(pageSource.includes('proposalsError'));
  assert.ok(pageSource.includes('Could not load suggestions'));
  assert.ok(pageSource.includes('Work Items remain fully functional'));
  // The development tool still uses the real Proposal dialog/producer path.
  assert.ok(pageSource.includes('WorkItemSuggestChangeDialog'));
  assert.ok(pageSource.includes('handleSuggestSubmit'));
});

test('Proposal components respect strict frontend transport boundaries (no wailsjs/go/models imports)', () => {
  for (const relativePath of [
    '../src/pages/WorkItems.tsx',
    '../src/components/workItems/WorkItemRow.tsx',
    '../src/components/workItems/workspace/WorkItemWorkspace.tsx',
    '../src/components/workItems/WorkItemSuggestChangeDialog.tsx',
    '../src/api/proposals/proposalProductApi.ts',
  ]) {
    const source = sourceOf(relativePath);
    assert.ok(
      !source.includes('wailsjs/go/models'),
      `${relativePath} should not import directly from wailsjs/go/models`,
    );
  }
});
