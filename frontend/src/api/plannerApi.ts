import {
  CompleteLocalTodo,
  CreateLocalTodo,
  GetPlannerItemHistory,
  GetPlannerView,
  RemoveTodoReminder,
  SetTodoReminder,
} from '../../wailsjs/go/app/App.js';
import type { app } from '../../wailsjs/go/models';
import type {
  CalendarEvent,
  PlannerAuthority,
  PlannerContextStatus,
  PlannerItem,
  PlannerItemStatus,
  PlannerItemType,
  PlannerRecentChange,
  PlannerViewData,
  TodoReminder,
} from '../pages/plannerData';

/** Transport values required by the Wails Planner page-level read API. */
export interface PlannerViewRequest {
  rangeStart: string;
  rangeEnd: string;
  today: string;
  timezone: string;
}

const plannerItemTypes: readonly PlannerItemType[] = [
  'TODO',
  'CHECKPOINT',
  'FOLLOW_UP',
  'REMINDER',
];
const plannerItemStatuses: readonly PlannerItemStatus[] = ['OPEN', 'COMPLETED'];
const plannerAuthorities: readonly PlannerAuthority[] = ['LOCAL', 'EXTERNAL'];
const plannerContextStatuses: readonly PlannerContextStatus[] = [
  'COMPLETE',
  'INCOMPLETE',
  'UNKNOWN',
];
const localDatePattern = /^\d{4}-\d{2}-\d{2}$/;

function expectEnum<T extends string>(value: string, supported: readonly T[], field: string): T {
  if (supported.includes(value as T)) return value as T;
  throw new Error(`Unsupported ${field}: ${value}`);
}

function normalizeOptional(value: string | null | undefined) {
  return value ?? null;
}

function expectLocalDate(value: string | null, field: string) {
  if (!value || !localDatePattern.test(value)) {
    throw new Error(`Invalid ${field}: ${value ?? 'null'}`);
  }
  const [year, month, day] = value.split('-').map(Number);
  const parsed = new Date(Date.UTC(year, month - 1, day));
  if (
    parsed.getUTCFullYear() !== year ||
    parsed.getUTCMonth() !== month - 1 ||
    parsed.getUTCDate() !== day
  ) {
    throw new Error(`Invalid ${field}: ${value}`);
  }
  return value;
}

function expectInstant(value: string | null, field: string) {
  if (!value || Number.isNaN(Date.parse(value))) {
    throw new Error(`Invalid ${field}: ${value ?? 'null'}`);
  }
  return value;
}

function normalizeLocalDate(value: string | null | undefined, field: string) {
  const normalized = normalizeOptional(value);
  return normalized === null ? null : expectLocalDate(normalized, field);
}

function normalizeInstant(value: string | null | undefined, field: string) {
  const normalized = normalizeOptional(value);
  return normalized === null ? null : expectInstant(normalized, field);
}

/**
 * Converts one generated Planner Item DTO into the stable frontend model.
 * Generated optional fields are normalized here so components never need to
 * distinguish `undefined` from JSON `null`.
 */
function mapPlannerItem(dto: app.PlannerItemDTO): PlannerItem {
  return {
    kind: 'plannerItem',
    id: dto.id,
    type: expectEnum(dto.type, plannerItemTypes, 'Planner Item type'),
    title: dto.title,
    details: normalizeOptional(dto.details),
    status: expectEnum(dto.status, plannerItemStatuses, 'Planner Item status'),
    authority: expectEnum(dto.authority, plannerAuthorities, 'Planner Item authority'),
    scheduledAt: normalizeInstant(dto.scheduledAt, 'Planner Item scheduledAt'),
    dueAt: normalizeInstant(dto.dueAt, 'Planner Item dueAt'),
    scheduledDate: normalizeLocalDate(dto.scheduledDate, 'Planner Item scheduledDate'),
    dueDate: normalizeLocalDate(dto.dueDate, 'Planner Item dueDate'),
    completedAt: normalizeInstant(dto.completedAt, 'Planner Item completedAt'),
    contextStatus: expectEnum(
      dto.contextStatus,
      plannerContextStatuses,
      'Planner Item context status',
    ),
    missingContext: dto.missingContext ?? [],
    workItemId: dto.workItemId ?? null,
    workflowId: dto.workflowId ?? null,
    workflowStepId: dto.workflowStepId ?? null,
    workItemTitle: dto.workItemTitle ?? null,
    workflowName: dto.workflowName ?? null,
    workflowStepTitle: dto.workflowStepTitle ?? null,
    trackedItemId: dto.trackedItemId ?? null,
    trackedItemTitle: dto.trackedItemTitle ?? null,
    reminder: dto.reminder
      ? {
          id: dto.reminder.id,
          plannerItemId: dto.reminder.plannerItemId,
          state: dto.reminder.state as 'ACTIVE' | 'SATISFIED' | 'CANCELLED',
          offsetsSeconds: dto.reminder.offsetsSeconds ?? [],
          firedOffsets: dto.reminder.firedOffsets ?? [],
          remindMode: dto.reminder.remindMode,
        }
      : null,
    localNote: dto.localNote ?? null,
    externalOrigin: dto.externalOrigin
      ? {
          systemLabel: dto.externalOrigin.systemLabel,
          providerName: dto.externalOrigin.providerName ?? null,
          extensionId: dto.externalOrigin.extensionId ?? null,
        }
      : null,
    recentChanges: (dto.recentChanges ?? []).map((ch) => ({
      id: ch.eventId,
      label: `${new Date(ch.occurredAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} · ${ch.actor}`,
      description: ch.summary,
    })),
  };
}

