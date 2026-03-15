export type SettingsSection =
  | "window"
  | "transfer"
  | "editor"
  | "command"
  | "terminal"
  | "network"
  | "backup"
  | "security"
  | "about";

export type LocalShellMode = "preset" | "custom";
export type LocalShellPreset = "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash";
export type LocalShellPreference = {
  mode: LocalShellMode;
  preset: LocalShellPreset;
  customPath: string;
};

export type SaveFn = (patch: Record<string, unknown>) => void;
