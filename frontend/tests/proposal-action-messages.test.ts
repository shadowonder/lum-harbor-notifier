import assert from 'node:assert/strict';
import test from 'node:test';
import {
  proposalAppliedMessage,
  proposalRejectedMessage,
  proposalStaleMessage,
} from '../src/proposals/proposalActionMessages.ts';

test('Planner proposals are not described as WorkItem changes', () => {
  for (const message of [
    proposalAppliedMessage('planner.item.create'),
    proposalStaleMessage('planner.item.create'),
    proposalRejectedMessage('planner.item.create'),
  ]) {
    assert.doesNotMatch(message, /WorkItem|Tracking/);
  }
  assert.equal(proposalAppliedMessage('planner.item.create'), 'Proposal applied to the Planner.');
});

test('WorkItem and Tracking proposal notices keep their domain wording', () => {
  assert.equal(proposalAppliedMessage('workitem.update_details'), 'Proposal applied to the authoritative WorkItem.');
  assert.equal(proposalAppliedMessage('tracked_item.create'), 'Proposal applied to authoritative Tracking.');
  assert.match(proposalStaleMessage('tracked_item.create'), /Tracking changed/);
  assert.equal(proposalRejectedMessage('workitem.set_lifecycle'), 'Proposal rejected. The authoritative WorkItem was not changed.');
  assert.equal(proposalRejectedMessage('tracked_item.create'), 'Proposal rejected. Tracking was not changed.');
});
