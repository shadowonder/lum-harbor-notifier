import type {
  ExtensionView,
  HealthTone,
  IntegrationInstanceView,
  SettingsConfigValue,
  SettingsControlPlaneView,
  SettingsDataSource,
  SettingsFieldDescriptor,
  SettingsFieldType,
  SettingsFieldVisibility,
  SettingsPlatform,
} from './model.ts';
import { composeSettingsSearchEntries, getCapabilityDisplayLabel } from './model.ts';

export interface PluginInventoryBindingDTO {
  id: string;
  type: string;
  capabilityId: string;
  // Nullable defense-in-depth: a SCHEDULE binding's actions is an empty
  // array on the wire, but this stays optional so a future nil-slice
  // regression on the Go side degrades to an empty suffix instead of
  // crashing the whole inventory render.
  actions: string[] | null;
}

export interface ConfiguredPluginInstanceDTO {
  id: string;
  extensionId: string;
  capabilityId: string;
  enabled: boolean;
  nativeNotificationAuthorization?: string;
  config?: Record<string, SettingsConfigValue>;
}

export interface NativeNotificationPermissionWarning {
  title: string;
  message: string;
  action: 'SYSTEM_SETTINGS' | 'RETRY';
}

export function nativeNotificationPermissionWarning(
  status: string | undefined,
): NativeNotificationPermissionWarning | null {
  switch (status) {
    case 'DENIED':
      return {
        title: 'Native notifications · Permission required',
        message: 'macOS notifications are disabled for this Plugin.',
        action: 'SYSTEM_SETTINGS',
      };
    case 'NOT_DETERMINED':
      return {
        title: 'Native notifications · Permission required',
        message: 'macOS has not granted notification permission for this Plugin.',
        action: 'RETRY',
      };
    case 'UNKNOWN':
    case 'CHECK_FAILED':
      return {
        title: 'Native notifications · Permission status unavailable',
        message: 'Lum Harbor could not verify macOS notification permission.',
        action: 'RETRY',
      };
    default:
      return null;
  }
}

export interface PluginConfigOptionDTO {
  label: string;
  value: string;
}

export interface PluginConfigFieldDTO {
  key: string;
  label: string;
  description?: string;
  type: string;
  required: boolean;
  default?: SettingsConfigValue;
  options?: PluginConfigOptionDTO[];
  visibility?: string;
}

export interface PluginInventoryItemDTO {
  id: string;
  name: string;
  version: string;
  protocolVersion: number;
  installed: boolean;
  selected: boolean;
  running: boolean;
  runtimeStatus: string;
  capabilities: string[];
  selectableCapabilities: string[];
  bindings: PluginInventoryBindingDTO[];
  instances: ConfiguredPluginInstanceDTO[];
  configSchema: PluginConfigFieldDTO[];
}

export interface PluginInventoryDTO {
  plugins: PluginInventoryItemDTO[];
}

export interface AIRuntimeStageDTO {
  id: string;
  name: string;
  stage: string;
  order: number;
  actions: string[];
  allActions: boolean;
  enabled: boolean;
  available: boolean;
  extensionId?: string;
  extensionName?: string;
  instanceId?: string;
  unavailableReason?: string;
}

export interface AIRuntimeDTO {
  status: string;
  issue?: string;
  mode: string;
  effectiveProviderId: string;
  effectiveProviderName: string;
  providerSource: string;
  builtInConfigEditable: boolean;
  managingExtensionId?: string;
  managingExtensionName?: string;
  managingProviderInstanceId?: string;
  stages: AIRuntimeStageDTO[];
}

export interface BuiltInOpenAISettingsDTO {
  endpoint: string;
  model: string;
  requestTimeoutSeconds: number;
  useSystemCredentialManager: boolean;
  credentialConfigured: boolean;
  restartRequired: boolean;
}

export type PluginInventoryLoader = () => Promise<PluginInventoryDTO>;
export type AIRuntimeLoader = () => Promise<AIRuntimeDTO>;
export type BuiltInOpenAISettingsLoader = () => Promise<BuiltInOpenAISettingsDTO>;
export type PlatformLoader = () => Promise<SettingsPlatform>;

