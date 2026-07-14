export const TERMINAL_DEFAULT_FONT_FAMILY = "JetBrains Mono, Menlo, Monaco, monospace";

export interface TerminalFontPreset {
  label: string;
  value: string;
  platformPriority: {
    darwin: number;
    win32: number;
    linux: number;
    other: number;
  };
}

export const TERMINAL_FONT_PRESETS: TerminalFontPreset[] = [
  {
    label: "macOS 优先（SF Mono）",
    value: "SFMono-Regular, Menlo, Monaco, 'Cascadia Mono', 'JetBrains Mono', monospace",
    platformPriority: { darwin: 0, win32: 3, linux: 3, other: 3 }
  },
  {
    label: "Windows 优先（Cascadia Mono）",
    value: "'Cascadia Mono', Consolas, 'JetBrains Mono', 'Courier New', monospace",
    platformPriority: { darwin: 3, win32: 0, linux: 3, other: 3 }
  },
  {
    label: "Linux 优先（JetBrains Mono）",
    value:
      "'JetBrains Mono', 'DejaVu Sans Mono', 'Liberation Mono', 'Noto Sans Mono CJK SC', monospace",
    platformPriority: { darwin: 3, win32: 3, linux: 0, other: 3 }
  },
  {
    label: "默认（JetBrains Mono）",
    value: TERMINAL_DEFAULT_FONT_FAMILY,
    platformPriority: { darwin: 1, win32: 1, linux: 1, other: 1 }
  },
  {
    label: "Fira Code",
    value: "'Fira Code', 'JetBrains Mono', Menlo, Monaco, monospace",
    platformPriority: { darwin: 2, win32: 2, linux: 2, other: 2 }
  }
];

const platformKey = (platform: string): keyof TerminalFontPreset["platformPriority"] => {
  if (platform === "darwin" || platform === "win32" || platform === "linux") {
    return platform;
  }
  return "other";
};

export const getTerminalFontOptions = (
  platform: string
): Array<{ label: string; value: string }> => {
  const key = platformKey(platform);
  return [...TERMINAL_FONT_PRESETS]
    .sort((a, b) => {
      const diff = a.platformPriority[key] - b.platformPriority[key];
      if (diff !== 0) {
        return diff;
      }
      return a.label.localeCompare(b.label, "zh-CN");
    })
    .map((preset) => ({
      label: preset.label,
      value: preset.value
    }));
};

export const canonicalizeFontFamily = (value: string): string => {
  return value
    .split(",")
    .map((part) => {
      const trimmed = part.trim().replace(/^['"]+|['"]+$/g, "");
      return trimmed.replace(/\s+/g, " ").toLowerCase();
    })
    .filter((part) => part.length > 0)
    .join(",");
};
