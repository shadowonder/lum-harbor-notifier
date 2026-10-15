import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  DEFAULT_EXTENSION_DISCLOSURE_STATE,
  buildIntegrationSearchEntries,
  canRemovePlugin,
  getActionDisplayLabel,
  getAIRuntimeModeDisplayLabel,
  getBuiltInProviderPresentation,
  getCapabilityDisplayLabel,
  parseRawConfiguration,
  searchSettings,
  updateSettingsValue,
} from '../src/settings/model.ts';
import type {
  IntegrationInstanceView,
  SettingsControlPlaneView,
  SettingsDataSource,
  SettingsSearchEntry,
} from '../src/settings/model.ts';
import {
  createPluginSettingsDataSource,
  mapAIRuntime,
  mapConfigFieldToDescriptor,
  mapPluginInventory,
  nativeNotificationPermissionWarning,
} from '../src/settings/pluginSettingsAdapter.ts';
import type { PluginInventoryItemDTO } from '../src/settings/pluginSettingsAdapter.ts';

const sourceFile = (path: string) =>
  readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const pluginDemoBase: SettingsControlPlaneView = {
  source: 'FRONTEND_DEMO',
  sourceLabel: 'Preview',
  platform: 'OTHER',
  landingCards: [],
  ai: {
    runtime: {
      mode: 'BUILTIN',
      effectiveProviderId: 'builtin',
      effectiveProviderName: 'Built-in',
      providerSource: 'BUILTIN',
      builtInConfigEditable: true,
      status: 'Ready',
    },
    builtInProvider: {
      name: 'Built-in',
      savedSummary: 'Preview',
      fields: [],
      config: {},
      useSystemCredentialManager: true,
      credentialConfigured: false,
      restartRequired: false,
    },
    providerStage: {
      id: 'builtin',
      name: 'Built-in',
      extensionName: 'Lum Harbor',
      stage: 'PROVIDER',
      enabled: true,
      actions: 'ALL',
      summary: 'Preview',
    },
    middleware: [],
  },
  integrations: {
    connected: [
      {
        id: 'rally-demo',
        typeId: 'rally',
        typeName: 'Rally',
        instanceName: 'Demo',
        extensionId: 'rally-demo-extension',
        extensionName: 'Rally Demo',
        health: 'neutral',
        status: 'Preview',
        detail: 'Preview',
      },
    ],
    available: [],
  },
  extensions: [],
  system: {
    startupStatus: 'Ready',
    configStatus: 'Preview',
    databaseStatus: 'Preview',
    backgroundStatus: 'Preview',
    jobs: [],
  },
  searchEntries: [
    {
      id: 'integration-rally-demo',
      label: 'Rally demo',
      keywords: ['rally'],
      page: 'integrations',
      category: 'Integrations',
      section: 'Connected',
    },
  ],
};

const pluginDemoSource: SettingsDataSource = {
  async load() {
    return pluginDemoBase;
  },
};

const searchEntries: SettingsSearchEntry[] = [
  {
    id: 'timeout',
    label: 'Request timeout',
    keywords: ['provider', 'timeout'],
    page: 'ai',
    category: 'AI',
    section: 'Built-in Provider',
    targetId: 'ai-built-in',
  },
  {
    id: 'openai',
    label: 'Built-in OpenAI provider',
    keywords: ['model', 'credentials'],
    page: 'ai',
    category: 'AI',
    section: 'Built-in Provider',
    targetId: 'ai-built-in',
  },
  {
    id: 'reminder',
    label: 'Personal',
    keywords: ['Apple Reminders'],
    page: 'integrations',
    category: 'Integrations',
    section: 'Connected',
    detailId: 'reminders-personal',
  },
];

const productLanguageSearchEntries: SettingsSearchEntry[] = [
  {
    id: 'action-source-interpretation',
    label: 'Source interpretation',
    keywords: ['source.interpret', 'source', 'current state'],
    page: 'ai',
    category: 'AI',
    section: 'Request Processing',
    targetId: 'ai-pipeline',
  },
  {
    id: 'capability-ai-input-processing',
    label: 'AI Input Processing',
    keywords: ['AI_PRE_MIDDLEWARE', 'Company Assistant'],
    page: 'extensions',
    category: 'Extensions',
    section: 'Company Assistant',
    targetId: 'extensions-installed',
    detailId: 'company-assistant',
  },
];

