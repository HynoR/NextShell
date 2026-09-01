export type SettingsSection =
  | "window"
  | "transfer"
  | "editor"
  | "command"
  | "terminal"
  | "network"
  | "cloudSync"
  | "recycleBin"
  | "security"
  | "agent"
  | "about";

export type LocalShellMode = "preset" | "custom";
export type LocalShellPreset = "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash";
export type LocalShellPreference = {
  mode: LocalShellMode;
  preset: LocalShellPreset;
  customPath: string;
};

export type TerminalWallpaperPreference = {
  seeThrough: boolean;
  useWebgl: boolean;
};

export type SaveFn = (patch: Record<string, unknown>) => void;
