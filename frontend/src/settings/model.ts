export type SettingsPageId =
  'landing' | 'general' | 'ai' | 'integrations' | 'notifications' | 'extensions' | 'system';

export type HealthTone = 'healthy' | 'neutral' | 'attention' | 'inactive';

/**
 * The application's own authoritative platform fact — never inferred by the
 * frontend from browser/renderer heuristics. Kept as its own local type here
 * (rather than importing `../api/runtimeConfig.ts`'s `Platform`) to match
 * this module's existing zero-dependency, self-contained boundary; the two
 * are intentionally the same three literal values.
 */
export type SettingsPlatform = 'MACOS' | 'WINDOWS' | 'OTHER';

export interface SettingsLandingCardView {
  id: Exclude<SettingsPageId, 'landing'>;
  title: string;
  description: string;
  summary: string;
  detail: string;
  tone: HealthTone;
}

export interface SettingsSearchEntry {
  id: string;
  label: string;
  keywords: string[];
  page: Exclude<SettingsPageId, 'landing'>;
  category: string;
  section: string;
  targetId?: string;
  detailId?: string;
}

export type ProviderSource = 'BUILTIN' | 'PLUGIN';
export type AIRuntimeMode = 'BUILTIN' | 'PLUGIN_PROVIDER' | 'PLUGIN_RUNTIME';

export interface AIRuntimeView {
  mode: AIRuntimeMode;
  effectiveProviderId: string;
  effectiveProviderName: string;
  providerSource: ProviderSource;
  builtInConfigEditable: boolean;
  status: 'Ready' | 'Unavailable' | 'Conflict';
  issue?: string;
  managingExtensionId?: string;
  managedByExtension?: string;
}

export interface AIMiddlewareBindingView {
  id: string;
  name: string;
  extensionName: string;
  stage: 'PRE' | 'PROVIDER' | 'RUNTIME' | 'POST';
  enabled: boolean;
  actions: string[] | 'ALL';
  summary: string;
}

export interface AISettingsView {
  runtime: AIRuntimeView;
  builtInProvider: {
    name: string;
    savedSummary: string;
    fields: SettingsFieldDescriptor[];
    config: SettingsConfigDocument;
    /**
     * macOS-only desired credential-store selection (true = system Keychain,
     * false = Lum Harbor's application-managed fallback). Kept as a plain
     * sibling field rather than a SettingsFieldDescriptor/config entry so the
     * generic schema-driven form never has to know about platform-conditional
     * visibility; the AI Settings page reads platform directly to decide
     * whether to render it at all.
     */
    useSystemCredentialManager: boolean;
    credentialConfigured: boolean;
    restartRequired: boolean;
  };
  primaryStage: AIMiddlewareBindingView;
  middleware: AIMiddlewareBindingView[];
}

export interface IntegrationInstanceView {
  id: string;
  typeId: string;
  typeName: string;
  instanceName: string;
  extensionId: string;
  extensionName: string;
  health: HealthTone;
  status: string;
  lastActivity?: string;
  detail: string;
  // enabled is the Instance's own desired-configuration fact (Stop
  // Using/Re-enable). It is independent of the parent extension's process
  // state: the Plugin Host runs one process per extension, not per Instance,
  // so a disabled Instance sharing an extension with a running one must never
  // be shown as "running" itself.
  enabled: boolean;
}

export interface AvailableIntegrationView {
  typeId: string;
  name: string;
  description: string;
  extensionName: string;
}

export type ExtensionCapability = string;

export const actionDisplayMetadata: Record<string, { label: string; keywords: string[] }> = {
  'source.clean': {
    label: 'Source cleaning',
    keywords: ['source', 'clean', 'normalization'],
  },
  'source.interpret': {
    label: 'Source interpretation',
    keywords: ['source', 'current state', 'interpretation'],
  },
  'assistant.ask': {
    label: 'Assistant questions',
    keywords: ['assistant', 'ask', 'question'],
  },
  'assistant.proposal': {
    label: 'Assistant suggestions',
    keywords: ['assistant', 'proposal', 'suggestions'],
  },
  'planner.proposal': {
    label: 'Planner suggestions',
    keywords: ['planner', 'proposal', 'suggestions'],
  },
  'notification.evaluate': {
    label: 'Notification evaluation',
    keywords: ['notification', 'evaluate', 'evaluation'],
  },
  'work_item.analyze': {
    label: 'Work item analysis',
    keywords: ['work item', 'analysis'],
  },
};

export const capabilityDisplayMetadata: Record<string, { label: string }> = {
  'source.fetch': { label: 'Source' },
  'ai.provider': { label: 'AI Provider' },
  'ai.runtime': { label: 'AI Runtime' },
  'ai.pre_request': { label: 'AI Input Processing' },
  'ai.post_response': { label: 'AI Output Processing' },
  SOURCE: { label: 'Source' },
  AI_PROVIDER: { label: 'AI Provider' },
  AI_PRE_MIDDLEWARE: { label: 'AI Input Processing' },
  AI_POST_MIDDLEWARE: { label: 'AI Output Processing' },
  NOTIFICATION: { label: 'Notifications' },
};

