export { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
export { WindowSection } from "./window-section";
export { TransferSection } from "./transfer-section";
export { EditorSection } from "./editor-section";
export { CommandSection } from "./command-section";
export { TerminalSection } from "./terminal-section";
export { NetworkSection } from "./network-section";
export { RecycleBinSection } from "./recycle-bin-section";
export { BackupSection } from "./backup-section";
export { SecuritySection } from "./security-section";
export { AboutSection } from "./about-section";

export type {
  SettingsSection,
  LocalShellMode,
  LocalShellPreset,
  LocalShellPreference,
  SaveFn,
} from "./types";

export {
  SECTIONS,
  EDITOR_PRESETS,
  DEFAULT_LOCAL_SHELL,
  readLocalShellPreference,
  resolvePresetByColors,
} from "./constants";
