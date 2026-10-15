import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { app } from '../wailsjs/go/models.ts';
import { mapProposalDTO } from '../src/api/proposals/proposalDtoAdapter.ts';
import {
  isAggregateProposal,
  type ProductProposal,
} from '../src/api/proposals/proposalProductApi.ts';
import {
  proposalAppliedMessage,
  proposalRejectedMessage,
  proposalStaleMessage,
} from '../src/proposals/proposalActionMessages.ts';
import {
  beginProposalReview,
  proposalForReviewSession,
  proposalTargetNavigation,
} from '../src/proposals/proposalReviewSession.ts';
import { projectWorkItemGhost } from '../src/proposals/workItemGhostProjection.ts';
import type { ProductWorkItem } from '../src/api/workItems/productApi.ts';
import { loadComponent, renderMarkup, visibleText } from './support/renderComponent.ts';

// Produced by the Go Wails path (app.GetPendingProposals) and kept in sync by
// TestAggregateProposalReviewDTOMatchesFrontendFixture.
const fixtureDTO = (): app.ProposalDTO =>
  JSON.parse(readFileSync(new URL('./fixtures/aggregate-proposal-review.json', import.meta.url), 'utf8'));

const singularDTO = (): app.ProposalDTO =>
  ({
    id: 'singular-planner',
    targetWorkItemId: null,
    operationType: 'planner.item.create',
    title: 'Create planner item: Plain todo',
    summary: null,
    reason: 'You asked for a reminder.',
    status: 'PENDING',
    baseTitle: '',
    baseDescription: null,
    proposedTitle: '',
    proposedDescription: null,
    createdAt: '2030-01-01T08:00:00Z',
    updatedAt: '2030-01-01T08:00:00Z',
    resolvedAt: null,
    aiRunId: null,
    sourceRevisionIds: [],
    sourceExtensionIds: [],
    sourceProvenances: [],
    changes: [
      { label: 'Title', before: null, after: 'Plain todo' },
      { label: 'Due', before: null, after: '2030-01-14' },
    ],
    operations: [],
  }) as unknown as app.ProposalDTO;

type PanelProps = Record<string, unknown>;
const panelProps = (overrides: PanelProps): PanelProps => ({
  context: 'Work Items',
  input: '',
  inputRef: { current: null },
  proposals: [],
  proposalsLoading: false,
  proposalsError: null,
  activeProposal: null,
  proposalMutation: null,
  proposalActionError: null,
  proposalActionNotice: null,
  entries: [],
  submitting: false,
  onSend: async () => undefined,
  onClose: () => undefined,
  onInputChange: () => undefined,
  onBeginReview: () => undefined,
  onEndReview: () => undefined,
  onApplyProposal: async () => undefined,
  onRejectProposal: async () => undefined,
  onRetryProposals: () => undefined,
  ...overrides,
});

const panel = loadComponent<PanelProps>('src/components/AssistantPanel.tsx');
const aggregateReview = loadComponent<PanelProps>('src/components/proposals/AggregateProposalReview.tsx');

function count(haystack: string, needle: string): number {
  return haystack.split(needle).length - 1;
}

function operationItems(markup: string): string[] {
  return markup.split('data-testid="aggregate-operation"').slice(1);
}

test('the adapter maps aggregate actions in exact sequence order', () => {
  const proposal = mapProposalDTO(fixtureDTO());
  assert.equal(isAggregateProposal(proposal), true);
  assert.equal(proposal.targetWorkItemId, null);
  assert.deepEqual(proposal.changes, []);
  assert.deepEqual(
    proposal.operations.map((op) => [op.sequenceNo, op.label]),
    [
      [0, 'Create Work Item & Workflow'],
      [1, 'Create Tracked Item'],
      [2, 'Create Tracked Item'],
      [3, 'Create Todo'],
      [4, 'Create Todo'],
    ],
  );
  assert.deepEqual(proposal.operations[3].relations, [
    { label: 'Work Item', value: 'Created in action 1 — Infrastructure Tech', createdBySequenceNo: 0 },
    { label: 'Tracked Item', value: 'Created in action 2 — Database Sharding', createdBySequenceNo: 1 },
  ]);
  assert.deepEqual(proposal.operations[0].relations, []);
});

test('the adapter keeps singular proposals singular and rejects out-of-order actions', () => {
  const singular = mapProposalDTO(singularDTO());
  assert.equal(isAggregateProposal(singular), false);
  assert.deepEqual(singular.operations, []);
  assert.deepEqual(singular.changes, [
    { label: 'Title', before: null, after: 'Plain todo' },
    { label: 'Due', before: null, after: '2030-01-14' },
  ]);
  const legacy = singularDTO() as unknown as Record<string, unknown>;
  delete legacy.operations;
  assert.deepEqual(mapProposalDTO(legacy as unknown as app.ProposalDTO).operations, []);

  const shuffled = fixtureDTO();
  shuffled.operations = [shuffled.operations[1], shuffled.operations[0], ...shuffled.operations.slice(2)];
  assert.throws(() => mapProposalDTO(shuffled), /out of order/);
});

