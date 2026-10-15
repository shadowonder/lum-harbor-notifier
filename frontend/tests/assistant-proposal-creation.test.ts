import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import {
  assistantKindLabel,
  sendAssistantMessage,
} from '../src/assistant/assistantConversation.ts';
import type { ProductProposal } from '../src/api/proposals/proposalProductApi.ts';
import { handoffCreatedProposal } from '../src/proposals/proposalHandoff.ts';
import {
  beginProposalReview,
  proposalForReviewSession,
  proposalTargetNavigation,
  type ProposalReviewSession,
} from '../src/proposals/proposalReviewSession.ts';
import {
  mapAssistantResult,
  type AssistantApi,
  type AssistantResult,
} from '../src/api/assistant/assistantApi.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const mutation = (over: Partial<AssistantResult>): AssistantResult => ({
  kind: 'MUTATION_INTENT',
  text: 'x',
  contextLabel: 'Q4',
  changeApplied: false,
  proposalOutcome: '',
  proposalId: '',
  ...over,
});

const apiReturning = (result: AssistantResult): AssistantApi => ({ ask: async () => result });

test('DTO mapping carries the real proposal ID and validates the outcome vocabulary', () => {
  const mapped = mapAssistantResult({
    kind: 'MUTATION_INTENT',
    text: 't',
    contextLabel: 'c',
    changeApplied: false,
    proposalOutcome: 'CREATED',
    proposalId: 'p-1',
  });
  assert.equal(mapped.proposalId, 'p-1');
  assert.equal(mapped.proposalOutcome, 'CREATED');
  assert.throws(() =>
    mapAssistantResult({
      kind: 'MUTATION_INTENT',
      text: 't',
      contextLabel: 'c',
      changeApplied: false,
      proposalOutcome: 'APPLIED',
    }),
  );
});

test('Proposal creation is never reported as an applied change', async () => {
  const entries = await sendAssistantMessage(
    apiReturning(mutation({ proposalOutcome: 'CREATED', proposalId: 'p-1' })),
    'Rename it',
    'wi-1',
    async () => [],
  );
  const reply = entries[1];
  assert.equal(reply.role === 'assistant' && reply.changeApplied, false);
  assert.match(assistantKindLabel('MUTATION_INTENT', 'CREATED') ?? '', /Proposal created/);
  assert.match(assistantKindLabel('MUTATION_INTENT', 'CREATED') ?? '', /nothing was changed/);
});

test('CREATED refreshes the authoritative collection exactly once with the real proposalId', async () => {
  const seen: string[] = [];
  const entries = await sendAssistantMessage(
    apiReturning(mutation({ proposalOutcome: 'CREATED', proposalId: 'p-9' })),
    'Rename it',
    'wi-1',
    async (id) => {
      seen.push(id);
    },
  );
  assert.deepEqual(seen, ['p-9']);
  assert.equal(entries.length, 2);
});

test('a post-create refresh failure is distinct from a creation failure and does not resend', async () => {
  let asks = 0;
  const api: AssistantApi = {
    ask: async () => {
      asks += 1;
      return mutation({ proposalOutcome: 'CREATED', proposalId: 'p-1' });
    },
  };
  const entries = await sendAssistantMessage(api, 'Rename it', 'wi-1', async () => {
    throw new Error('boom');
  });
  assert.equal(asks, 1);
  assert.equal(entries[1].role, 'assistant');
  const notice = entries[2];
  assert.equal(notice.role, 'error');
  assert.match(notice.text, /Proposal was created, but the review view could not be loaded/);
  assert.doesNotMatch(notice.text, /failed to create|could not create/i);
});

test('NO_CHANGE, CONTEXT_CHANGED and plain results never trigger a refresh', async () => {
  for (const outcome of ['NO_CHANGE', 'CONTEXT_CHANGED', ''] as const) {
    let refreshed = 0;
    await sendAssistantMessage(
      apiReturning(mutation({ proposalOutcome: outcome })),
      'x',
      'wi-1',
      async () => {
        refreshed += 1;
      },
    );
    assert.equal(refreshed, 0, outcome);
  }
  assert.match(
    assistantKindLabel('MUTATION_INTENT', 'CONTEXT_CHANGED') ?? '',
    /no Proposal created/,
  );
});

const proposal = (id: string, target = 'wi-1'): ProductProposal => ({
  id,
  targetWorkItemId: target,
  operationType: 'work_item.update_details',
  title: `Proposal ${id}`,
  summary: null,
  reason: null,
  status: 'PENDING',
  baseTitle: 'Before',
  baseDescription: null,
  proposedTitle: 'After',
  proposedDescription: null,
  createdAt: '2026-09-19T10:00:00Z',
  updatedAt: '2026-09-19T10:00:00Z',
  resolvedAt: null,
});

