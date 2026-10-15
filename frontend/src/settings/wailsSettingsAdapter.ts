import {
  CreatePluginInstance,
  DeleteBuiltInOpenAICredential,
  GetAIRuntime,
  GetBuiltInOpenAISettings,
  GetPluginInventory,
  InstallPlugin,
  RemovePlugin,
  SaveBuiltInOpenAISettings,
  SelectPluginArtifactFile,
  SetPluginInstanceEnabled,
  SetBuiltInOpenAICredential,
  UpdatePluginInstanceConfig,
  VerifyAIRuntime,
} from '../../wailsjs/go/app/App';
import { getRuntimeConfig } from '../api/runtimeConfig.ts';
import { demoSettingsDataSource } from './demoSettingsAdapter';
import { createPluginSettingsDataSource } from './pluginSettingsAdapter';
import type { PluginManagementActions } from './pluginManagementActions';

export const settingsDataSource = createPluginSettingsDataSource(
  demoSettingsDataSource,
  GetPluginInventory,
  GetAIRuntime,
  GetBuiltInOpenAISettings,
  () => getRuntimeConfig().then((config) => config.platform),
);

export const aiSettingsActions = {
  saveSettings: SaveBuiltInOpenAISettings,
  setCredential: SetBuiltInOpenAICredential,
  deleteCredential: DeleteBuiltInOpenAICredential,
  verifyRuntime: VerifyAIRuntime,
};

export const pluginManagementActions: PluginManagementActions = {
  selectArtifactDirectory: SelectPluginArtifactFile,
  installPlugin: InstallPlugin,
  createPluginInstance: CreatePluginInstance,
  updatePluginInstanceConfig: UpdatePluginInstanceConfig,
  setPluginInstanceEnabled: SetPluginInstanceEnabled,
  removePlugin: RemovePlugin,
};

