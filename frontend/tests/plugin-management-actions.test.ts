import assert from 'node:assert/strict';
import test from 'node:test';
import {
  addPlugin,
  createPluginInstance,
  removePlugin,
  setPluginInstanceEnabled,
} from '../src/settings/pluginManagementActions.ts';
import type {
  PluginInstallResultDTO,
  PluginInstanceCreationResultDTO,
  PluginInstanceMutationResultDTO,
  PluginManagementActions,
  PluginRemovalResultDTO,
} from '../src/settings/pluginManagementActions.ts';

function actionsWith(overrides: Partial<PluginManagementActions>): PluginManagementActions {
  return {
    selectArtifactDirectory: async () => '',
    installPlugin: async () => {
      throw new Error('installPlugin should not be called in this scenario');
    },
    createPluginInstance: async () => {
      throw new Error('createPluginInstance should not be called in this scenario');
    },
    setPluginInstanceEnabled: async () => {
      throw new Error('setPluginInstanceEnabled should not be called in this scenario');
    },
    removePlugin: async () => {
      throw new Error('removePlugin should not be called in this scenario');
    },
    ...overrides,
  };
}

test('addPlugin returns canceled and never calls installPlugin when the picker is dismissed', async () => {
  let installCalls = 0;
  const actions = actionsWith({
    selectArtifactDirectory: async () => '',
    installPlugin: async () => {
      installCalls += 1;
      throw new Error('must not be called');
    },
  });

  const outcome = await addPlugin(actions);

  assert.deepEqual(outcome, { status: 'canceled' });
  assert.equal(installCalls, 0);
});

test('addPlugin treats a whitespace-only selection as canceled', async () => {
  let installCalls = 0;
  const actions = actionsWith({
    selectArtifactDirectory: async () => '   ',
    installPlugin: async () => {
      installCalls += 1;
      throw new Error('must not be called');
    },
  });

  const outcome = await addPlugin(actions);

  assert.deepEqual(outcome, { status: 'canceled' });
  assert.equal(installCalls, 0);
});

test('addPlugin reports success with restart-required facts from a valid selection', async () => {
  const result: PluginInstallResultDTO = {
    extensionId: 'lh-plugin-base',
    version: '1.0.0',
    restartRequired: true,
  };
  const actions = actionsWith({
    selectArtifactDirectory: async () => '/Users/example/artifacts/lh-plugin-base',
    installPlugin: async (sourceDirectory) => {
      assert.equal(sourceDirectory, '/Users/example/artifacts/lh-plugin-base');
      return result;
    },
  });

  const outcome = await addPlugin(actions);

  assert.deepEqual(outcome, { status: 'installed', result });
});

test('addPlugin surfaces a backend validation error as a visible message', async () => {
  const actions = actionsWith({
    selectArtifactDirectory: async () => '/Users/example/artifacts/broken',
    installPlugin: async () => {
      throw new Error('selected folder is not a valid plugin artifact');
    },
  });

  const outcome = await addPlugin(actions);

  assert.deepEqual(outcome, {
    status: 'error',
    message: 'selected folder is not a valid plugin artifact',
  });
});

test('addPlugin surfaces an already-installed error as a visible message', async () => {
  const actions = actionsWith({
    selectArtifactDirectory: async () => '/Users/example/artifacts/lh-plugin-base',
    installPlugin: async () => {
      throw new Error('this plugin is already installed');
    },
  });

  const outcome = await addPlugin(actions);

  assert.deepEqual(outcome, { status: 'error', message: 'this plugin is already installed' });
});

test('addPlugin does not expose or require any inventory mutation dependency', async () => {
  // addPlugin's signature only depends on PluginManagementActions; it has no
  // way to mutate a settings/inventory view, so a successful install cannot
  // fake-update the current Plugin list. This is a structural guarantee, not
  // a runtime side effect to poll for.
  const result: PluginInstallResultDTO = {
    extensionId: 'lh-plugin-base',
    version: '2.0.0',
    restartRequired: true,
  };
  const actions = actionsWith({
    selectArtifactDirectory: async () => '/Users/example/artifacts/lh-plugin-base',
    installPlugin: async () => result,
  });

  const outcome = await addPlugin(actions);

  assert.equal(outcome.status, 'installed');
  assert.equal(Object.keys(actions).length, 5);
});

test('createPluginInstance reports success with the persisted instance facts', async () => {
  const result: PluginInstanceCreationResultDTO = {
    instanceId: 'lh-plugin-base-01997a3f',
    extensionId: 'lh-plugin-base',
    capabilityId: 'source.fetch',
    restartRequired: true,
  };
  const actions = actionsWith({
    createPluginInstance: async (request) => {
      assert.deepEqual(request, {
        extensionId: 'lh-plugin-base',
        capabilityId: 'source.fetch',
        config: { endpoint: 'https://example.test' },
      });
      return result;
    },
  });

  const outcome = await createPluginInstance(actions, {
    extensionId: 'lh-plugin-base',
    capabilityId: 'source.fetch',
    config: { endpoint: 'https://example.test' },
  });

  assert.deepEqual(outcome, { status: 'created', result });
});