test('product fixture: the real review panel renders the whole aggregate plan for one decision', async () => {
  const proposal = mapProposalDTO(fixtureDTO());
  const markup = renderMarkup(await panel, panelProps({ proposals: [proposal], activeProposal: proposal }));
  const text = visibleText(markup);

  assert.match(markup, /data-testid="aggregate-proposal-review"/);
  assert.match(text, /Set up Infrastructure Tech and remind me about groceries/);
  assert.match(text, /Two engineering defects tracked under a new work stream/);
  assert.match(text, /Why You asked to set up Infrastructure Tech/);
  assert.match(text, /5 proposed actions/);

  const items = operationItems(markup);
  assert.equal(items.length, 5);
  assert.deepEqual(
    items.map((item) => visibleText(`<x ${item}`).split(' ').slice(0, 1)[0]),
    ['1', '2', '3', '4', '5'],
  );
  const itemText = items.map((item) => visibleText(`<x ${item}`));
  assert.match(itemText[0], /Create Work Item & Workflow.*Title Infrastructure Tech.*Workflow Standard Delivery.*Stages Design, Dev, Release/);
  assert.match(itemText[1], /Create Tracked Item.*Work Item Created in action 1 — Infrastructure Tech.*Tracked Item Database Sharding/);
  assert.match(itemText[2], /Create Tracked Item.*Work Item Created in action 1 — Infrastructure Tech.*Tracked Item Cache Invalidation/);
  assert.match(itemText[3], /Create Todo.*Tracked Item Created in action 2 — Database Sharding.*Title Review Sharding Design.*Reminder 3 days before due/);
  assert.match(itemText[4], /Create Todo.*Title Buy groceries.*Reminder At due time/);
  assert.doesNotMatch(itemText[4], /Work Item|Tracked Item/, 'the grocery Todo is unrelated to the work stream');

  assert.match(text, /either every action is applied or none is/);
  assert.equal(count(text, 'Apply all 5 actions'), 1);
  assert.equal(count(text, 'Reject all'), 1);
  for (const item of items) {
    assert.equal(count(item.split('</li>')[0], '<button'), 0, 'actions have no per-action controls');
  }
  assert.doesNotMatch(text, /\$op\[/);
  assert.doesNotMatch(text, /operation_kind|\{"/);
  assert.doesNotMatch(text, /Proposed Changes|Previewing/, 'the singular review is not rendered');
});

test('literal text that looks like a reference stays literal in the review', async () => {
  const dto = fixtureDTO();
  dto.operations[1].changes[0].after = 'Review $op[0].work_item_id';
  const proposal = mapProposalDTO(dto);
  const text = visibleText(renderMarkup(await panel, panelProps({ activeProposal: proposal })));
  assert.match(text, /Tracked Item Review \$op\[0\]\.work_item_id/);
  assert.match(text, /Created in action 1 — Infrastructure Tech/);
});

test('the singular review is unchanged for singular proposals', async () => {
  const proposal = mapProposalDTO(singularDTO());
  const markup = renderMarkup(await panel, panelProps({ activeProposal: proposal }));
  const text = visibleText(markup);
  assert.doesNotMatch(markup, /aggregate-proposal-review|aggregate-operation/);
  assert.match(text, /Previewing/);
  assert.match(text, /Proposed Changes Title Not set Plain todo Due Not set 2030-01-14/);
  assert.match(text, /Reject Apply$/);
  assert.doesNotMatch(text, /Apply all|Reject all|proposed actions/);
});

test('the Needs Review list labels aggregate rows by action count and keeps singular hints', async () => {
  const aggregate = mapProposalDTO(fixtureDTO());
  const singular = mapProposalDTO(singularDTO());
  const text = visibleText(renderMarkup(await panel, panelProps({ proposals: [aggregate, singular] })));
  assert.match(text, /5 actions · Review →/);
  assert.match(text, /Planner · Review →/);
});

test('apply in progress and apply failure keep the aggregate review intact', async () => {
  const proposal = mapProposalDTO(fixtureDTO());
  const review = await aggregateReview;
  const base = { proposal, onBack: () => undefined, onApply: async () => undefined, onReject: async () => undefined };

  const applying = renderMarkup(review, { ...base, mutation: 'apply', error: null });
  assert.match(visibleText(applying), /Applying…/);
  assert.equal(count(applying, 'disabled=""'), 3, 'back, reject and apply are disabled while applying');

  const failed = renderMarkup(review, { ...base, mutation: null, error: 'database is locked' });
  const failedText = visibleText(failed);
  assert.match(failedText, /Proposal action failed: database is locked\. Review remains active so you can retry\./);
  assert.equal(operationItems(failed).length, 5);
  assert.equal(count(failed, 'disabled=""'), 0);
  assert.match(failedText, /Apply all 5 actions/);
});

test('aggregate review never navigates to a guessed Work Item', () => {
  const aggregate = mapProposalDTO(fixtureDTO());
  for (const page of ['dashboard', 'planner', 'workItems']) {
    assert.equal(proposalTargetNavigation(page, null, aggregate), 'NONE');
    assert.equal(proposalTargetNavigation(page, 'wi-1', aggregate), 'NONE');
  }
  // Even if an aggregate ever carried a target hint, review does not pick one Work Item.
  const hinted: ProductProposal = { ...aggregate, targetWorkItemId: 'wi-1' };
  assert.equal(proposalTargetNavigation('planner', null, hinted), 'NONE');

  const singular: ProductProposal = { ...mapProposalDTO(singularDTO()), targetWorkItemId: 'wi-1', operationType: 'work_item.update_details' };
  assert.equal(proposalTargetNavigation('planner', null, singular), 'OPEN_TARGET');
  assert.equal(proposalTargetNavigation('workItems', 'wi-2', singular), 'SELECT_TARGET');
  assert.equal(proposalTargetNavigation('workItems', 'wi-1', singular), 'NONE');
});

test('aggregate outcome notices describe the whole proposal and singular notices are unchanged', () => {
  assert.equal(proposalAppliedMessage('proposal.aggregate', 5), 'Proposal applied: all of its 5 actions were applied together.');
  assert.equal(proposalStaleMessage('proposal.aggregate', 5), 'This proposal is no longer current. None of its 5 actions were applied.');
  assert.equal(proposalRejectedMessage('proposal.aggregate', 5), 'Proposal rejected. None of its 5 actions were applied.');
  assert.equal(proposalAppliedMessage('planner.item.create'), 'Proposal applied to the Planner.');
  assert.equal(proposalRejectedMessage('tracked_item.create'), 'Proposal rejected. Tracking was not changed.');
  assert.equal(proposalStaleMessage('work_item.update_details'), 'This proposal is no longer current because the WorkItem changed. Nothing was applied.');
});

test('the shell passes aggregate action counts and keeps the singular apply/reject flow', () => {
  const shell = readFileSync(new URL('../src/components/AppShell.tsx', import.meta.url), 'utf8');
  assert.match(shell, /proposalAppliedMessage\(operationType, actionCount\)/);
  assert.match(shell, /proposalStaleMessage\(operationType, actionCount\)/);
  assert.match(shell, /proposalRejectedMessage\(\s*activeProposal\.operationType,\s*activeProposal\.operations\?\.length \?\? 0,?\s*\)/);
  // Reject and Apply still close review and refresh through one reconcile path; failure keeps review open.
  assert.equal(count(shell, 'await reconcileTerminalProposalAction('), 2);
  assert.equal(count(shell, 'setProposalActionError(error instanceof Error ? error.message : String(error));'), 2);
});

test('an aggregate proposal survives a re-fetch with its actions intact', () => {
  const first = mapProposalDTO(fixtureDTO());
  const session = beginProposalReview(first);
  const refetched = [mapProposalDTO(singularDTO()), mapProposalDTO(JSON.parse(JSON.stringify(fixtureDTO())))];
  const reviewed = proposalForReviewSession(refetched, session);
  assert.ok(reviewed);
  assert.deepEqual(reviewed, first);
});

test('aggregate proposals never trigger the singular Work Item ghost projection', () => {
  const item = { id: 'wi-1', title: 'Payments', description: 'Card rails' } as ProductWorkItem;
  const aggregate = mapProposalDTO(fixtureDTO());
  const projected = projectWorkItemGhost(item, aggregate);
  assert.deepEqual(projected.title, { value: 'Payments', isProposed: false });
  assert.deepEqual(projected.description, { value: 'Card rails', isProposed: false });
  const hinted = projectWorkItemGhost(item, { ...aggregate, targetWorkItemId: 'wi-1' });
  assert.equal(hinted.title.isProposed || hinted.description.isProposed, false);

  const update: ProductProposal = {
    ...mapProposalDTO(singularDTO()),
    operationType: 'work_item.update_details',
    targetWorkItemId: 'wi-1',
    proposedTitle: 'Payments v2',
    proposedDescription: 'Card rails',
  };
  assert.deepEqual(projectWorkItemGhost(item, update).title, { value: 'Payments v2', isProposed: true });
});