test('settings search matches labels and keywords with navigation metadata', () => {
  assert.equal(searchSettings(searchEntries, 'timeout')[0]?.targetId, 'ai-built-in');
  assert.equal(searchSettings(searchEntries, 'OPENAI')[0]?.page, 'ai');
  assert.equal(searchSettings(searchEntries, 'reminder')[0]?.detailId, 'reminders-personal');
  assert.deepEqual(searchSettings(searchEntries, 'missing'), []);
});

test('canonical actions map to friendly product labels', () => {
  assert.equal(getActionDisplayLabel('source.clean'), 'Source cleaning');
  assert.equal(getActionDisplayLabel('source.interpret'), 'Source interpretation');
  assert.equal(getActionDisplayLabel('assistant.ask'), 'Assistant questions');
  assert.equal(getActionDisplayLabel('assistant.proposal'), 'Assistant suggestions');
  assert.equal(getActionDisplayLabel('planner.proposal'), 'Planner suggestions');
  assert.equal(getActionDisplayLabel('notification.evaluate'), 'Notification evaluation');
  assert.equal(getActionDisplayLabel('work_item.analyze'), 'Work item analysis');
});

test('canonical capabilities map to friendly product labels', () => {
  assert.equal(getCapabilityDisplayLabel('AI_PROVIDER'), 'AI Provider');
  assert.equal(getCapabilityDisplayLabel('AI_PRE_MIDDLEWARE'), 'AI Input Processing');
  assert.equal(getCapabilityDisplayLabel('AI_POST_MIDDLEWARE'), 'AI Output Processing');
});

test('friendly and canonical action searches return the friendly label', () => {
  assert.equal(
    searchSettings(productLanguageSearchEntries, 'Source interpretation')[0]?.label,
    'Source interpretation',
  );
  assert.equal(
    searchSettings(productLanguageSearchEntries, 'source.interpret')[0]?.label,
    'Source interpretation',
  );
});

test('canonical capability search returns the friendly capability label', () => {
  assert.equal(
    searchSettings(productLanguageSearchEntries, 'AI_PRE_MIDDLEWARE')[0]?.label,
    'AI Input Processing',
  );
});

test('plugin provider mode has a product label instead of exposing the enum', () => {
  const label = getAIRuntimeModeDisplayLabel('PLUGIN_PROVIDER');
  assert.equal(label, 'Managed by extension');
  assert.notEqual(label, 'PLUGIN_PROVIDER');
});

test('advanced and developer disclosures are collapsed by default', () => {
  assert.deepEqual(DEFAULT_EXTENSION_DISCLOSURE_STATE, {
    advanced: false,
    developer: false,
  });
});

test('schema-driven updates return a new document without mutating the prior value', () => {
  const before = { endpoint: 'old', enabled: true };
  const after = updateSettingsValue(before, 'endpoint', 'new');
  assert.deepEqual(before, { endpoint: 'old', enabled: true });
  assert.deepEqual(after, { endpoint: 'new', enabled: true });
});

test('raw JSON rejects invalid input and accepts one logical config document', () => {
  const invalid = parseRawConfiguration('{bad');
  assert.equal(invalid.ok, false);
  const nested = parseRawConfiguration('{"nested":{"not":"supported"}}');
  assert.equal(nested.ok, false);
  const valid = parseRawConfiguration('{"timeout":30,"enabled":true}');
  assert.deepEqual(valid, { ok: true, value: { timeout: 30, enabled: true } });
});

test('plugin provider takeover makes built-in settings inactive and collapsed', () => {
  const presentation = getBuiltInProviderPresentation({
    mode: 'PLUGIN_PROVIDER',
    effectiveProviderId: 'company-ai',
    effectiveProviderName: 'Claude Local',
    providerSource: 'PLUGIN',
    builtInConfigEditable: false,
    status: 'Ready',
    managedByExtension: 'Company Assistant',
  });
  assert.equal(presentation.active, false);
  assert.equal(presentation.collapsedByDefault, true);
  assert.match(presentation.helperText, /Company Assistant/);
});