export const pipelineStageDisplayMetadata = {
  PRE: { label: 'Before AI' },
  PROVIDER: { label: 'AI Provider' },
  RUNTIME: { label: 'AI Runtime' },
  POST: { label: 'After AI' },
} as const;

export const DEFAULT_EXTENSION_DISCLOSURE_STATE = Object.freeze({
  advanced: false,
  developer: false,
});

function humanizeIdentifier(value: string): string {
  const words = value
    .replace(/[._-]+/g, ' ')
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (!words.length) return 'Unknown';
  return words
    .map((word, index) =>
      index === 0
        ? `${word.charAt(0).toUpperCase()}${word.slice(1).toLowerCase()}`
        : word.toLowerCase(),
    )
    .join(' ');
}

export function getActionDisplayLabel(action: string): string {
  return actionDisplayMetadata[action]?.label ?? humanizeIdentifier(action);
}

export function getCapabilityDisplayLabel(capability: ExtensionCapability): string {
  return capabilityDisplayMetadata[capability]?.label ?? humanizeIdentifier(capability);
}

export function getPipelineStageDisplayLabel(
  stage: 'PRE' | 'PROVIDER' | 'RUNTIME' | 'POST',
): string {
  return pipelineStageDisplayMetadata[stage].label;
}

export function getAIRuntimeModeDisplayLabel(mode: AIRuntimeMode): string {
  if (mode === 'PLUGIN_RUNTIME') return 'Extension runtime';
  return mode === 'PLUGIN_PROVIDER' ? 'Extension provider' : 'Built-in runtime';
}

export function getProviderSourceDisplayLabel(source: ProviderSource): string {
  return source === 'PLUGIN' ? 'Provided by extension' : 'Provided by Lum Harbor';
}

export type SettingsFieldType = 'string' | 'number' | 'boolean' | 'enum' | 'secret';
export type SettingsFieldVisibility = 'primary' | 'secondary' | 'expert';
export type SettingsConfigValue = string | number | boolean;
export type SettingsConfigDocument = Record<string, SettingsConfigValue>;

export interface SettingsFieldDescriptor {
  key: string;
  label: string;
  description: string;
  type: SettingsFieldType;
  value?: SettingsConfigValue;
  defaultValue?: SettingsConfigValue;
  required?: boolean;
  options?: { label: string; value: string }[];
  visibility?: SettingsFieldVisibility;
}

export interface ExtensionView {
  id: string;
  displayName: string;
  version: string;
  installed: boolean;
  selected: boolean;
  running: boolean;
  runtimeStatus: string;
  health: HealthTone;
  status: string;
  summary: string;
  capabilities: ExtensionCapability[];
  selectableCapabilities: ExtensionCapability[];
  bindings: string[];
  instances: {
    id: string;
    extensionId: string;
    capabilityId: string;
    enabled: boolean;
    nativeNotificationAuthorization?: string;
    config?: SettingsConfigDocument;
  }[];
  fields: SettingsFieldDescriptor[];
  config: SettingsConfigDocument;
  developer: { packageId: string; protocol: string };
}

// canRemovePlugin is the single source of truth for Remove eligibility in the
// UI: an extension with any enabled instance, or a process still running
// from this session, must never offer Remove. `selected` already means "at
// least one enabled instance" (see pluginSettingsAdapter's Selected
// projection), so this is a pure function of two existing facts, not a new
// one. The backend re-validates independently regardless of this UI gate.
export function canRemovePlugin(extension: ExtensionView): boolean {
  return !extension.selected && !extension.running;
}

export interface SystemRuntimeView {
  startupStatus: string;
  configStatus: string;
  databaseStatus: string;
  backgroundStatus: string;
  jobs: { id: string; label: string; status: string; detail: string }[];
}

export interface SettingsControlPlaneView {
  source: 'FRONTEND_DEMO' | 'MIXED';
  sourceLabel: string;
  landingCards: SettingsLandingCardView[];
  ai: AISettingsView;
  integrations: {
    connected: IntegrationInstanceView[];
    available: AvailableIntegrationView[];
  };
  extensions: ExtensionView[];
  system: SystemRuntimeView;
  searchEntries: SettingsSearchEntry[];
  /** Real backend fact (see `SettingsPlatform`), not part of the FRONTEND_DEMO preview domains. */
  platform: SettingsPlatform;
}

export interface SettingsDataSource {
  load(): Promise<SettingsControlPlaneView>;
}

export interface BuiltInProviderPresentation {
  active: boolean;
  collapsedByDefault: boolean;
  statusLabel: string;
  helperText: string;
}