/** Mirrors AppShell.beginReview using the same real helpers. */
function shellHarness(opts: {
  pending: ProductProposal[] | Error;
  page?: string;
  selected?: string | null;
  open?: boolean;
  beginThrows?: boolean;
}) {
  const state = {
    session: null as ProposalReviewSession | null,
    navigations: [] as string[],
    refreshes: 0,
    begun: [] as ProductProposal[],
  };
  const deps = {
    refreshPending: async () => {
      state.refreshes += 1;
      if (opts.pending instanceof Error) throw opts.pending;
      return opts.pending;
    },
    beginReview: (p: ProductProposal) => {
      if (opts.beginThrows) throw new Error('navigation failed');
      state.begun.push(p);
      state.navigations.push(
        proposalTargetNavigation(opts.page ?? 'dashboard', opts.selected ?? null, p),
      );
      state.session = beginProposalReview(p);
    },
    isAssistantOpen: () => opts.open ?? true,
  };
  return { state, deps };
}

function createdApi(proposalId: string) {
  const calls = { asks: 0 };
  const api: AssistantApi = {
    ask: async () => {
      calls.asks += 1;
      return mutation({ proposalOutcome: 'CREATED', proposalId });
    },
  };
  return { api, calls };
}

test('CREATED: authoritative refresh, then existing review begins for the exact refreshed Proposal', async () => {
  const refreshed = proposal('p-1', 'wi-7');
  const { state, deps } = shellHarness({ pending: [proposal('other'), refreshed] });
  const { api, calls } = createdApi('p-1');
  const entries = await sendAssistantMessage(api, 'Rename it', 'wi-7', (id) =>
    handoffCreatedProposal(deps, id),
  );
  assert.equal(state.refreshes, 1);
  assert.equal(state.begun[0], refreshed, 'must be the object from the authoritative collection');
  assert.equal(state.session?.proposalId, 'p-1');
  assert.deepEqual(state.session, { proposalId: 'p-1' });
  assert.equal(proposalForReviewSession([refreshed], state.session)?.targetWorkItemId, 'wi-7');
  assert.equal(entries.length, 2, 'no error entry on a successful handoff');
  assert.equal(calls.asks, 1);
});

test('review starts only after the refresh resolves, never from the Assistant result', async () => {
  const order: string[] = [];
  const refreshed = proposal('p-1');
  const result = await handoffCreatedProposal(
    {
      refreshPending: async () => {
        order.push('refresh');
        return [refreshed];
      },
      beginReview: () => order.push('begin'),
      isAssistantOpen: () => true,
    },
    'p-1',
  );
  assert.equal(result, 'STARTED');
  assert.deepEqual(order, ['refresh', 'begin']);
});

test('target navigation follows the authoritative Proposal target and no-ops when already there', async () => {
  const target = proposal('p-1', 'wi-1');
  const cases: Array<[string, string | null, string]> = [
    ['dashboard', null, 'OPEN_TARGET'],
    ['planner', 'wi-1', 'OPEN_TARGET'],
    ['workItems', 'wi-other', 'SELECT_TARGET'],
    ['workItems', 'wi-1', 'NONE'],
  ];
  for (const [page, selected, expected] of cases) {
    const { state, deps } = shellHarness({ pending: [target], page, selected });
    await handoffCreatedProposal(deps, 'p-1');
    assert.deepEqual(state.navigations, [expected], `${page}/${selected}`);
  }
});

test('an AI handoff produces the same identity-only session as a Needs Review click', async () => {
  const target = proposal('p-1');
  const { state, deps } = shellHarness({ pending: [target] });
  await handoffCreatedProposal(deps, 'p-1');
  assert.deepEqual(state.session, beginProposalReview(target));
});

test('refresh failure: no review, no fake Proposal, no resend, honest message', async () => {
  const { state, deps } = shellHarness({ pending: new Error('db down') });
  const { api, calls } = createdApi('p-1');
  const entries = await sendAssistantMessage(api, 'Rename it', 'wi-1', (id) =>
    handoffCreatedProposal(deps, id),
  );
  assert.equal(state.begun.length, 0);
  assert.equal(state.session, null);
  assert.equal(calls.asks, 1);
  const notice = entries[2];
  assert.equal(
    notice.role === 'error' && notice.text,
    'Proposal was created, but the review view could not be loaded.',
  );
});

test('missing after a successful refresh: no review, no synthesized Proposal, no resend', async () => {
  const { state, deps } = shellHarness({ pending: [proposal('someone-else')] });
  const { api, calls } = createdApi('p-1');
  const entries = await sendAssistantMessage(api, 'Rename it', 'wi-1', (id) =>
    handoffCreatedProposal(deps, id),
  );
  assert.equal(state.begun.length, 0);
  assert.equal(state.session, null);
  assert.equal(calls.asks, 1);
  const notice = entries[2];
  assert.match(
    notice.role === 'error' ? notice.text : '',
    /not currently available in the review list/,
  );
});