test('same integration type produces independently addressable instances', () => {
  const base = {
    typeId: 'rally',
    typeName: 'Rally',
    extensionId: 'lum.rally',
    extensionName: 'Rally Sources',
    health: 'healthy' as const,
    status: 'Healthy',
    lastActivity: 'Now',
    detail: 'Workspace',
  };
  const instances: IntegrationInstanceView[] = [
    { ...base, id: 'rally-team-a', instanceName: 'Team A' },
    { ...base, id: 'rally-team-b', instanceName: 'Team B' },
  ];
  const entries = buildIntegrationSearchEntries(instances);
  assert.deepEqual(
    entries.map((entry) => entry.detailId),
    ['rally-team-a', 'rally-team-b'],
  );
  assert.deepEqual(
    entries.map((entry) => entry.label),
    ['Team A', 'Team B'],
  );
});

test('real plugin inventory maps installed, selected, running, and configured instance facts', () => {
  const mapped = mapPluginInventory({
    plugins: [
      {
        id: 'lh-plugin-base',
        name: 'Lum Harbor Base Source',
        version: '1.0.0',
        protocolVersion: 1,
        installed: true,
        selected: true,
        running: true,
        runtimeStatus: 'RUNNING',
        capabilities: ['source.fetch'],
        selectableCapabilities: ['source.fetch'],
        bindings: [{ id: 'poll', type: 'SCHEDULE', capabilityId: 'source.fetch', actions: [] }],
        configSchema: [],
        instances: [
          {
            id: 'team-a',
            extensionId: 'lh-plugin-base',
            capabilityId: 'source.fetch',
            enabled: true,
          },
          {
            id: 'team-b',
            extensionId: 'lh-plugin-base',
            capabilityId: 'source.fetch',
            enabled: true,
          },
        ],
      },
      {
        id: 'installed-only',
        name: 'Installed Only',
        version: '2.0.0',
        protocolVersion: 1,
        installed: true,
        selected: false,
        running: false,
        runtimeStatus: 'DISCOVERED',
        capabilities: ['ai.provider'],
        selectableCapabilities: ['ai.provider'],
        bindings: [],
        configSchema: [],
        instances: [],
      },
    ],
  });
  assert.equal(mapped.extensions[0]?.status, 'Running');
  assert.equal(mapped.extensions[0]?.instances.length, 2);
  assert.equal(mapped.extensions[1]?.status, 'Installed · not selected');
  assert.equal(mapped.integrations.connected.length, 2);
  assert.equal(mapped.integrations.connected[0]?.enabled, true);
  assert.equal(mapped.integrations.connected[0]?.health, 'healthy');
  assert.equal(mapped.integrations.connected[0]?.status, 'Process running');
  assert.deepEqual(mapped.integrations.available, []);
});

test('native notification permission warnings belong to Plugin detail and distinguish denied from unavailable', () => {
  assert.deepEqual(nativeNotificationPermissionWarning('DENIED'), {
    title: 'Native notifications · Permission required',
    message: 'macOS notifications are disabled for this Plugin.',
    action: 'SYSTEM_SETTINGS',
  });
  assert.equal(nativeNotificationPermissionWarning('AUTHORIZED'), null);
  assert.equal(nativeNotificationPermissionWarning('PROVISIONAL'), null);
  assert.equal(nativeNotificationPermissionWarning('EPHEMERAL'), null);
  assert.equal(nativeNotificationPermissionWarning('UNKNOWN')?.action, 'RETRY');
  assert.equal(nativeNotificationPermissionWarning('CHECK_FAILED')?.action, 'RETRY');

  const settings = sourceFile('pages/Settings.tsx');
  const shell = sourceFile('components/AppShell.tsx');
  assert.match(settings, /NativeNotificationPermissionAlert/);
  assert.match(settings, /Open System Settings/);
  assert.doesNotMatch(
    shell,
    /macOS notifications are off|could not confirm native notification permission/,
  );
  assert.doesNotMatch(shell, /notifications\.macOSDelivery/);
});

