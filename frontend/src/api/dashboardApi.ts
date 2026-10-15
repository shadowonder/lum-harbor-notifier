import type { app } from '../../wailsjs/go/models';
import type { ProductWorkItemLifecycle } from './workItems/productApi';
import { mapMonitorPoint } from './tracking/trackingDtoAdapter.ts';
import type { MonitorPoint } from './tracking/trackingProductApi.ts';
import { mapPlannerViewDTO, type PlannerViewRequest } from './plannerApi.ts';
import type { PlannerViewData } from '../pages/plannerData';

export type DashboardViewRequest = PlannerViewRequest;

export interface DashboardCurrentWorkItem {
  id: string;
  title: string;
  description: string | null;
  lifecycleState: 'ACTIVE' | 'PAUSED';
  updatedAt: string;
  /** The WorkItem-subject ACTIVE FINAL_TARGET, or null when there is none. Meaningless when tracking is unavailable. */
  activeFinalTarget: MonitorPoint | null;
  /** Authoritative actual Workflow start (ISO instant), or null when the WorkItem has no Workflow. */
  workflowStartedAt: string | null;
}

export interface DashboardWorkItemSummary {
  currentCount: number;
  pausedCount: number;
  completedCount: number;
  cancelledCount: number;
  closedCount: number;
  currentItems: DashboardCurrentWorkItem[];
  allCurrentItems: DashboardCurrentWorkItem[];
  /** False when Tracking could not be read; targets must then be shown as unavailable, not unscheduled. */
  trackingAvailable: boolean;
}

export interface DashboardViewData {
  planner: PlannerViewData;
  workItems: DashboardWorkItemSummary;
}

export interface DashboardViewApi {
  get(request: DashboardViewRequest): Promise<DashboardViewData>;
}

export function loadDashboardView(api: DashboardViewApi, request: DashboardViewRequest) {
  return api.get(request);
}

function expectCount(value: number, field: string) {
  if (!Number.isInteger(value) || value < 0) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

function expectInstant(value: string, field: string) {
  if (!value || Number.isNaN(Date.parse(value))) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

function mapCurrentWorkItem(dto: app.DashboardCurrentWorkItemDTO): DashboardCurrentWorkItem {
  const lifecycle = dto.lifecycleState as ProductWorkItemLifecycle;
  if (lifecycle !== 'ACTIVE' && lifecycle !== 'PAUSED') {
    throw new Error(`Unsupported Dashboard current WorkItem lifecycle: ${dto.lifecycleState}`);
  }
  return {
    id: dto.id,
    title: dto.title,
    description: dto.description ?? null,
    lifecycleState: lifecycle,
    updatedAt: expectInstant(dto.updatedAt, 'Dashboard WorkItem updatedAt'),
    activeFinalTarget: dto.tracking?.activeFinalTarget
      ? mapMonitorPoint(dto.tracking.activeFinalTarget)
      : null,
    workflowStartedAt: dto.workflowStartedAt
      ? expectInstant(dto.workflowStartedAt, 'Dashboard Workflow startedAt')
      : null,
  };
}

export function mapDashboardViewDTO(dto: app.DashboardViewDTO): DashboardViewData {
  const workItems = dto.workItems;
  return {
    planner: mapPlannerViewDTO(dto.planner),
    workItems: {
      currentCount: expectCount(workItems.currentCount, 'currentCount'),
      pausedCount: expectCount(workItems.pausedCount, 'pausedCount'),
      completedCount: expectCount(workItems.completedCount, 'completedCount'),
      cancelledCount: expectCount(workItems.cancelledCount, 'cancelledCount'),
      closedCount: expectCount(workItems.closedCount, 'closedCount'),
      currentItems: (workItems.currentItems ?? []).map(mapCurrentWorkItem),
      allCurrentItems: (workItems.allCurrentItems ?? []).map(mapCurrentWorkItem),
      trackingAvailable: workItems.trackingAvailable,
    },
  };
}
