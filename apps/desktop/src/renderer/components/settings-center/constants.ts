import type {
  LocalShellMode,
  LocalShellPreset,
  LocalShellPreference,
  SettingsSection,
} from "./types";

export const SECTIONS: Array<{ key: SettingsSection; label: string; icon: string }> = [
  { key: "window", label: "窗口行为", icon: "ri-window-line" },
  { key: "transfer", label: "文件传输", icon: "ri-upload-cloud-2-line" },
  { key: "editor", label: "远端编辑", icon: "ri-code-s-slash-line" },
  { key: "command", label: "命令中心", icon: "ri-terminal-box-line" },
  { key: "terminal", label: "终端主题", icon: "ri-palette-line" },
  { key: "network", label: "网络工具", icon: "ri-route-line" },
  { key: "backup", label: "云存档", icon: "ri-cloud-line" },
  { key: "security", label: "安全与审计", icon: "ri-shield-keyhole-line" },
  { key: "about", label: "关于", icon: "ri-information-line" },
];

export const EDITOR_PRESETS: Array<{ label: string; value: string }> = [
  { label: "VS Code", value: "code" },
  { label: "Cursor", value: "cursor" },
  { label: "Sublime", value: "subl" },
  { label: "Vim", value: "vim" },
  { label: "Nano", value: "nano" },
  { label: "Notepad++", value: "notepad++" },
  { label: "TextEdit", value: "open -t" },
  { label: "Xcode", value: "open -a Xcode" },
];

export const HEX_COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;
export const CUSTOM_THEME_PRESET = "custom";
export const CUSTOM_FONT_PRESET = "__terminal_font_custom__";
export const TERMINAL_THEME_PRESETS = [
  { label: "默认", value: "default", backgroundColor: "#000000", foregroundColor: "#d8eaff" },
  { label: "Dracula", value: "dracula", backgroundColor: "#282a36", foregroundColor: "#f8f8f2" },
  { label: "Solarized Dark", value: "solarized-dark", backgroundColor: "#002b36", foregroundColor: "#93a1a1" },
  { label: "Gruvbox Dark", value: "gruvbox-dark", backgroundColor: "#282828", foregroundColor: "#ebdbb2" },
  { label: "Nord", value: "nord", backgroundColor: "#2e3440", foregroundColor: "#d8dee9" },
] as const;

export const TERMINAL_DEBOUNCE_MS = 3000;
export const DEBUG_MAX_ENTRIES = 300;

export const appVersion = __APP_VERSION__;
export const githubRepo = __GITHUB_REPO__;
const normalizedRepo = githubRepo.trim();
export const hasRepo = normalizedRepo.length > 0;
export const displayRepo = hasRepo ? normalizedRepo : "owner/repo";
export const displayRepoUrl = `https://github.com/${displayRepo}`;
export const licenseUrl = `https://github.com/${displayRepo}/blob/main/LICENSE`;

export const DEFAULT_LOCAL_SHELL: LocalShellPreference = {
  mode: "preset",
  preset: "system",
  customPath: ""
};

export const isLocalShellMode = (value: unknown): value is LocalShellMode =>
  value === "preset" || value === "custom";

export const isLocalShellPreset = (value: unknown): value is LocalShellPreset =>
  value === "system" ||
  value === "powershell" ||
  value === "cmd" ||
  value === "zsh" ||
  value === "sh" ||
  value === "bash";

export const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

export const readLocalShellPreference = (
  terminal: Record<string, unknown> | undefined,
  platform: string
): LocalShellPreference => {
  const raw = terminal?.["localShell"];
  if (!raw || typeof raw !== "object") {
    return DEFAULT_LOCAL_SHELL;
  }

  const rawRecord = raw as Record<string, unknown>;
  const rawMode = rawRecord["mode"];
  const rawPreset = rawRecord["preset"];
  const rawCustomPath = rawRecord["customPath"];

  const mode = isLocalShellMode(rawMode)
    ? rawMode
    : DEFAULT_LOCAL_SHELL.mode;
  const customPath =
    typeof rawCustomPath === "string"
      ? String(rawCustomPath).trim()
      : "";

  if (mode === "custom") {
    return {
      mode,
      preset: isLocalShellPreset(rawPreset) ? rawPreset : DEFAULT_LOCAL_SHELL.preset,
      customPath
    };
  }

  const preset = isLocalShellPreset(rawPreset)
    ? rawPreset
    : DEFAULT_LOCAL_SHELL.preset;
  if (
    platform === "win32" &&
    preset !== "system" &&
    preset !== "powershell" &&
    preset !== "cmd"
  ) {
    return { ...DEFAULT_LOCAL_SHELL, customPath };
  }

  if (
    platform === "darwin" &&
    preset !== "system" &&
    preset !== "zsh" &&
    preset !== "sh"
  ) {
    return { ...DEFAULT_LOCAL_SHELL, customPath };
  }

  if (
    platform !== "win32" &&
    platform !== "darwin" &&
    preset !== "system" &&
    preset !== "bash" &&
    preset !== "sh"
  ) {
    return { ...DEFAULT_LOCAL_SHELL, customPath };
  }

  return {
    mode,
    preset,
    customPath
  };
};

export const getLocalShellOptions = (platform: string): Array<{ label: string; value: LocalShellPreset }> => {
  if (platform === "win32") {
    return [
      { label: "跟随系统（PowerShell）", value: "system" },
      { label: "PowerShell", value: "powershell" },
      { label: "CMD", value: "cmd" }
    ];
  }

  if (platform === "darwin") {
    return [
      { label: "跟随系统（zsh）", value: "system" },
      { label: "zsh", value: "zsh" },
      { label: "sh", value: "sh" }
    ];
  }

  return [
    { label: "跟随系统（bash/sh）", value: "system" },
    { label: "bash", value: "bash" },
    { label: "sh", value: "sh" }
  ];
};

export const resolvePresetByColors = (bg: string, fg: string): string => {
  const nb = bg.trim().toLowerCase();
  const nf = fg.trim().toLowerCase();
  const preset = TERMINAL_THEME_PRESETS.find(
    (p) => p.backgroundColor.toLowerCase() === nb && p.foregroundColor.toLowerCase() === nf
  );
  return preset?.value ?? CUSTOM_THEME_PRESET;
};

export const formatTimestamp = (ts: number): string => {
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
};

export const truncateCommand = (cmd: string, maxLen = 80): string => {
  return cmd.length > maxLen ? `${cmd.slice(0, maxLen)}…` : cmd;
};
