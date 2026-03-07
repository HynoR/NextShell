import { existsSync } from "node:fs";
import path from "node:path";
import type { AppPreferences } from "../../../../../packages/core/src/index";

export type LocalShellPreference = AppPreferences["terminal"]["localShell"];
export type SupportedPlatform = "win32" | "darwin" | "linux" | string;

export interface LocalShellLaunch {
  command: string;
  args: string[];
  label: string;
}

const isPresetSupportedOnPlatform = (
  preset: LocalShellPreference["preset"],
  platform: SupportedPlatform
): boolean => {
  if (preset === "system") {
    return true;
  }

  if (platform === "win32") {
    return preset === "powershell" || preset === "cmd";
  }

  if (platform === "darwin") {
    return preset === "zsh" || preset === "sh";
  }

  return preset === "bash" || preset === "sh";
};

const basenameWithoutExtension = (value: string): string => {
  const ext = path.extname(value);
  return path.basename(value, ext);
};

const resolvePlatformSystemPreset = (platform: SupportedPlatform): LocalShellLaunch => {
  if (platform === "win32") {
    return {
      command: "powershell.exe",
      args: [],
      label: "PowerShell"
    };
  }

  if (platform === "darwin") {
    return {
      command: "/bin/zsh",
      args: [],
      label: "zsh"
    };
  }

  if (existsSync("/bin/bash")) {
    return {
      command: "/bin/bash",
      args: [],
      label: "bash"
    };
  }

  return {
    command: "/bin/sh",
    args: [],
    label: "sh"
  };
};

const PRESET_MAP: Record<Exclude<LocalShellPreference["preset"], "system">, LocalShellLaunch> = {
  powershell: {
    command: "powershell.exe",
    args: [],
    label: "PowerShell"
  },
  cmd: {
    command: "cmd.exe",
    args: [],
    label: "CMD"
  },
  zsh: {
    command: "/bin/zsh",
    args: [],
    label: "zsh"
  },
  sh: {
    command: "/bin/sh",
    args: [],
    label: "sh"
  },
  bash: {
    command: "/bin/bash",
    args: [],
    label: "bash"
  }
};

export const normalizeLocalShellPreference = (
  preference: LocalShellPreference,
  platform: SupportedPlatform
): LocalShellPreference => {
  if (preference.mode === "custom") {
    return preference;
  }

  if (isPresetSupportedOnPlatform(preference.preset, platform)) {
    return preference;
  }

  return {
    ...preference,
    preset: "system"
  };
};

export const resolveLocalShellLaunch = (
  preference: LocalShellPreference,
  platform: SupportedPlatform
): LocalShellLaunch => {
  const normalized = normalizeLocalShellPreference(preference, platform);

  if (normalized.mode === "custom") {
    return {
      command: normalized.customPath,
      args: [],
      label: basenameWithoutExtension(normalized.customPath) || "custom"
    };
  }

  if (normalized.preset === "system") {
    return resolvePlatformSystemPreset(platform);
  }

  return PRESET_MAP[normalized.preset];
};