test('review-start/navigation failure keeps the Proposal, resends nothing, and shows an honest message', async () => {
  const { state, deps } = shellHarness({ pending: [proposal('p-1')], beginThrows: true });
  const { api, calls } = createdApi('p-1');
  const entries = await sendAssistantMessage(api, 'Rename it', 'wi-1', (id) =>
    handoffCreatedProposal(deps, id),
  );
  assert.equal(state.session, null);
  assert.equal(calls.asks, 1);
  assert.equal(entries[2].role, 'error');
});

test('Assistant closed while the request was in flight: refresh happens, review is not forced', async () => {
  const { state, deps } = shellHarness({ pending: [proposal('p-1')], open: false });
  assert.equal(await handoffCreatedProposal(deps, 'p-1'), 'SKIPPED');
  assert.equal(state.refreshes, 1);
  assert.equal(state.begun.length, 0);
});

test('non-created outcomes never refresh or begin review and leave an active session alone', async () => {
  const cases: AssistantResult[] = [
    mutation({ proposalOutcome: 'NO_CHANGE' }),
    mutation({ proposalOutcome: 'CONTEXT_CHANGED' }),
    { ...mutation({}), kind: 'ANSWER' },
    { ...mutation({}), kind: 'NEEDS_CONTEXT' },
    { ...mutation({}), kind: 'UNSUPPORTED' },
  ];
  for (const result of cases) {
    const { state, deps } = shellHarness({ pending: [proposal('p-1')] });
    state.session = { proposalId: 'active' };
    await sendAssistantMessage(apiReturning(result), 'x', 'wi-1', (id) =>
      handoffCreatedProposal(deps, id),
    );
    assert.equal(state.refreshes, 0);
    assert.deepEqual(state.session, { proposalId: 'active' });
  }
  for (const failure of [new Error('runtime'), new Error('malformed')]) {
    const { state, deps } = shellHarness({ pending: [proposal('p-1')] });
    const entries = await sendAssistantMessage(
      { ask: async () => Promise.reject(failure) },
      'x',
      'wi-1',
      (id) => handoffCreatedProposal(deps, id),
    );
    assert.equal(entries[1].role, 'error');
    assert.equal(state.refreshes, 0);
  }
});

test('beginning B replaces session A without touching either Proposal (M9A semantics)', async () => {
  const { state, deps } = shellHarness({ pending: [proposal('a'), proposal('b')] });
  state.session = beginProposalReview(proposal('a'));
  await handoffCreatedProposal(deps, 'b');
  assert.deepEqual(state.session, { proposalId: 'b' });
});

test('CREATED without a proposalId is rejected by the adapter; other outcomes need none', () => {
  assert.throws(() =>
    mapAssistantResult({
      kind: 'MUTATION_INTENT',
      text: 't',
      contextLabel: 'c',
      changeApplied: false,
      proposalOutcome: 'CREATED',
      proposalId: '',
    }),
  );
  for (const outcome of ['NO_CHANGE', 'CONTEXT_CHANGED', '']) {
    assert.doesNotThrow(() =>
      mapAssistantResult({
        kind: 'MUTATION_INTENT',
        text: 't',
        contextLabel: 'c',
        changeApplied: false,
        proposalOutcome: outcome,
        proposalId: '',
      }),
    );
  }
});

test('AppShell reuses the single beginReview entry and owns no AI-specific review state', () => {
  const shell = source('components/AppShell.tsx');
  assert.match(shell, /handoffCreatedProposal\(/);
  assert.match(shell, /beginReview: \(proposal\) => beginReviewRef\.current\(proposal\)/);
  assert.equal((shell.match(/setProposalReviewSession\(beginProposalReview/g) ?? []).length, 1);
  assert.doesNotMatch(shell, /AIProposalReview|foregroundPreview|assistantPreviewProposal/);
  const start = shell.indexOf('const sendAssistant = async');
  const block = shell.slice(start, shell.indexOf('const selectWorkItemForAssistant'));
  assert.doesNotMatch(block, /setProposals\(|proposeWorkItemUpdate|applyProposal|rejectProposal/);
  const handoff = source('proposals/proposalHandoff.ts');
  assert.doesNotMatch(handoff, /proposeWorkItemUpdate|applyProposal|rejectProposal|AskAssistant/);
});

test('DEV test producer keeps its own path: create then refresh, no auto review', () => {
  const shell = source('components/AppShell.tsx');
  const start = shell.indexOf('const createProposal = async');
  const block = shell.slice(start, shell.indexOf('const reconcileTerminalProposalAction'));
  assert.match(block, /refreshPendingProposals/);
  assert.doesNotMatch(block, /beginReview|setProposalReviewSession/);
});