function runtimeStatus(status: string): 'Ready' | 'Unavailable' | 'Conflict' {
  if (status === 'READY') return 'Ready';
  if (status === 'CONFLICT') return 'Conflict';
  return 'Unavailable';
}

export function mapAIRuntime(
  runtime: AIRuntimeDTO,
  saved: BuiltInOpenAISettingsDTO = {
    endpoint: '',
    model: '',
    requestTimeoutSeconds: 30,
    useSystemCredentialManager: true,
    credentialConfigured: false,
    restartRequired: false,
  },
): SettingsControlPlaneView['ai'] {
  const stages = runtime.stages
    .filter(
      (stage) =>
        stage.stage === 'PRE' ||
        stage.stage === 'PROVIDER' ||
        stage.stage === 'RUNTIME' ||
        stage.stage === 'POST',
    )
    .map((stage) => ({
      id: stage.id,
      name: stage.name,
      extensionName: stage.extensionName ?? 'Lum Harbor',
      stage: stage.stage as 'PRE' | 'PROVIDER' | 'RUNTIME' | 'POST',
      enabled: stage.enabled && stage.available,
      actions: stage.allActions ? ('ALL' as const) : [...stage.actions],
      summary: stage.unavailableReason || `Runtime order ${stage.order}`,
    }));
  const primaryStage = stages.find((stage) =>
    runtime.mode === 'PLUGIN_RUNTIME' ? stage.stage === 'RUNTIME' : stage.stage === 'PROVIDER',
  ) ?? {
    id: 'provider-unavailable',
    name: runtime.effectiveProviderName || 'No effective provider',
    extensionName: runtime.managingExtensionName ?? 'Lum Harbor',
    stage: 'PROVIDER' as const,
    enabled: false,
    actions: 'ALL' as const,
    summary: runtime.issue ?? 'No provider is available.',
  };
  return {
    runtime: {
      mode:
        runtime.mode === 'PLUGIN_RUNTIME'
          ? 'PLUGIN_RUNTIME'
          : runtime.mode === 'PLUGIN_PROVIDER'
            ? 'PLUGIN_PROVIDER'
            : 'BUILTIN',
      effectiveProviderId: runtime.effectiveProviderId,
      effectiveProviderName: runtime.effectiveProviderName || 'No effective provider',
      providerSource: runtime.providerSource === 'PLUGIN' ? 'PLUGIN' : 'BUILTIN',
      builtInConfigEditable: runtime.builtInConfigEditable,
      status: runtimeStatus(runtime.status),
      issue: runtime.issue,
      managingExtensionId: runtime.managingExtensionId,
      managedByExtension: runtime.managingExtensionName,
    },
    builtInProvider: {
      name: 'Built-in OpenAI',
      savedSummary: saved.endpoint && saved.model ? `Saved · ${saved.model}` : 'Not configured',
      fields: [
        {
          key: 'endpoint',
          label: 'Endpoint',
          description: 'OpenAI-compatible API base URL.',
          type: 'string',
          required: true,
          visibility: 'primary',
        },
        {
          key: 'model',
          label: 'Model',
          description: 'Model used by the built-in provider after restart.',
          type: 'string',
          required: true,
          visibility: 'primary',
        },
        {
          key: 'requestTimeoutSeconds',
          label: 'Request timeout',
          description: 'Maximum provider request duration in seconds.',
          type: 'number',
          required: true,
          visibility: 'secondary',
        },
      ],
      config: {
        endpoint: saved.endpoint,
        model: saved.model,
        requestTimeoutSeconds: saved.requestTimeoutSeconds,
      },
      useSystemCredentialManager: saved.useSystemCredentialManager,
      credentialConfigured: saved.credentialConfigured,
      restartRequired: saved.restartRequired,
    },
    primaryStage,
    middleware: stages.filter((stage) => stage.stage !== 'PROVIDER'),
  };
}