test('Wails-backed SettingsDataSource replaces Plugin demo records and fake AI takeover', async () => {
  const source = createPluginSettingsDataSource(
    pluginDemoSource,
    async () => ({
      plugins: [
        {
          id: 'real-extension',
          name: 'Real Extension',
          version: '1.0.0',
          protocolVersion: 1,
          installed: true,
          selected: false,
          running: false,
          runtimeStatus: 'DISCOVERED',
          capabilities: ['source.fetch'],
          selectableCapabilities: ['source.fetch'],
          bindings: [],
          configSchema: [],
          instances: [],
        },
      ],
    }),
    async () => ({
      status: 'UNAVAILABLE',
      issue: 'Built-in OpenAI is not configured.',
      mode: 'BUILTIN',
      effectiveProviderId: 'builtin-openai',
      effectiveProviderName: 'Built-in OpenAI',
      providerSource: 'BUILTIN',
      builtInConfigEditable: true,
      stages: [
        {
          id: 'builtin-openai',
          name: 'Built-in OpenAI',
          stage: 'PROVIDER',
          order: 0,
          actions: [],
          allActions: true,
          enabled: false,
          available: false,
          unavailableReason: 'Built-in OpenAI is not configured.',
        },
      ],
    }),
    async () => ({
      endpoint: '',
      model: '',
      requestTimeoutSeconds: 30,
      useSystemCredentialManager: true,
      credentialConfigured: false,
      restartRequired: false,
    }),
    async () => 'MACOS',
  );
  const view = await source.load();
  assert.equal(view.source, 'MIXED');
  assert.equal(view.platform, 'MACOS');
  assert.deepEqual(
    view.extensions.map((extension) => extension.id),
    ['real-extension'],
  );
  assert.deepEqual(view.integrations.connected, []);
  assert.deepEqual(view.integrations.available, []);
  assert.equal(
    view.searchEntries.some((entry) => entry.id.includes('company-assistant')),
    false,
  );
  assert.equal(
    view.searchEntries.some((entry) => entry.id.includes('rally')),
    false,
  );
  assert.equal(
    view.searchEntries.some((entry) => entry.id === 'extension-real-extension'),
    true,
  );
  assert.equal(view.ai.runtime.status, 'Unavailable');
  assert.equal(view.ai.runtime.effectiveProviderName, 'Built-in OpenAI');
  assert.equal(view.ai.runtime.managedByExtension, undefined);
  assert.equal(view.ai.providerStage.enabled, false);
});

test('saved built-in settings never promote the current runtime before restart', () => {
  const view = mapAIRuntime(
    {
      status: 'UNAVAILABLE',
      issue: 'Built-in OpenAI configuration is missing in this process.',
      mode: 'BUILTIN',
      effectiveProviderId: 'builtin-openai',
      effectiveProviderName: 'Built-in OpenAI',
      providerSource: 'BUILTIN',
      builtInConfigEditable: true,
      stages: [
        {
          id: 'builtin-openai',
          name: 'Built-in OpenAI',
          stage: 'PROVIDER',
          order: 0,
          actions: [],
          allActions: true,
          enabled: false,
          available: false,
        },
      ],
    },
    {
      endpoint: 'https://api.openai.com/v1',
      model: 'gpt-test',
      requestTimeoutSeconds: 45,
      useSystemCredentialManager: true,
      credentialConfigured: true,
      restartRequired: true,
    },
  );
  assert.equal(view.runtime.status, 'Unavailable');
  assert.equal(view.providerStage.enabled, false);
  assert.equal(view.builtInProvider.savedSummary, 'Saved · gpt-test');
  assert.equal(view.builtInProvider.credentialConfigured, true);
  assert.equal(view.builtInProvider.restartRequired, true);
  assert.equal('credential' in view.builtInProvider.config, false);
});

