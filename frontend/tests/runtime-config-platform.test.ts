import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { mapRuntimeConfigDTO } from '../src/api/runtimeConfigMapping.ts';
import type { app } from '../wailsjs/go/models.ts';

function dto(
  overrides: Partial<{
    todoDisplayLimit: number;
    platform: string;
    closeWindowBehavior?: string;
  }> = {},
) {
  return {
    general: { closeWindowBehavior: overrides.closeWindowBehavior ?? 'background' },
    dashboard: { todoDisplayLimit: overrides.todoDisplayLimit ?? 6 },
    notifications: {
      inApp: { autoDismiss: false, autoDismissSeconds: 600, placement: 'BOTTOM_RIGHT' },
    },
    platform: overrides.platform ?? 'MACOS',
  } as unknown as app.RuntimeConfigDTO;
}

test('mapRuntimeConfigDTO maps a real MACOS platform fact', () => {
  const config = mapRuntimeConfigDTO(dto({ platform: 'MACOS' }));
  assert.equal(config.platform, 'MACOS');
  assert.equal(config.dashboard.todoDisplayLimit, 6);
  assert.equal(config.general.closeWindowBehavior, 'background');
  assert.deepEqual(config.notifications.inApp, {
    autoDismiss: false,
    autoDismissSeconds: 600,
    placement: 'BOTTOM_RIGHT',
  });
});

test('mapRuntimeConfigDTO maps a real WINDOWS platform fact', () => {
  const config = mapRuntimeConfigDTO(dto({ platform: 'WINDOWS' }));
  assert.equal(config.platform, 'WINDOWS');
});

test('mapRuntimeConfigDTO maps an OTHER platform fact for any non-macOS/Windows GOOS', () => {
  const config = mapRuntimeConfigDTO(dto({ platform: 'OTHER' }));
  assert.equal(config.platform, 'OTHER');
});

test('mapRuntimeConfigDTO rejects an unrecognized platform value rather than passing it through', () => {
  assert.throws(() => mapRuntimeConfigDTO(dto({ platform: 'LINUX' })), /Invalid platform/);
  assert.throws(() => mapRuntimeConfigDTO(dto({ platform: '' })), /Invalid platform/);
  const missingPlatform = { dashboard: { todoDisplayLimit: 6 } } as unknown as app.RuntimeConfigDTO;
  assert.throws(() => mapRuntimeConfigDTO(missingPlatform), /Invalid platform/);
});

test('mapRuntimeConfigDTO still validates dashboard.todoDisplayLimit unchanged', () => {
  assert.throws(() => mapRuntimeConfigDTO(dto({ todoDisplayLimit: 0 })), /todoDisplayLimit/);
  assert.throws(() => mapRuntimeConfigDTO(dto({ todoDisplayLimit: 21 })), /todoDisplayLimit/);
});

test('mapRuntimeConfigDTO validates in-app notification preferences', () => {
  const invalidDuration = dto() as unknown as {
    notifications: {
      inApp: { autoDismiss: boolean; autoDismissSeconds: number; placement: string };
    };
  };
  invalidDuration.notifications.inApp.autoDismissSeconds = 6;
  assert.throws(
    () => mapRuntimeConfigDTO(invalidDuration as unknown as app.RuntimeConfigDTO),
    /autoDismissSeconds/,
  );

  const invalidPlacement = dto() as unknown as {
    notifications: {
      inApp: { autoDismiss: boolean; autoDismissSeconds: number; placement: string };
    };
  };
  invalidPlacement.notifications.inApp.placement = 'MIDDLE';
  assert.throws(
    () => mapRuntimeConfigDTO(invalidPlacement as unknown as app.RuntimeConfigDTO),
    /placement/,
  );
});

test('mapRuntimeConfigDTO preserves every supported in-app notification placement', () => {
  for (const placement of [
    'TOP_LEFT',
    'TOP_CENTER',
    'TOP_RIGHT',
    'BOTTOM_LEFT',
    'BOTTOM_CENTER',
    'BOTTOM_RIGHT',
  ]) {
    const value = dto() as unknown as {
      notifications: { inApp: { placement: string } };
    };
    value.notifications.inApp.placement = placement;
    assert.equal(
      mapRuntimeConfigDTO(value as unknown as app.RuntimeConfigDTO).notifications.inApp.placement,
      placement,
    );
  }
});

test('the frontend never infers platform from navigator/userAgent heuristics', () => {
  for (const relativePath of [
    '../src/api/runtimeConfig.ts',
    '../src/api/runtimeConfigMapping.ts',
  ]) {
    const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
    assert.ok(
      !/navigator\.(platform|userAgent)/.test(source),
      `${relativePath} must not use navigator heuristics`,
    );
  }
});

test('mapRuntimeConfigDTO maps General closeWindowBehavior: background and quit', () => {
  const bgConfig = mapRuntimeConfigDTO(dto({ closeWindowBehavior: 'background' }));
  assert.equal(bgConfig.general.closeWindowBehavior, 'background');

  const quitConfig = mapRuntimeConfigDTO(dto({ closeWindowBehavior: 'quit' }));
  assert.equal(quitConfig.general.closeWindowBehavior, 'quit');
});

test('mapRuntimeConfigDTO defaults missing general to background', () => {
  const missingGeneral = {
    dashboard: { todoDisplayLimit: 6 },
    notifications: {
      inApp: { autoDismiss: false, autoDismissSeconds: 600, placement: 'BOTTOM_RIGHT' },
    },
    platform: 'MACOS',
  } as unknown as app.RuntimeConfigDTO;
  const config = mapRuntimeConfigDTO(missingGeneral);
  assert.equal(config.general.closeWindowBehavior, 'background');
});

test('mapRuntimeConfigDTO rejects unrecognized general.closeWindowBehavior', () => {
  assert.throws(
    () => mapRuntimeConfigDTO(dto({ closeWindowBehavior: 'minimize' })),
    /Invalid general\.closeWindowBehavior/,
  );
  assert.throws(
    () => mapRuntimeConfigDTO(dto({ closeWindowBehavior: 'hide' })),
    /Invalid general\.closeWindowBehavior/,
  );
});
