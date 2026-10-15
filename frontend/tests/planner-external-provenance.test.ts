import assert from 'node:assert/strict';
import test from 'node:test';
import { mapPlannerViewDTO } from '../src/api/plannerApi.ts';
import { formatPlannerItemSource, type PlannerItem } from '../src/pages/plannerData.ts';
import { loadComponent, renderMarkup, visibleText } from './support/renderComponent.ts';

const reminderDTO = {
  id: 'p-1',
  type: 'REMINDER',
  title: 'New reminder test 1',
  details: 'Apple notes v2',
  status: 'OPEN',
  authority: 'EXTERNAL',
  scheduledAt: null,
  dueAt: null,
  scheduledDate: null,
  dueDate: '2026-10-20',
  completedAt: null,
  contextStatus: 'UNKNOWN',
  missingContext: [],
  localNote: 'This is a test reminder synced from Apple Reminders.',
  externalOrigin: {
    systemLabel: 'Apple Reminders',
    providerName: 'Apple Reminders (EventKit)',
    extensionId: 'lh-plugin-apple-reminders',
    instanceId: 'apple-reminders-main',
  },
};

function mappedReminder(overrides: Record<string, unknown> = {}): PlannerItem {
  const view = mapPlannerViewDTO({
    calendarEvents: [],
    calendarItems: [],
    currentOpenItems: [{ ...reminderDTO, ...overrides }],
    completedItems: [],
    completedTodayItems: [],
  } as never);
  return view.currentOpenItems[0];
}

test('Planner DTO mapping keeps external notes, Lum Harbor note and backend provenance separate', () => {
  const item = mappedReminder();
  assert.equal(item.details, 'Apple notes v2');
  assert.equal(item.localNote, 'This is a test reminder synced from Apple Reminders.');
  assert.deepEqual(item.externalOrigin, {
    systemLabel: 'Apple Reminders',
    providerName: 'Apple Reminders (EventKit)',
    extensionId: 'lh-plugin-apple-reminders',
  });
});

test('source label comes from backend provenance, never from item type', () => {
  assert.deepEqual(formatPlannerItemSource(mappedReminder()), {
    source: 'Apple Reminders',
    provider: 'Apple Reminders (EventKit) · lh-plugin-apple-reminders',
  });
  // An EXTERNAL REMINDER without resolved provenance is not guessed to be Apple Reminders.
  assert.deepEqual(formatPlannerItemSource(mappedReminder({ externalOrigin: undefined })), {
    source: 'External source',
    provider: null,
  });
  assert.deepEqual(formatPlannerItemSource({ authority: 'LOCAL', externalOrigin: null }), {
    source: 'Lum Harbor',
    provider: null,
  });
});

test('Planner inspector renders Apple Reminders provenance and the Lum Harbor note', async () => {
  const DayPanel = await loadComponent<Record<string, unknown>>('src/components/planner/DayPanel.tsx');
  const item = mappedReminder();
  const text = visibleText(
    renderMarkup(DayPanel, {
      date: new Date(2026, 9, 20),
      today: '2026-10-20',
      timezone: 'UTC',
      calendarEvents: [],
      calendarItems: [item],
      currentOpenItems: [item],
      completedTodayItems: [],
      mode: 'day',
      onModeChange: () => {},
      selectedItem: item,
      onSelectItem: () => {},
      onCompletePlannerItem: async () => {},
      completingItemIds: new Set<string>(),
      onBack: () => {},
    }),
  );
  assert.match(text, /Source Apple Reminders/);
  assert.match(text, /Provided by Apple Reminders \(EventKit\) · lh-plugin-apple-reminders/);
  assert.match(text, /Managed by Apple Reminders/);
  assert.match(text, /Notes · from Apple Reminders Apple notes v2/);
  assert.match(text, /Lum Harbor note This is a test reminder synced from Apple Reminders\./);
  assert.doesNotMatch(text, /External source/);
});
