import type { HistoryCausalSourceDto } from '../../api/history/contracts';

const TEXT_KEYS = ['message', 'text', 'content', 'body', 'note', 'title'] as const;
const MAX_LENGTH = 600;

/** The human-readable input a Source captured, e.g. an Assistant prompt or injected note. */
export function rootSourceText(source: Pick<HistoryCausalSourceDto, 'payloadJson'>): string {
  const raw = source.payloadJson ?? '';
  let text = raw;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed === 'string') {
      text = parsed;
    } else if (parsed && typeof parsed === 'object') {
      const record = parsed as Record<string, unknown>;
      const key = TEXT_KEYS.find(
        (candidate) => typeof record[candidate] === 'string' && record[candidate] !== '',
      );
      text = key ? (record[key] as string) : JSON.stringify(parsed);
    }
  } catch {
    // Not JSON: show the payload as captured.
  }
  return text.length > MAX_LENGTH ? `${text.slice(0, MAX_LENGTH)}…` : text;
}

/** Identity of the capturing surface, e.g. "core.assistant / direct-turn · USER_INTENT". */
export function rootSourceLabel(
  source: Pick<HistoryCausalSourceDto, 'extensionId' | 'instanceId' | 'sourceKind'>,
): string {
  return `${source.extensionId} / ${source.instanceId} · ${source.sourceKind}`;
}
