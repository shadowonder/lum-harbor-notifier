import type { ProgressStatusDto } from '../../api/workItems/contracts';
import type { RequirementStatus, TrackableItemStatus, TrackingStatus } from '../../types/workItem';

export interface StatusPresentation {
  label: string;
  symbol: string;
  tone: 'positive' | 'info' | 'neutral' | 'warning' | 'danger';
  line: 'solid' | 'dashed';
  priority: number;
  fill: 'solid' | 'hollow';
  showSymbol: boolean;
}

export const statusPresentation: Record<ProgressStatusDto, StatusPresentation> = {
  completed: { label: 'Completed', symbol: '●', tone: 'positive', line: 'solid', priority: 1, fill: 'solid', showSymbol: false },
  in_progress: { label: 'In progress', symbol: '◉', tone: 'info', line: 'solid', priority: 4, fill: 'solid', showSymbol: false },
  queued: { label: 'Queued', symbol: '○', tone: 'neutral', line: 'dashed', priority: 3, fill: 'hollow', showSymbol: false },
  waiting: { label: 'Waiting', symbol: '⌛', tone: 'warning', line: 'dashed', priority: 7, fill: 'solid', showSymbol: true },
  blocked: { label: 'Blocked', symbol: '×', tone: 'danger', line: 'dashed', priority: 9, fill: 'solid', showSymbol: true },
  failed: { label: 'Failed', symbol: '×', tone: 'danger', line: 'solid', priority: 10, fill: 'solid', showSymbol: true },
  unknown: { label: 'Unknown', symbol: '?', tone: 'neutral', line: 'dashed', priority: 6, fill: 'hollow', showSymbol: true },
  deferred: { label: 'Deferred', symbol: 'Ⅱ', tone: 'neutral', line: 'dashed', priority: 5, fill: 'hollow', showSymbol: true },
  target: { label: 'Final target', symbol: '◆', tone: 'neutral', line: 'solid', priority: 0, fill: 'hollow', showSymbol: false },
};

export function mostSignificantStatus(statuses: ProgressStatusDto[]): ProgressStatusDto {
  if (!statuses.length) return 'unknown';
  return [...statuses].sort(
    (left, right) => statusPresentation[right].priority - statusPresentation[left].priority,
  )[0];
}

export function isAttentionStatus(status: ProgressStatusDto) {
  return ['waiting', 'blocked', 'failed', 'unknown'].includes(status);
}

export function normalizeProgressStatus(
  status: TrackableItemStatus | RequirementStatus | TrackingStatus,
): ProgressStatusDto {
  if (status === 'complete' || status === 'verified' || status === 'ready') return 'completed';
  if (status === 'inProgress') return 'in_progress';
  if (status === 'missing') return 'waiting';
  if (status === 'behind') return 'failed';
  return status;
}

export function statusColorToken(status: ProgressStatusDto) {
  const tone = statusPresentation[status].tone;
  if (tone === 'positive') return 'success.main';
  if (tone === 'info') return 'primary.main';
  if (tone === 'danger') return 'error.main';
  if (tone === 'warning') return 'warning.main';
  return 'text.secondary';
}
