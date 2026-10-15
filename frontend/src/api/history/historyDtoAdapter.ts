import type { app } from '../../../wailsjs/go/models';
import type {
  CanonicalHistoryScope,
  HistoryCausalChainDto,
  HistoryCausalSourceDto,
  HistoryEventDto,
  HistoryScope,
  HistoryTraceDto,
  HistoryTraceStage,
  HistoryTraceSummaryDto,
} from './contracts';

const canonicalScopes: readonly CanonicalHistoryScope[] = [
  'TRACKING',
  'ATTENTION',
  'NOTIFICATION',
  'PROPOSAL',
  'WORK_ITEM',
  'PLANNER',
  'SOURCE',
  'AI',
];
const presentationScope: Record<CanonicalHistoryScope, HistoryScope> = {
  TRACKING: 'workItems',
  ATTENTION: 'attention',
  NOTIFICATION: 'attention',
  PROPOSAL: 'automation',
  WORK_ITEM: 'workItems',
  PLANNER: 'planner',
  SOURCE: 'evidence',
  AI: 'automation',
};

function canonicalScope(value: string): CanonicalHistoryScope {
  if (!canonicalScopes.includes(value as CanonicalHistoryScope))
    throw new Error(`Unsupported History scope: ${value}`);
  return value as CanonicalHistoryScope;
}

function short(value: string) {
  return value.length > 12 ? `${value.slice(0, 8)}…` : value;
}
function title(eventType: string) {
  return eventType
    .toLocaleLowerCase()
    .split('_')
    .map((word) => word[0]?.toLocaleUpperCase() + word.slice(1))
    .join(' ');
}
function valueText(value: unknown): string {
  if (value === null) return 'None';
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean')
    return String(value);
  return JSON.stringify(value);
}
export function formatSnapshot(
  value: Record<string, unknown> | null | undefined,
): string | undefined {
  if (!value) return undefined;
  return (
    Object.keys(value)
      .filter((key) => key !== 'version')
      .sort()
      .map((key) => `${key.replaceAll('_', ' ')}: ${valueText(value[key])}`)
      .join(' · ') || undefined
  );
}
function stage(dto: app.HistoryEventDTO): HistoryTraceStage {
  if (
    dto.eventType.includes('RESOLVED') ||
    dto.eventType.includes('COMPLETED') ||
    dto.eventType.includes('CANCELLED') ||
    dto.eventType.includes('APPLIED') ||
    dto.eventType.includes('REJECTED') ||
    dto.eventType.includes('STALE') ||
    dto.eventType.startsWith('AI_RUN_')
  )
    return 'resolution';
  if (dto.scope === 'SOURCE') return 'source';
  if (dto.scope === 'AI' || dto.actor === 'RULE') return 'evaluation';
  if (dto.actor === 'USER') return 'action';
  if (dto.scope === 'ATTENTION') return 'attention';
  if (dto.scope === 'NOTIFICATION') return 'notification';
  if (dto.sourceRevisionId) return 'evidence';
  return 'state';
}
function actor(dto: app.HistoryEventDTO): HistoryEventDto['actor'] {
  const kind =
    dto.actor === 'USER'
      ? 'user'
      : dto.actor === 'SOURCE'
        ? 'integration'
        : dto.actor === 'SYSTEM'
          ? 'system'
          : 'automation';
  const label =
    dto.actor === 'RULE'
      ? 'Rule Engine'
      : dto.actor === 'AI'
        ? 'Lum Harbor AI'
        : dto.actor[0] + dto.actor.slice(1).toLocaleLowerCase();
  return { label, kind };
}

export function mapHistoryEvent(dto: app.HistoryEventDTO): HistoryEventDto {
  const scope = canonicalScope(dto.scope);
  const subjectId =
    dto.subjectEntityId ??
    dto.workItemId ??
    dto.proposalId ??
    dto.aiRunId ??
    dto.sourceRevisionId ??
    dto.revisionId ??
    dto.id;
  const details = [
    ['Revision', dto.revisionId],
    ['Proposal', dto.proposalId],
    ['AI Run', dto.aiRunId],
    ['Source Revision', dto.sourceRevisionId],
    ['Causation', dto.causationEventId],
    ['Correlation', dto.correlationId],
    ['Recorded at', dto.recordedAt],
  ]
    .filter((entry): entry is [string, string] => Boolean(entry[1]))
    .map(([label, value]) => ({ label, value }));
  return {
    id: dto.id,
    occurredAt: dto.occurredAt,
    eventType: dto.eventType,
    scope: presentationScope[scope],
    canonicalScope: scope,
    actor: actor(dto),
    subject: { id: subjectId, type: scope, label: dto.summary },
    workItemId: dto.workItemId ?? undefined,
    workItemTitle: dto.workItemId ? `Work Item · ${short(dto.workItemId)}` : undefined,
    title: title(dto.eventType),
    summary: dto.summary,
    before: formatSnapshot(dto.before),
    after: formatSnapshot(dto.after),
    source: dto.sourceRevisionId
      ? `Source Revision · ${short(dto.sourceRevisionId)}`
      : dto.aiRunId
        ? `AI Run · ${short(dto.aiRunId)}`
        : undefined,
    correlationId: dto.correlationId,
    causationId: dto.causationEventId ?? undefined,
    revisionId: dto.revisionId ?? undefined,
    proposalId: dto.proposalId ?? undefined,
    aiRunId: dto.aiRunId ?? undefined,
    sourceRevisionId: dto.sourceRevisionId ?? undefined,
    traceStage: stage(dto),
    details,
    meaningful: true,
  };
}

export function mapTraceSummary(dto: app.HistoryTraceSummaryDTO): HistoryTraceSummaryDto {
  return {
    id: dto.correlationId,
    eventCount: dto.eventCount,
    firstOccurredAt: dto.firstOccurredAt,
    lastOccurredAt: dto.lastOccurredAt,
    title: dto.representativeSummary,
    representativeScope: canonicalScope(dto.representativeScope),
    workItemId: dto.workItemId ?? undefined,
    scopes: (dto.scopes ?? []).map(canonicalScope),
  };
}
export function mapTrace(dto: app.HistoryTraceDTO): HistoryTraceDto {
  return {
    id: dto.correlationId,
    events: (dto.events ?? []).map(mapHistoryEvent),
    truncated: dto.truncated,
  };
}

export function mapCausalChain(dto: app.HistoryCausalChainDTO): HistoryCausalChainDto {
  return {
    targetEventId: dto.targetEventId,
    targetEvent: dto.targetEvent ? mapHistoryEvent(dto.targetEvent) : undefined,
    rootEvent: dto.rootEvent ? mapHistoryEvent(dto.rootEvent) : undefined,
    events: (dto.events ?? []).map(mapHistoryEvent),
    rootSource: (dto as unknown as { rootSource?: HistoryCausalSourceDto }).rootSource ?? undefined,
  };
}