export function getBuiltInProviderPresentation(
  runtime: AIRuntimeView,
): BuiltInProviderPresentation {
  if (runtime.builtInConfigEditable && runtime.status === 'Ready') {
    return {
      active: true,
      collapsedByDefault: false,
      statusLabel: 'Active',
      helperText: 'Built-in configuration is available for the effective provider.',
    };
  }
  return {
    active: false,
    collapsedByDefault: true,
    statusLabel: runtime.mode !== 'BUILTIN' ? 'Inactive' : 'Not configured',
    helperText:
      runtime.mode !== 'BUILTIN'
        ? runtime.status === 'Conflict'
          ? 'Multiple AI provider plugins are in conflict. Built-in configuration is inactive.'
          : `Managed by ${runtime.managedByExtension ?? runtime.effectiveProviderName ?? 'extension'}. Built-in configuration is inactive.`
        : 'Save configuration and a credential below, then restart before Current Runtime can change.',
  };
}

export function updateSettingsValue(
  document: SettingsConfigDocument,
  key: string,
  value: SettingsConfigValue,
): SettingsConfigDocument {
  return { ...document, [key]: value };
}

export type RawConfigurationParseResult =
  { ok: true; value: SettingsConfigDocument } | { ok: false; error: string };

export function parseRawConfiguration(raw: string): RawConfigurationParseResult {
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || Array.isArray(value) || typeof value !== 'object') {
      return { ok: false, error: 'Configuration must be a JSON object.' };
    }
    for (const [key, fieldValue] of Object.entries(value)) {
      if (
        typeof fieldValue !== 'string' &&
        typeof fieldValue !== 'number' &&
        typeof fieldValue !== 'boolean'
      ) {
        return { ok: false, error: `Field “${key}” must contain a string, number, or boolean.` };
      }
    }
    return { ok: true, value: value as SettingsConfigDocument };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof SyntaxError ? error.message : 'Configuration is not valid JSON.',
    };
  }
}

export function composeSettingsSearchEntries(
  view: Omit<SettingsControlPlaneView, 'searchEntries'>,
  baseEntries: SettingsSearchEntry[],
): SettingsSearchEntry[] {
  const integrationEntries = buildIntegrationSearchEntries(view.integrations.connected);
  const actionEntries = Array.from(
    new Set(
      view.ai.middleware.flatMap((middleware) =>
        middleware.actions === 'ALL' ? [] : middleware.actions,
      ),
    ),
  ).map((action) => ({
    id: `ai-action-${action}`,
    label: getActionDisplayLabel(action),
    keywords: [action, ...(actionDisplayMetadata[action]?.keywords ?? [])],
    page: 'ai' as const,
    category: 'AI',
    section: 'Request Processing',
    targetId: 'ai-pipeline',
  }));
  const extensionEntries = view.extensions.flatMap((extension) => [
    {
      id: `extension-${extension.id}`,
      label: extension.displayName,
      keywords: [extension.id, extension.version],
      page: 'extensions' as const,
      category: 'Extensions',
      section: 'Installed',
      targetId: 'extensions-installed',
      detailId: extension.id,
    },
    ...extension.capabilities.map((capability) => ({
      id: `extension-${extension.id}-capability-${capability}`,
      label: getCapabilityDisplayLabel(capability),
      keywords: [capability, extension.displayName, extension.id],
      page: 'extensions' as const,
      category: 'Extensions',
      section: extension.displayName,
      targetId: 'extensions-installed',
      detailId: extension.id,
    })),
    ...extension.fields.map((field) => ({
      id: `extension-${extension.id}-${field.key}`,
      label: field.label,
      keywords: [extension.displayName, extension.id, field.key, field.description],
      page: 'extensions' as const,
      category: 'Extensions',
      section: extension.displayName,
      targetId: 'extensions-installed',
      detailId: extension.id,
    })),
  ]);
  return [...baseEntries, ...actionEntries, ...integrationEntries, ...extensionEntries];
}

export function buildIntegrationSearchEntries(
  integrations: IntegrationInstanceView[],
): SettingsSearchEntry[] {
  return integrations.map((integration) => ({
    id: `integration-${integration.id}`,
    label: integration.instanceName,
    keywords: [integration.typeName, integration.status, integration.extensionId],
    page: 'integrations' as const,
    category: 'Integrations',
    section: 'Connected',
    targetId: 'integrations-connected',
    detailId: integration.id,
  }));
}

export function searchSettings(
  entries: SettingsSearchEntry[],
  query: string,
): SettingsSearchEntry[] {
  const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return [];
  return entries
    .filter((entry) => {
      const searchable = [entry.label, entry.category, entry.section, ...entry.keywords]
        .join(' ')
        .toLocaleLowerCase();
      return terms.every((term) => searchable.includes(term));
    })
    .slice(0, 8);
}