test('createPluginInstance surfaces a backend validation error as a visible message', async () => {
  const actions = actionsWith({
    createPluginInstance: async () => {
      throw new Error('selected capability is not available for this plugin');
    },
  });

  const outcome = await createPluginInstance(actions, {
    extensionId: 'lh-plugin-base',
    capabilityId: 'custom.unsupported',
  });

  assert.deepEqual(outcome, {
    status: 'error',
    message: 'selected capability is not available for this plugin',
  });
});

test('createPluginInstance surfaces a required-secret rejection as a visible message', async () => {
  const actions = actionsWith({
    createPluginInstance: async () => {
      throw new Error(
        'instance "lh-plugin-base-01997a3f": configuration invalid: required field "token" is missing',
      );
    },
  });

  const outcome = await createPluginInstance(actions, {
    extensionId: 'lh-plugin-base',
    capabilityId: 'source.fetch',
  });

  assert.equal(outcome.status, 'error');
  assert.equal(
    outcome.status === 'error' && outcome.message.includes('required field "token" is missing'),
    true,
  );
});

test('setPluginInstanceEnabled reports success when disabling an instance', async () => {
  const result: PluginInstanceMutationResultDTO = {
    instanceId: 'lh-plugin-base-01997a3f',
    extensionId: 'lh-plugin-base',
    capabilityId: 'source.fetch',
    enabled: false,
    restartRequired: true,
  };
  const actions = actionsWith({
    setPluginInstanceEnabled: async (instanceId, enabled) => {
      assert.equal(instanceId, 'lh-plugin-base-01997a3f');
      assert.equal(enabled, false);
      return result;
    },
  });

  const outcome = await setPluginInstanceEnabled(actions, 'lh-plugin-base-01997a3f', false);

  assert.deepEqual(outcome, { status: 'updated', result });
});

test('setPluginInstanceEnabled reports success when re-enabling an instance', async () => {
  const result: PluginInstanceMutationResultDTO = {
    instanceId: 'lh-plugin-base-01997a3f',
    extensionId: 'lh-plugin-base',
    capabilityId: 'source.fetch',
    enabled: true,
    restartRequired: true,
  };
  const actions = actionsWith({
    setPluginInstanceEnabled: async (instanceId, enabled) => {
      assert.equal(instanceId, 'lh-plugin-base-01997a3f');
      assert.equal(enabled, true);
      return result;
    },
  });

  const outcome = await setPluginInstanceEnabled(actions, 'lh-plugin-base-01997a3f', true);

  assert.deepEqual(outcome, { status: 'updated', result });
});

test('setPluginInstanceEnabled surfaces a re-enable validation failure as a visible message', async () => {
  const actions = actionsWith({
    setPluginInstanceEnabled: async () => {
      throw new Error('selected plugin is not installed');
    },
  });

  const outcome = await setPluginInstanceEnabled(actions, 'lh-plugin-base-01997a3f', true);

  assert.deepEqual(outcome, { status: 'error', message: 'selected plugin is not installed' });
});

test('removePlugin reports success with the restart-required facts', async () => {
  const result: PluginRemovalResultDTO = {
    extensionId: 'lh-plugin-base',
    restartRequired: true,
  };
  const actions = actionsWith({
    removePlugin: async (extensionId) => {
      assert.equal(extensionId, 'lh-plugin-base');
      return result;
    },
  });

  const outcome = await removePlugin(actions, 'lh-plugin-base');

  assert.deepEqual(outcome, { status: 'removed', result });
});

test('removePlugin surfaces a backend eligibility rejection as a visible message', async () => {
  const actions = actionsWith({
    removePlugin: async () => {
      throw new Error(
        'stop using every instance of this plugin and restart Lum Harbor before removing it',
      );
    },
  });

  const outcome = await removePlugin(actions, 'lh-plugin-base');

  assert.deepEqual(outcome, {
    status: 'error',
    message: 'stop using every instance of this plugin and restart Lum Harbor before removing it',
  });
});

test('removePlugin surfaces a still-running-process rejection as a visible message', async () => {
  const actions = actionsWith({
    removePlugin: async () => {
      throw new Error(
        "this plugin's process is still running from this session; restart Lum Harbor before removing it",
      );
    },
  });

  const outcome = await removePlugin(actions, 'lh-plugin-base');

  assert.equal(outcome.status, 'error');
  assert.equal(
    outcome.status === 'error' && outcome.message.includes('restart Lum Harbor before removing it'),
    true,
  );
});
