export interface PluginInstallResultDTO {
  extensionId: string;
  version: string;
  restartRequired: boolean;
}

export interface CreatePluginInstanceRequest {
  extensionId: string;
  capabilityId: string;
  config?: Record<string, string | number | boolean>;
}

export interface PluginInstanceCreationResultDTO {
  instanceId: string;
  extensionId: string;
  capabilityId: string;
  restartRequired: boolean;
}

export interface PluginInstanceMutationResultDTO {
  instanceId: string;
  extensionId: string;
  capabilityId: string;
  enabled: boolean;
  restartRequired: boolean;
}

export interface PluginRemovalResultDTO {
  extensionId: string;
  restartRequired: boolean;
}

export interface UpdatePluginInstanceConfigRequest {
  instanceId: string;
  config?: Record<string, string | number | boolean>;
}

// PluginManagementActions is the narrow transport boundary Settings depends
// on for the whole Plugin control plane (install, select/use, stop
// using/re-enable, remove). Page components never import generated Wails
// bindings directly.
export interface PluginManagementActions {
  selectArtifactDirectory(): Promise<string>;
  installPlugin(sourceDirectory: string): Promise<PluginInstallResultDTO>;
  createPluginInstance(request: CreatePluginInstanceRequest): Promise<PluginInstanceCreationResultDTO>;
  updatePluginInstanceConfig?(
    request: UpdatePluginInstanceConfigRequest,
  ): Promise<PluginInstanceMutationResultDTO>;
  setPluginInstanceEnabled(
    instanceId: string,
    enabled: boolean,
  ): Promise<PluginInstanceMutationResultDTO>;
  removePlugin(extensionId: string): Promise<PluginRemovalResultDTO>;
}

export type AddPluginOutcome =
  | { status: 'canceled' }
  | { status: 'installed'; result: PluginInstallResultDTO }
  | { status: 'error'; message: string };

// addPlugin drives the user-selection -> install flow. A cancelled directory
// picker never reaches installPlugin. Installing never mutates any inventory
// view by itself; the caller decides how to present the outcome.
export async function addPlugin(actions: PluginManagementActions): Promise<AddPluginOutcome> {
  const sourceDirectory = await actions.selectArtifactDirectory();
  if (!sourceDirectory.trim()) {
    return { status: 'canceled' };
  }
  try {
    const result = await actions.installPlugin(sourceDirectory);
    return { status: 'installed', result };
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) };
  }
}

export type CreateInstanceOutcome =
  | { status: 'created'; result: PluginInstanceCreationResultDTO }
  | { status: 'error'; message: string };

// createPluginInstance submits a capability + non-secret configuration for an
// installed Plugin. It never mutates any inventory view by itself; the caller
// decides how to present the outcome (typically: close the form and show a
// restart-required notice).
export async function createPluginInstance(
  actions: PluginManagementActions,
  request: CreatePluginInstanceRequest,
): Promise<CreateInstanceOutcome> {
  try {
    const result = await actions.createPluginInstance(request);
    return { status: 'created', result };
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) };
  }
}

export type SetInstanceEnabledOutcome =
  | { status: 'updated'; result: PluginInstanceMutationResultDTO }
  | { status: 'error'; message: string };

// setPluginInstanceEnabled toggles Stop Using (enabled=false) / Use Again
// (enabled=true) for an already-configured Instance. It never mutates any
// inventory view by itself; the caller decides how to present the outcome
// (typically: show a restart-required notice, keeping the current runtime
// facts unchanged until restart).
export async function setPluginInstanceEnabled(
  actions: PluginManagementActions,
  instanceId: string,
  enabled: boolean,
): Promise<SetInstanceEnabledOutcome> {
  try {
    const result = await actions.setPluginInstanceEnabled(instanceId, enabled);
    return { status: 'updated', result };
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) };
  }
}

export type RemovePluginOutcome =
  | { status: 'removed'; result: PluginRemovalResultDTO }
  | { status: 'error'; message: string };

// removePlugin uninstalls a Plugin's artifact and cleans up its now-dangling
// disabled Instance declarations. It never mutates any inventory view by
// itself; the caller decides how to present the outcome (typically: show a
// restart-required notice, keeping the current runtime facts unchanged until
// restart).
export async function removePlugin(
  actions: PluginManagementActions,
  extensionId: string,
): Promise<RemovePluginOutcome> {
  try {
    const result = await actions.removePlugin(extensionId);
    return { status: 'removed', result };
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) };
  }
}

export type UpdateInstanceConfigOutcome =
  | { status: 'updated'; result: PluginInstanceMutationResultDTO }
  | { status: 'error'; message: string };

// updatePluginInstanceConfig submits updated non-secret configuration for an
// already-configured Plugin Instance. It never mutates running processes;
// the caller presents the outcome (restart-required notice).
export async function updatePluginInstanceConfig(
  actions: PluginManagementActions,
  request: UpdatePluginInstanceConfigRequest,
): Promise<UpdateInstanceConfigOutcome> {
  try {
    if (!actions.updatePluginInstanceConfig) {
      throw new Error('Plugin configuration editing is not supported');
    }
    const result = await actions.updatePluginInstanceConfig(request);
    return { status: 'updated', result };
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) };
  }
}