function unavailableAIRuntime(message: string): AIRuntimeDTO {
  return {
    status: 'UNAVAILABLE',
    issue: `AI runtime could not be loaded: ${message}`,
    mode: 'BUILTIN',
    effectiveProviderId: '',
    effectiveProviderName: '',
    providerSource: 'BUILTIN',
    builtInConfigEditable: false,
    stages: [],
  };
}

function statusPresentation(plugin: PluginInventoryItemDTO): {
  health: HealthTone;
  status: string;
} {
  if (plugin.running) return { health: 'healthy', status: 'Running' };
  if (plugin.selected) return { health: 'attention', status: 'Selected · process stopped' };
  return { health: 'inactive', status: 'Installed · not selected' };
}

function bindingLabel(binding: PluginInventoryBindingDTO): string {
  const actions = binding.actions ?? [];
  const actionSuffix = actions.length ? ` · ${actions.join(', ')}` : '';
  return `${binding.type} · ${binding.capabilityId}${actionSuffix}`;
}

function mapConfigFieldVisibility(visibility: string | undefined): SettingsFieldVisibility {
  switch (visibility) {
    case 'SECONDARY':
      return 'secondary';
    case 'EXPERT':
      return 'expert';
    default:
      return 'primary';
  }
}

// mapConfigFieldToDescriptor projects a manifest schema field (including
// secret fields) into the frontend's schema-driven descriptor shape. Secret
// fields are still mapped here so Instance-creation UI can detect a required
// secret field and refuse to offer a form; the create-instance form itself
// filters secret fields out before rendering.
export function mapConfigFieldToDescriptor(field: PluginConfigFieldDTO): SettingsFieldDescriptor {
  return {
    key: field.key,
    label: field.label,
    description: field.description ?? '',
    type: field.type as SettingsFieldType,
    required: field.required,
    defaultValue: field.default,
    options: field.options,
    visibility: mapConfigFieldVisibility(field.visibility),
  };
}

export function mapPluginInventory(inventory: PluginInventoryDTO): {
  extensions: ExtensionView[];
  integrations: SettingsControlPlaneView['integrations'];
} {
  const extensions = inventory.plugins.map((plugin): ExtensionView => {
    const presentation = statusPresentation(plugin);
    return {
      id: plugin.id,
      displayName: plugin.name,
      version: plugin.version,
      installed: plugin.installed,
      selected: plugin.selected,
      running: plugin.running,
      runtimeStatus: plugin.runtimeStatus,
      health: presentation.health,
      status: presentation.status,
      summary:
        plugin.instances.length === 0
          ? 'Installed extension with no configured instances.'
          : `${plugin.instances.length} configured instance${plugin.instances.length === 1 ? '' : 's'}.`,
      capabilities: [...plugin.capabilities],
      selectableCapabilities: [...plugin.selectableCapabilities],
      bindings: plugin.bindings.map(bindingLabel),
      instances: plugin.instances.map((instance) => ({ ...instance })),
      fields: plugin.configSchema.map(mapConfigFieldToDescriptor),
      config: {},
      developer: { packageId: plugin.id, protocol: `Plugin RPC v${plugin.protocolVersion}` },
    };
  });

  const byID = new Map(inventory.plugins.map((plugin) => [plugin.id, plugin]));
  const connected = inventory.plugins.flatMap((plugin) =>
    plugin.instances.map((instance): IntegrationInstanceView => {
      const extension = byID.get(instance.extensionId) ?? plugin;
      // The Plugin Host runs one process per extension, not per Instance, so
      // "running" is never an Instance-level fact: a disabled Instance must
      // never appear "running" just because a sibling enabled Instance keeps
      // the shared extension process alive.
      const health: HealthTone = !instance.enabled
        ? 'inactive'
        : extension.running
          ? 'healthy'
          : 'attention';
      const status = !instance.enabled
        ? 'Disabled'
        : extension.running
          ? 'Process running'
          : 'Configured · process stopped';
      return {
        id: instance.id,
        typeId: instance.capabilityId,
        typeName: getCapabilityDisplayLabel(instance.capabilityId),
        instanceName: instance.id,
        extensionId: instance.extensionId,
        extensionName: extension.name,
        health,
        status,
        detail: `Capability · ${instance.capabilityId}`,
        enabled: instance.enabled,
      };
    }),
  );
  return { extensions, integrations: { connected, available: [] } };
}

