import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  mapHistoryEvent,
  mapTrace,
  mapTraceSummary,
  formatSnapshot,
} from '../src/api/history/historyDtoAdapter.ts';
import type { app } from '../wailsjs/go/models.ts';

function event(overrides: Partial<app.HistoryEventDTO> = {}): app.HistoryEventDTO {
  return {
    id: 'event-1',
    eventType: 'WORK_ITEM_DETAILS_CHANGED',
    scope: 'WORK_ITEM',
    actor: 'USER',
    summary: 'Work Item title or description changed',
    occurredAt: '2026-09-20T12:00:00Z',
    recordedAt: '2026-09-20T12:00:01Z',
    correlationId: 'proposal:p-1',
    causationEventId: 'event-0',
    subjectEntityId: 'work-1',
    workItemId: 'work-1',
    revisionId: 'revision-1',
    proposalId: 'proposal-1',
    aiRunId: null,
    sourceRevisionId: null,
    before: { version: 1, title: 'Before', description: null },
    after: { version: 1, title: 'After', description: 'Bounded' },
    ...overrides,
  } as app.HistoryEventDTO;
}

test('real adapter maps canonical scope, actor, summary, bounded diff and nullable provenance', () => {
  const mapped = mapHistoryEvent(event());
  assert.equal(mapped.canonicalScope, 'WORK_ITEM');
  assert.deepEqual(mapped.actor, { label: 'User', kind: 'user' });
  assert.equal(mapped.summary, 'Work Item title or description changed');
  assert.match(mapped.before ?? '', /title: Before/);
  assert.match(mapped.after ?? '', /description: Bounded/);
  assert.equal(mapped.revisionId, 'revision-1');
  assert.equal(mapped.proposalId, 'proposal-1');
  assert.equal(mapped.aiRunId, undefined);
  assert.equal(mapped.workItemId, 'work-1');
});

test('snapshot formatting is deterministic, ignores version, and tolerates unknown bounded fields', () => {
  assert.equal(
    formatSnapshot({ version: 1, z_value: { safe: true }, a_value: null }),
    'a value: None · z value: {"safe":true}',
  );
  assert.equal(formatSnapshot(null), undefined);
});

test('trace adapter preserves backend correlation ordering and only explicit causation', () => {
  const mapped = mapTrace({
    correlationId: 'processing:one',
    truncated: false,
    events: [
      event({
        id: 'clean',
        scope: 'AI',
        actor: 'AI',
        eventType: 'AI_RUN_SUCCEEDED',
        correlationId: 'processing:one',
        causationEventId: null,
        aiRunId: 'run-clean',
        sourceRevisionId: 'source-1',
      }),
      event({
        id: 'interpret',
        scope: 'AI',
        actor: 'AI',
        eventType: 'AI_RUN_SUCCEEDED',
        correlationId: 'processing:one',
        causationEventId: 'clean',
        aiRunId: 'run-interpret',
        sourceRevisionId: 'source-1',
      }),
    ],
  } as app.HistoryTraceDTO);
  assert.deepEqual(
    mapped.events.map((item) => item.id),
    ['clean', 'interpret'],
  );
  assert.equal(mapped.events[0].causationId, undefined);
  assert.equal(mapped.events[1].causationId, 'clean');
  assert.equal(mapped.events[0].source, 'Source Revision · source-1');
});

test('trace summaries retain only real backend correlation IDs and owning WorkItem identity', () => {
  const mapped = mapTraceSummary({
    correlationId: 'monitor:m-1',
    eventCount: 3,
    firstOccurredAt: '2026-09-20T10:00:00Z',
    lastOccurredAt: '2026-09-20T11:00:00Z',
    representativeSummary: 'Monitor created',
    representativeScope: 'TRACKING',
    workItemId: 'work-1',
    scopes: ['TRACKING', 'ATTENTION'],
  } as app.HistoryTraceSummaryDTO);
  assert.equal(mapped.id, 'monitor:m-1');
  assert.equal(mapped.eventCount, 3);
  assert.equal(mapped.workItemId, 'work-1');
});

test('production History API uses Wails and has no prototype fallback', () => {
  const source = readFileSync(new URL('../src/api/history/index.ts', import.meta.url), 'utf8');
  assert.match(source, /wailsHistoryApi/);
  assert.doesNotMatch(source, /MockHistoryApi|mockHistoryApi/);
});

test('History components remain transport-free and preserve loading, empty, error, View Trace and Open Work Item states', () => {
  const source =
    readFileSync(new URL('../src/pages/History.tsx', import.meta.url), 'utf8') +
    readFileSync(new URL('../src/components/history/HistoryTimeline.tsx', import.meta.url), 'utf8');
  assert.doesNotMatch(source, /wailsjs\/go\/models/);
  for (const text of [
    'Loading History…',
    'No meaningful events match these filters.',
    'History could not be loaded.',
    'View Trace',
    'Open Work Item',
    'Timeline',
    'Traces',
    'Audit',
  ])
    assert.ok(source.includes(text), text);
});
