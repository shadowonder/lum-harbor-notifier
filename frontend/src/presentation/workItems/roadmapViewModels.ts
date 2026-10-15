import type { WorkItemMilestoneDto, WorkItemSummaryDto } from '../../api/workItems/contracts';
import type { Attention } from '../../types/workItem';

export type RoadmapRange = '2W' | '4W' | '3M';

export interface RoadmapMilestoneViewModel {
  id: string;
  title: string;
  occurredAt: string;
  status: 'completed' | 'upcoming';
  kind: WorkItemMilestoneDto['kind'];
}

export interface RoadmapItemViewModel {
  id: string;
  title: string;
  lifecycleState: 'active';
  attention: Attention;
  startAt: string;
  targetAt: string;
  forecastAt?: string;
  progress: {
    completed: number;
    total: number;
    ratio: number;
    label: string;
  };
  latestProgress?: { date: string; label: string };
  nextAction?: string;
  milestones: RoadmapMilestoneViewModel[];
}

const attentionPriority: Record<Attention, number> = { high: 4, attention: 3, normal: 1, low: 0 };

function dayValue(value: string) {
  const date = new Date(value);
  return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
}

function shortDate(value: string) {
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(value));
}

export function toRoadmapItemViewModel(dto: WorkItemSummaryDto, asOf: string): RoadmapItemViewModel | null {
  if (dto.lifecycleState !== 'active' || !dto.startedAt || !dto.targetAt) return null;
  const completed = dto.progress.completed;
  const total = dto.progress.total;
  const milestones = [...dto.milestones]
    .sort((left, right) => dayValue(left.occurredAt) - dayValue(right.occurredAt))
    .slice(-3)
    .map((milestone) => ({
      id: milestone.id,
      title: milestone.label,
      occurredAt: milestone.occurredAt,
      status: dayValue(milestone.occurredAt) <= dayValue(asOf) ? 'completed' as const : 'upcoming' as const,
      kind: milestone.kind,
    }));
  const latest = [...milestones].filter((milestone) => milestone.status === 'completed').at(-1);
  return {
    id: dto.id,
    title: dto.title,
    lifecycleState: 'active',
    attention: dto.attention,
    startAt: dto.startedAt,
    targetAt: dto.targetAt,
    forecastAt: dto.forecastAt,
    progress: {
      completed,
      total,
      ratio: total ? Math.min(1, Math.max(0, completed / total)) : 0,
      label: dto.progress.label,
    },
    latestProgress: latest ? { date: shortDate(latest.occurredAt), label: latest.title } : undefined,
    nextAction: dto.nextAction,
    milestones,
  };
}

export function sortRoadmapItems(items: RoadmapItemViewModel[]) {
  return [...items].sort((left, right) =>
    attentionPriority[right.attention] - attentionPriority[left.attention]
    || dayValue(left.targetAt) - dayValue(right.targetAt),
  );
}
