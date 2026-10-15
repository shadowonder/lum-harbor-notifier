import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import type { app } from '../wailsjs/go/models.ts';
import { mapDashboardProposalSummaryDTO } from '../src/api/proposals/proposalDtoAdapter.ts';
import {
  getDashboardProposalSummary,
  type DashboardProposalSummary,
  type ProposalProductApi,
} from '../src/api/proposals/proposalProductApi.ts';

function sourceOf(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const mockSummaryDTO: app.DashboardProposalSummaryDTO = {
  pendingCount: 3,
  affectedWorkItemCount: 2,
};

test('mapDashboardProposalSummaryDTO maps valid numbers cleanly', () => {
  const summary = mapDashboardProposalSummaryDTO(mockSummaryDTO);
  assert.equal(summary.pendingCount, 3);
  assert.equal(summary.affectedWorkItemCount, 2);
});

test('mapDashboardProposalSummaryDTO handles zero counts cleanly for quiet zero state', () => {
  const summary = mapDashboardProposalSummaryDTO({
    pendingCount: 0,
    affectedWorkItemCount: 0,
  });
  assert.equal(summary.pendingCount, 0);
  assert.equal(summary.affectedWorkItemCount, 0);
});

test('mapDashboardProposalSummaryDTO rejects invalid or negative counts', () => {
  assert.throws(
    () => mapDashboardProposalSummaryDTO({ pendingCount: -1, affectedWorkItemCount: 1 }),
    /Invalid pendingCount: -1/,
  );
  assert.throws(
    () =>
      mapDashboardProposalSummaryDTO({
        pendingCount: 'not-a-num' as unknown as number,
        affectedWorkItemCount: 1,
      }),
    /Invalid pendingCount: not-a-num/,
  );
  assert.throws(
    () => mapDashboardProposalSummaryDTO({ pendingCount: 1, affectedWorkItemCount: -2 }),
    /Invalid affectedWorkItemCount: -2/,
  );
  assert.throws(
    () =>
      mapDashboardProposalSummaryDTO({
        pendingCount: 1,
        affectedWorkItemCount: null as unknown as number,
      }),
    /Invalid affectedWorkItemCount: null/,
  );
});

test('getDashboardProposalSummary helper delegates to api.getDashboardSummary', async () => {
  let called = false;
  const mockApi: ProposalProductApi = {
    getPendingProposals: async () => [],
    proposeWorkItemUpdate: async () => {
      throw new Error('not implemented');
    },
    applyProposal: async () => {
      throw new Error('not implemented');
    },
    rejectProposal: async () => {
      throw new Error('not implemented');
    },
    getDashboardSummary: async (): Promise<DashboardProposalSummary> => {
      called = true;
      return { pendingCount: 4, affectedWorkItemCount: 2 };
    },
  };

  const result = await getDashboardProposalSummary(mockApi);
  assert.equal(called, true);
  assert.equal(result.pendingCount, 4);
  assert.equal(result.affectedWorkItemCount, 2);
});

test('Dashboard Proposal Awareness architecture: imports only from proposalProductApi and wailsProposalProductApi, never from models', () => {
  const dashboardSource = sourceOf('../src/pages/Dashboard.tsx');

  // Verify transport boundary: React component must NOT import from wailsjs/go/models
  assert.ok(
    !dashboardSource.includes("from '../wailsjs/go/models") &&
      !dashboardSource.includes('from "../wailsjs/go/models') &&
      !dashboardSource.includes("from '../../wailsjs/go/models") &&
      !dashboardSource.includes('from "../../wailsjs/go/models'),
    'Dashboard.tsx must never import from wailsjs/go/models',
  );

  // Verify it imports proposal API types from proposalProductApi and wailsProposalProductApi
  assert.ok(
    dashboardSource.includes("from '../api/proposals/proposalProductApi.ts'"),
    'Dashboard.tsx must import types from proposalProductApi.ts',
  );
  assert.ok(
    dashboardSource.includes("from '../api/proposals/wailsProposalProductApi.ts'"),
    'Dashboard.tsx must import wailsProposalProductApi as default prop',
  );
});

test('Dashboard Proposal Awareness architecture: backend projection truth source without React filtering', () => {
  const dashboardSource = sourceOf('../src/pages/Dashboard.tsx');

  // Must call proposalApi.getDashboardSummary()
  assert.ok(
    dashboardSource.includes('proposalApi.getDashboardSummary()'),
    'Dashboard.tsx must query proposalApi.getDashboardSummary() for proposal counts',
  );

  // Must NOT filter proposals locally (e.g. .filter(p => p.status === 'PENDING'))
  assert.ok(
    !dashboardSource.includes('.filter(') || !dashboardSource.includes("=== 'PENDING'"),
    'Dashboard must not locally filter proposals; truth comes from backend projection',
  );
});

test('Dashboard Proposal Awareness UI: quiet zero state, Assistant CTA, and no mutate controls', () => {
  const dashboardSource = sourceOf('../src/pages/Dashboard.tsx');

  // Must render awareness banner conditionally on pendingCount > 0
  assert.ok(
    dashboardSource.includes('proposalSummary.pendingCount > 0'),
    'Dashboard must gate banner rendering on pendingCount > 0 for quiet zero state',
  );

  // Review ownership converges on the Assistant inbox.
  assert.ok(
    dashboardSource.includes('Review suggestions'),
    'Dashboard must render "Review suggestions" button',
  );

  // Must wire the Proposal CTA to Assistant, not Work Items.
  assert.ok(
    dashboardSource.includes('onClick={onReviewSuggestions}'),
    'Dashboard must wire Proposal review to the Assistant inbox',
  );

  // Must NOT include accept/reject/apply buttons in Dashboard
  assert.ok(
    !dashboardSource.includes('applyProposal') &&
      !dashboardSource.includes('rejectProposal') &&
      !dashboardSource.includes('Accept Change') &&
      !dashboardSource.includes('Reject Change'),
    'Dashboard must NOT contain mutate actions like Accept/Reject; awareness and navigation only',
  );
});

test('Dashboard Proposal Awareness UI: error isolation without masking error as count = 0', () => {
  const dashboardSource = sourceOf('../src/pages/Dashboard.tsx');

  // Must capture error independently as proposalError
  assert.ok(
    dashboardSource.includes('proposalError'),
    'Dashboard must track proposalError independently from dashboardError',
  );

  // Must NOT mask error as count = 0 (proposalSummary set to null on error)
  assert.ok(
    dashboardSource.includes('setProposalSummary(null)'),
    'Dashboard must set proposalSummary to null on error, not fake { pendingCount: 0 }',
  );

  // Must provide isolated alert with retry
  assert.ok(
    dashboardSource.includes('Suggested changes could not be loaded:'),
    'Dashboard must display isolated error alert for proposal summary failure',
  );
  assert.ok(
    dashboardSource.includes('retryProposalSummary'),
    'Dashboard must provide isolated Retry mechanism for proposal summary',
  );
});