/**
 * Converts one generated Calendar Event DTO while preserving mutually
 * exclusive exact-time and all-day date boundaries. All-day `endDate` remains
 * exclusive and is never converted into an artificial timestamp.
 */
function mapCalendarEvent(dto: app.CalendarEventDTO): CalendarEvent {
  const authority = expectEnum(dto.authority, plannerAuthorities, 'Calendar Event authority');
  const startAt = normalizeOptional(dto.startAt);
  const endAt = normalizeOptional(dto.endAt);
  const startDate = normalizeOptional(dto.startDate);
  const endDate = normalizeOptional(dto.endDate);

  if (dto.isAllDay) {
    const parsedStart = expectLocalDate(startDate, 'Calendar Event startDate');
    const parsedEnd = expectLocalDate(endDate, 'Calendar Event endDate');
    if (parsedEnd <= parsedStart) {
      throw new Error(`Calendar Event ${dto.id} endDate must be after startDate`);
    }
    if (startAt || endAt) {
      throw new Error(`All-day Calendar Event ${dto.id} must not contain exact timestamps`);
    }
  } else {
    const parsedStart = expectInstant(startAt, 'Calendar Event startAt');
    const parsedEnd = expectInstant(endAt, 'Calendar Event endAt');
    if (Date.parse(parsedEnd) <= Date.parse(parsedStart)) {
      throw new Error(`Calendar Event ${dto.id} endAt must be after startAt`);
    }
    if (startDate || endDate) {
      throw new Error(`Timed Calendar Event ${dto.id} must not contain date-only boundaries`);
    }
  }

  return {
    kind: 'event',
    id: dto.id,
    authority,
    title: dto.title,
    details: normalizeOptional(dto.details),
    location: normalizeOptional(dto.location),
    startAt,
    endAt,
    startDate,
    endDate,
    isAllDay: dto.isAllDay,
    timezone: normalizeOptional(dto.timezone),
    recentChanges: [],
  };
}

/**
 * Loads one Planner page projection through the generated Wails binding.
 *
 * Generated Wails models are contained in this module; the rest of the
 * frontend consumes only normalized CalendarEvent, PlannerItem, and
 * PlannerViewData values.
 */
export async function getPlannerView(request: PlannerViewRequest): Promise<PlannerViewData> {
  const response = await GetPlannerView(request);
  return mapPlannerViewDTO(response);
}

/** Maps the shared Planner projection contained by Planner and Dashboard DTOs. */
export function mapPlannerViewDTO(response: app.PlannerViewDTO): PlannerViewData {
  return {
    calendarEvents: (response.calendarEvents ?? []).map(mapCalendarEvent),
    calendarItems: (response.calendarItems ?? []).map(mapPlannerItem),
    currentOpenItems: (response.currentOpenItems ?? []).map(mapPlannerItem),
    completedItems: (response.completedItems ?? []).map(mapPlannerItem),
    completedTodayItems: (response.completedTodayItems ?? []).map(mapPlannerItem),
  };
}

/**
 * Completes one locally authoritative Todo through the Wails application
 * boundary and returns the committed backend state. Projection membership is
 * deliberately left to the subsequent GetPlannerView refresh.
 */
export async function completeLocalTodo(id: string): Promise<PlannerItem> {
  const itemID = id.trim();
  if (!itemID) {
    throw new Error('Planner item ID must not be empty.');
  }

  const response = await CompleteLocalTodo(itemID);
  return mapPlannerItem(response);
}

export async function createLocalTodo(request: app.CreateLocalTodoRequestDTO): Promise<PlannerItem> {
  const response = await CreateLocalTodo(request);
  return mapPlannerItem(response);
}

export async function setTodoReminder(request: app.SetTodoReminderRequestDTO): Promise<TodoReminder> {
  const response = await SetTodoReminder(request);
  return {
    id: response.id,
    plannerItemId: response.plannerItemId,
    state: response.state as 'ACTIVE' | 'SATISFIED' | 'CANCELLED',
    offsetsSeconds: response.offsetsSeconds ?? [],
    firedOffsets: response.firedOffsets ?? [],
    remindMode: response.remindMode,
  };
}

export async function removeTodoReminder(plannerItemId: string): Promise<void> {
  await RemoveTodoReminder(plannerItemId);
}

export async function getPlannerItemHistory(plannerItemId: string): Promise<PlannerRecentChange[]> {
  const response = await GetPlannerItemHistory(plannerItemId);
  return (response ?? []).map((ev) => ({
    eventId: ev.eventId,
    eventType: ev.eventType,
    summary: ev.summary,
    occurredAt: ev.occurredAt,
    actor: ev.actor,
  }));
}