export function mergePluginInventory(
  base: SettingsControlPlaneView,
  inventory: PluginInventoryDTO,
  aiRuntime: AIRuntimeDTO,
  builtInSettings: BuiltInOpenAISettingsDTO,
  platform: SettingsPlatform,
): SettingsControlPlaneView {
  const pluginView = mapPluginInventory(inventory);
  const runningCount = pluginView.extensions.filter((extension) => extension.running).length;
  const landingCards = base.landingCards.map((card) => {
    if (card.id === 'ai') {
      const ai = mapAIRuntime(aiRuntime, builtInSettings);
      return {
        ...card,
        summary: ai.runtime.status,
        detail:
          ai.runtime.status === 'Ready'
            ? ai.runtime.effectiveProviderName
            : (ai.runtime.issue ?? 'No AI provider is available'),
        tone: (ai.runtime.status === 'Ready' ? 'healthy' : 'attention') as HealthTone,
      };
    }
    if (card.id === 'extensions') {
      return {
        ...card,
        summary: `${pluginView.extensions.length} installed extension${pluginView.extensions.length === 1 ? '' : 's'}`,
        detail: `${runningCount} process${runningCount === 1 ? '' : 'es'} running`,
        tone: (runningCount > 0 ? 'healthy' : 'neutral') as HealthTone,
      };
    }
    if (card.id === 'integrations') {
      return {
        ...card,
        summary: `${pluginView.integrations.connected.length} configured instance${pluginView.integrations.connected.length === 1 ? '' : 's'}`,
        detail: 'Live startup selection',
        tone: 'neutral' as HealthTone,
      };
    }
    return card;
  });
  const withoutSearch: Omit<SettingsControlPlaneView, 'searchEntries'> = {
    ...base,
    source: 'MIXED',
    sourceLabel: 'Plugin inventory and AI runtime are live · other settings remain preview data',
    landingCards,
    ai: mapAIRuntime(aiRuntime, builtInSettings),
    extensions: pluginView.extensions,
    integrations: pluginView.integrations,
    platform,
  };
  const nonPluginSearchEntries = base.searchEntries.filter(
    (entry) => entry.page !== 'extensions' && entry.page !== 'integrations',
  );
  return {
    ...withoutSearch,
    searchEntries: composeSettingsSearchEntries(withoutSearch, nonPluginSearchEntries),
  };
}

export function createPluginSettingsDataSource(
  baseSource: SettingsDataSource,
  loadInventory: PluginInventoryLoader,
  loadAIRuntime: AIRuntimeLoader,
  loadBuiltInSettings: BuiltInOpenAISettingsLoader,
  loadPlatform: PlatformLoader,
): SettingsDataSource {
  return {
    async load() {
      const [base, inventory, aiResult, settingsResult, platform] = await Promise.all([
        baseSource.load(),
        loadInventory(),
        loadAIRuntime().then(
          (value) => ({ value }),
          (error: unknown) => ({ error }),
        ),
        loadBuiltInSettings().then(
          (value) => ({ value }),
          (error: unknown) => ({ error }),
        ),
        loadPlatform(),
      ]);
      const aiRuntime =
        'value' in aiResult
          ? aiResult.value
          : unavailableAIRuntime(
              aiResult.error instanceof Error ? aiResult.error.message : String(aiResult.error),
            );
      const builtInSettings =
        'value' in settingsResult
          ? settingsResult.value
          : {
              endpoint: '',
              model: '',
              requestTimeoutSeconds: 30,
              useSystemCredentialManager: true,
              credentialConfigured: false,
              restartRequired: false,
            };
      return mergePluginInventory(base, inventory, aiRuntime, builtInSettings, platform);
    },
  };
}