test('mapAIRuntime reflects the currently saved desired credential store, not a fixed default', () => {
  const runtime = {
    status: 'UNAVAILABLE' as const,
    mode: 'BUILTIN' as const,
    effectiveProviderId: 'builtin-openai',
    effectiveProviderName: 'Built-in OpenAI',
    providerSource: 'BUILTIN' as const,
    builtInConfigEditable: true,
    stages: [],
  };
  const systemStoreView = mapAIRuntime(runtime, {
    endpoint: 'https://api.openai.com/v1',
    model: 'gpt-test',
    requestTimeoutSeconds: 30,
    useSystemCredentialManager: true,
    credentialConfigured: true,
    restartRequired: false,
  });
  assert.equal(systemStoreView.builtInProvider.useSystemCredentialManager, true);
  assert.equal(systemStoreView.builtInProvider.credentialConfigured, true);

  // Desired store switched to the application-managed fallback, which has
  // no credential yet — must show "missing" here, never fall back to the
  // no-longer-selected system store's true state (no automatic migration,
  // no stale credentialConfigured carry-over).
  const fallbackStoreView = mapAIRuntime(runtime, {
    endpoint: 'https://api.openai.com/v1',
    model: 'gpt-test',
    requestTimeoutSeconds: 30,
    useSystemCredentialManager: false,
    credentialConfigured: false,
    restartRequired: true,
  });
  assert.equal(fallbackStoreView.builtInProvider.useSystemCredentialManager, false);
  assert.equal(fallbackStoreView.builtInProvider.credentialConfigured, false);
  assert.equal(fallbackStoreView.builtInProvider.restartRequired, true);
});

test('mapAIRuntime defaults an omitted saved state to the system credential manager', () => {
  const view = mapAIRuntime({
    status: 'UNAVAILABLE',
    mode: 'BUILTIN',
    effectiveProviderId: 'builtin-openai',
    effectiveProviderName: 'Built-in OpenAI',
    providerSource: 'BUILTIN',
    builtInConfigEditable: true,
    stages: [],
  });
  assert.equal(view.builtInProvider.useSystemCredentialManager, true);
});

test('SettingsDataSource surfaces backend inventory errors without demo fallback', async () => {
  const source = createPluginSettingsDataSource(
    pluginDemoSource,
    async () => {
      throw new Error('inventory unavailable');
    },
    async () => {
      throw new Error('AI unavailable');
    },
    async () => ({
      endpoint: '',
      model: '',
      requestTimeoutSeconds: 30,
      useSystemCredentialManager: true,
      credentialConfigured: false,
      restartRequired: false,
    }),
    async () => 'OTHER',
  );
  await assert.rejects(source.load(), /inventory unavailable/);
});

test('AI runtime read failure becomes bounded unavailable state without demo fallback', async () => {
  const source = createPluginSettingsDataSource(
    pluginDemoSource,
    async () => ({ plugins: [] }),
    async () => {
      throw new Error('projection failed');
    },
    async () => ({
      endpoint: '',
      model: '',
      requestTimeoutSeconds: 30,
      useSystemCredentialManager: true,
      credentialConfigured: false,
      restartRequired: false,
    }),
    async () => 'WINDOWS',
  );
  const view = await source.load();
  assert.equal(view.extensions.length, 0);
  assert.equal(view.ai.runtime.status, 'Unavailable');
  assert.match(view.ai.runtime.issue ?? '', /projection failed/);
  assert.notEqual(view.ai.runtime.effectiveProviderName, 'Claude Local');
  assert.equal(view.platform, 'WINDOWS');
});

test('plugin takeover and pipeline identity come only from backend projection', () => {
  const aiView = mapAIRuntime({
    status: 'READY',
    mode: 'PLUGIN_PROVIDER',
    providerSource: 'PLUGIN',
    effectiveProviderId: 'real-ai:prod:provider',
    effectiveProviderName: 'Real AI',
    builtInConfigEditable: false,
    managingExtensionId: 'real-ai',
    managingExtensionName: 'Real AI Extension',
    managingProviderInstanceId: 'prod',
    stages: [
      {
        id: 'pre',
        name: 'context',
        stage: 'PRE',
        order: 10,
        actions: ['source.interpret'],
        allActions: false,
        enabled: true,
        available: true,
        extensionName: 'Real AI Extension',
      },
      {
        id: 'provider',
        name: 'Real AI',
        stage: 'PROVIDER',
        order: 20,
        actions: ['*'],
        allActions: true,
        enabled: true,
        available: true,
        extensionName: 'Real AI Extension',
      },
      {
        id: 'post',
        name: 'policy',
        stage: 'POST',
        order: 30,
        actions: ['*'],
        allActions: true,
        enabled: false,
        available: false,
        extensionName: 'Real AI Extension',
        unavailableReason: 'Extension process is unavailable.',
      },
    ],
  });
  assert.equal(aiView.runtime.effectiveProviderName, 'Real AI');
  assert.equal(aiView.runtime.managedByExtension, 'Real AI Extension');
  assert.equal(aiView.runtime.builtInConfigEditable, false);
  assert.deepEqual(
    aiView.middleware.map((stage) => [stage.name, stage.enabled]),
    [
      ['context', true],
      ['policy', false],
    ],
  );
  assert.equal(aiView.providerStage.enabled, true);
});

