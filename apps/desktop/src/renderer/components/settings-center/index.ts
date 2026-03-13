export { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
export { WindowSection } from "./window-section";
export { TransferSection } from "./transfer-section";
export { EditorSection } from "./editor-section";
export { CommandSection } from "./command-section";
export { TerminalSection } from "./terminal-section";
export { NetworkSection } from "./network-section";
export { CloudSyncSection } from "./cloud-sync-section";
export { CloudSyncV2Section } from "./cloud-sync-v2-section";
export { RecycleBinSection } from "./recycle-bin-section";
export { BackupSection } from "./backup-section";
export { SecuritySection } from "./security-section";
export { AboutSection } from "./about-section";

export type {
  SettingsSection,
  LocalShellMode,
  LocalShellPreset,
  LocalShellPreference,
  CloudSyncRuntimeState,
  CloudSyncStatusView,
  CloudSyncApi,
  SaveFn,
} from "./types";

export {
  SECTIONS,
  EDITOR_PRESETS,
  DEFAULT_LOCAL_SHELL,
  DEFAULT_CLOUD_SYNC_STATUS,
  readLocalShellPreference,
  resolvePresetByColors,
  getCloudSyncApi,
  normalizeCloudSyncStatus,
  normalizeCloudSyncConflicts,
  formatCloudSyncState,
  formatCloudSyncTime,
} from "./constants";
