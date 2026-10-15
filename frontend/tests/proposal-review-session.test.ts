import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type { ProductProposal } from '../src/api/proposals/proposalProductApi.ts';
import {
  beginProposalReview,
  proposalForReviewSession,
  proposalTargetNavigation,
} from '../src/proposals/proposalReviewSession.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

function proposal(id: string, targetWorkItemId: string): ProductProposal {
  return {
    id,
    targetWorkItemId,
    operationType: 'work_item.update_details',
    title: `Proposal ${id}`,
    summary: 'Summary',
    reason: 'Reason',
    status: 'PENDING',
    baseTitle: 'Before',
    baseDescription: 'Before description',
    proposedTitle: 'After',
    proposedDescription: 'After description',
    createdAt: '2026-09-19T10:00:00Z',
    updatedAt: '2026-09-19T10:00:00Z',
    resolvedAt: null,
  };
}

test('review session stores proposal identity only and derives authoritative truth', () => {
  const first = proposal('a', 'wi-a');
  const session = beginProposalReview(first);
  assert.deepEqual(session, { proposalId: 'a' });
  assert.deepEqual(Object.keys(session), ['proposalId']);
  assert.equal(proposalForReviewSession([first], session), first);
});

test('switching proposal replaces identity without changing either proposal', () => {
  const first = proposal('a', 'wi-a');
  const second = proposal('b', 'wi-b');
  const next = beginProposalReview(second);
  assert.deepEqual(next, { proposalId: 'b' });
  assert.equal(first.status, 'PENDING');
  assert.equal(second.status, 'PENDING');
});

test('ordinary page navigation has no review-session transition', () => {
  const session = beginProposalReview(proposal('a', 'wi-a'));
  for (const page of ['dashboard', 'planner', 'settings', 'workItems']) {
    assert.equal(session.proposalId, 'a', page);
  }
});

test('target navigation opens, selects, or no-ops using real target identity', () => {
  const item = proposal('a', 'wi-a');
  assert.equal(proposalTargetNavigation('dashboard', null, item), 'OPEN_TARGET');
  assert.equal(proposalTargetNavigation('workItems', 'wi-b', item), 'SELECT_TARGET');
  assert.equal(proposalTargetNavigation('workItems', 'wi-a', item), 'NONE');
});

test('Assistant production path is authoritative, pending-only, and has no mock fallback', () => {
  const shell = source('components/AppShell.tsx');
  const panel = source('components/AssistantPanel.tsx');
  const index = source('api/proposals/index.ts');
  assert.match(shell, /proposalApi = wailsProposalProductApi/);
  assert.match(shell, /proposalApi\.getPendingProposals\(\)/);
  assert.match(panel, /proposal\.status === 'PENDING'/);
  assert.doesNotMatch(`${shell}\n${panel}\n${index}`, /MockProposalApi|listProposals\(\)/);
  assert.doesNotMatch(panel, /onRegenerate|Preview Changes|Apply All|Reject All/);
});

test('review exit surfaces only clear transient session and banner has no Apply action', () => {
  const shell = source('components/AppShell.tsx');
  const banner = source('components/proposals/PreviewBanner.tsx');
  assert.match(
    shell,
    /const endProposalReview = \(\) => \{[\s\S]*?setProposalReviewSession\(null\);[\s\S]*?setProposalActionError\(null\);[\s\S]*?\};/,
  );
  assert.match(
    shell,
    /const closeAssistant = useCallback\(\(\) => \{[\s\S]*setProposalReviewSession\(null\)/,
  );
  assert.match(panelSource(), /onBack=\{props\.onEndReview\}/);
  assert.doesNotMatch(banner, />Apply</);
  assert.match(banner, /Exit Preview/);
});

test('Assistant owns Apply/Reject while Banner remains exit-only', () => {
  const shell = source('components/AppShell.tsx');
  const panel = panelSource();
  const banner = source('components/proposals/PreviewBanner.tsx');
  assert.match(panel, /onApplyProposal/);
  assert.match(panel, /onRejectProposal/);
  assert.match(shell, /proposalApi\.applyProposal\(activeProposal\.id\)/);
  assert.match(shell, /proposalApi\.rejectProposal\(activeProposal\.id\)/);
  assert.doesNotMatch(banner, /Apply|Reject/);
});

test('terminal Apply outcomes reconcile while failures retain the active review', () => {
  const shell = source('components/AppShell.tsx');
  assert.match(shell, /result\.outcome === 'APPLIED'/);
  assert.match(shell, /result\.outcome === 'STALE'/);
  assert.match(shell, /already applied/i);
  assert.match(shell, /setWorkItemRefreshGeneration/);
  assert.match(shell, /await refreshPendingProposals\(\)/);
  assert.match(shell, /catch \(error\) \{\s*setProposalActionError/);
  assert.match(shell, /proposalMutationRef\.current/);
});

test('mutation failure message is inline and review remains retryable', () => {
  const panel = panelSource();
  assert.match(panel, /Proposal action failed:/);
  assert.match(panel, /Review remains active so you can retry/);
  assert.match(panel, /disabled=\{mutation !== null\}/);
});

function panelSource() {
  return source('components/AssistantPanel.tsx');
}

test('Dashboard CTA opens Assistant Needs Review instead of navigating to Work Items', () => {
  const dashboard = source('pages/Dashboard.tsx');
  const shell = source('components/AppShell.tsx');
  assert.match(dashboard, /Review suggestions/);
  assert.match(dashboard, /onClick=\{onReviewSuggestions\}/);
  assert.match(shell, /setAssistantOpen\(true\)/);
});

test('Assistant detail renders typed proposal changes and deterministic Apply impact', () => {
  const panel = panelSource();
  for (const field of [
    'proposal.title',
    'proposal.summary',
    'proposal.reason',
    'proposal.baseTitle',
    'proposal.baseDescription',
    'proposal.proposedTitle',
    'proposal.proposedDescription',
  ]) {
    assert.match(panel, new RegExp(field.replace('.', '\\.')));
  }
  assert.match(panel, /proposal\.changes/);
  assert.match(panel, /authoritative domain service in one transaction/);
  assert.doesNotMatch(panel, /Background AI|sources ·|Dashboard \+5 days|fake producer/i);
});