test('multiple-provider conflict never renders Ready or an enabled provider', () => {
  const aiView = mapAIRuntime({
    status: 'CONFLICT',
    issue: 'Multiple enabled AI providers.',
    mode: 'PLUGIN_PROVIDER',
    effectiveProviderId: '',
    effectiveProviderName: '',
    providerSource: 'PLUGIN',
    builtInConfigEditable: false,
    stages: [],
  });
  assert.equal(aiView.runtime.status, 'Conflict');
  assert.equal(aiView.providerStage.enabled, false);
  assert.equal(aiView.runtime.effectiveProviderName, 'No effective provider');
});

test('a disabled instance sharing a running extension process is never shown as running', () => {
  const mapped = mapPluginInventory({
    plugins: [
      {
        id: 'lh-plugin-base',
        name: 'Lum Harbor Base Source',
        version: '1.0.0',
        protocolVersion: 1,
        installed: true,
        selected: true,
        running: true,
        runtimeStatus: 'RUNNING',
        capabilities: ['source.fetch'],
        selectableCapabilities: ['source.fetch'],
        bindings: [],
        configSchema: [],
        instances: [
          {
            id: 'active-team',
            extensionId: 'lh-plugin-base',
            capabilityId: 'source.fetch',
            enabled: true,
          },
          {
            id: 'stopped-team',
            extensionId: 'lh-plugin-base',
            capabilityId: 'source.fetch',
            enabled: false,
          },
        ],
      },
    ],
  });

  const active = mapped.integrations.connected.find((instance) => instance.id === 'active-team');
  const stopped = mapped.integrations.connected.find((instance) => instance.id === 'stopped-team');

  assert.equal(active?.enabled, true);
  assert.equal(active?.health, 'healthy');
  assert.equal(active?.status, 'Process running');

  // The extension process is running (shared Host process), but this
  // specific Instance is disabled and must not read as running.
  assert.equal(stopped?.enabled, false);
  assert.equal(stopped?.health, 'inactive');
  assert.equal(stopped?.status, 'Disabled');

  // Extension-level ExtensionView.instances must also carry the same
  // per-instance enabled fact, independent of extension.running.
  const extension = mapped.extensions.find((item) => item.id === 'lh-plugin-base');
  const stoppedInstance = extension?.instances.find((instance) => instance.id === 'stopped-team');
  assert.equal(stoppedInstance?.enabled, false);
  assert.equal(extension?.running, true);
});

test('an extension with only disabled instances is not selected', () => {
  const mapped = mapPluginInventory({
    plugins: [
      {
        id: 'lh-plugin-base',
        name: 'Lum Harbor Base Source',
        version: '1.0.0',
        protocolVersion: 1,
        installed: true,
        selected: false,
        running: false,
        runtimeStatus: 'DISCOVERED',
        capabilities: ['source.fetch'],
        selectableCapabilities: ['source.fetch'],
        bindings: [],
        configSchema: [],
        instances: [
          {
            id: 'stopped-team',
            extensionId: 'lh-plugin-base',
            capabilityId: 'source.fetch',
            enabled: false,
          },
        ],
      },
    ],
  });

  const extension = mapped.extensions[0];
  assert.equal(extension?.selected, false);
  assert.equal(extension?.status, 'Installed · not selected');
  assert.equal(extension?.instances.length, 1);
  assert.equal(extension?.instances[0]?.enabled, false);
});

function pluginFixture(overrides: {
  selected: boolean;
  running: boolean;
  instances?: PluginInventoryItemDTO['instances'];
}): { plugins: PluginInventoryItemDTO[] } {
  return {
    plugins: [
      {
        id: 'lh-plugin-base',
        name: 'Lum Harbor Base Source',
        version: '1.0.0',
        protocolVersion: 1,
        installed: true,
        selected: overrides.selected,
        running: overrides.running,
        runtimeStatus: overrides.running ? 'RUNNING' : 'DISCOVERED',
        capabilities: ['source.fetch'],
        selectableCapabilities: ['source.fetch'],
        bindings: [],
        configSchema: [],
        instances: overrides.instances ?? [],
      },
    ],
  };
}

test('canRemovePlugin allows removal for an installed, unselected, stopped extension', () => {
  const mapped = mapPluginInventory(pluginFixture({ selected: false, running: false }));
  assert.equal(canRemovePlugin(mapped.extensions[0]), true);
});

test('canRemovePlugin blocks removal when an enabled instance exists', () => {
  const mapped = mapPluginInventory(
    pluginFixture({
      selected: true,
      running: false,
      instances: [
        {
          id: 'active-team',
          extensionId: 'lh-plugin-base',
          capabilityId: 'source.fetch',
          enabled: true,
        },
      ],
    }),
  );
  assert.equal(canRemovePlugin(mapped.extensions[0]), false);
});

test('canRemovePlugin blocks removal while the extension process is still running', () => {
  const mapped = mapPluginInventory(pluginFixture({ selected: false, running: true }));
  assert.equal(canRemovePlugin(mapped.extensions[0]), false);
});

test('canRemovePlugin allows removal when every instance is disabled and the process is stopped', () => {
  const mapped = mapPluginInventory(
    pluginFixture({
      selected: false,
      running: false,
      instances: [
        {
          id: 'stopped-team',
          extensionId: 'lh-plugin-base',
          capabilityId: 'source.fetch',
          enabled: false,
        },
      ],
    }),
  );
  assert.equal(canRemovePlugin(mapped.extensions[0]), true);
});

test('mapConfigFieldToDescriptor projects non-secret fields with defaults and visibility', () => {
  const descriptor = mapConfigFieldToDescriptor({
    key: 'endpoint',
    label: 'Endpoint',
    description: 'Source endpoint URL',
    type: 'string',
    required: true,
    default: 'https://example.test',
    visibility: 'SECONDARY',
  });
  assert.equal(descriptor.key, 'endpoint');
  assert.equal(descriptor.type, 'string');
  assert.equal(descriptor.required, true);
  assert.equal(descriptor.defaultValue, 'https://example.test');
  assert.equal(descriptor.visibility, 'secondary');
});

test('mapConfigFieldToDescriptor still projects secret fields so callers can detect required credentials', () => {
  const descriptor = mapConfigFieldToDescriptor({
    key: 'token',
    label: 'Token',
    type: 'secret',
    required: true,
  });
  assert.equal(descriptor.type, 'secret');
  assert.equal(descriptor.required, true);
  assert.equal(descriptor.visibility, 'primary');
});

test('real plugin inventory carries selectable capabilities and schema fields into ExtensionView', () => {
  const mapped = mapPluginInventory({
    plugins: [
      {
        id: 'lh-plugin-base',
        name: 'Lum Harbor Base Source',
        version: '1.0.0',
        protocolVersion: 1,
        installed: true,
        selected: false,
        running: false,
        runtimeStatus: 'DISCOVERED',
        capabilities: ['source.fetch', 'custom.unsupported'],
        selectableCapabilities: ['source.fetch'],
        bindings: [],
        configSchema: [
          { key: 'endpoint', label: 'Endpoint', type: 'string', required: true },
          { key: 'token', label: 'Token', type: 'secret', required: true },
        ],
        instances: [],
      },
    ],
  });
  const extension = mapped.extensions[0];
  assert.deepEqual(extension?.selectableCapabilities, ['source.fetch']);
  assert.equal(extension?.fields.length, 2);
  assert.equal(
    extension?.fields.some((field) => field.type === 'secret' && field.required),
    true,
  );
});
