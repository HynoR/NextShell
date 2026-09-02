import type { AppPreferences, AppPreferencesPatch } from "../../../../../packages/core/src/index";

export const mergePreferences = (
  current: AppPreferences,
  patch: AppPreferencesPatch
): AppPreferences => {
  const normalizeWindowAppearance = (
    value: "system" | "light" | "dark" | undefined,
    fallback: AppPreferences["window"]["appearance"]
  ): AppPreferences["window"]["appearance"] => {
    if (value === "system" || value === "light" || value === "dark") {
      return value;
    }
    return fallback;
  };

  const normalizeTerminalColor = (value: string | undefined, fallback: string): string => {
    const trimmed = value?.trim();
    if (!trimmed || !/^#[0-9a-fA-F]{6}$/.test(trimmed)) {
      return fallback;
    }
    return trimmed;
  };

  const normalizeTerminalFontSize = (value: number | undefined, fallback: number): number => {
    if (!Number.isInteger(value) || (value ?? 0) < 10 || (value ?? 0) > 24) {
      return fallback;
    }
    return value as number;
  };

  const normalizeTerminalLineHeight = (value: number | undefined, fallback: number): number => {
    if (!Number.isFinite(value) || (value ?? 0) < 1 || (value ?? 0) > 2) {
      return fallback;
    }
    return value as number;
  };

  const normalizeTerminalFontFamily = (value: string | undefined, fallback: string): string => {
    if (typeof value !== "string") {
      return fallback;
    }
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : fallback;
  };

  const normalizeLocalShellMode = (
    value: "preset" | "custom" | undefined,
    fallback: AppPreferences["terminal"]["localShell"]["mode"]
  ): AppPreferences["terminal"]["localShell"]["mode"] => {
    if (value === "preset" || value === "custom") {
      return value;
    }
    return fallback;
  };

  const normalizeLocalShellPreset = (
    value: "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash" | undefined,
    fallback: AppPreferences["terminal"]["localShell"]["preset"]
  ): AppPreferences["terminal"]["localShell"]["preset"] => {
    if (
      value === "system" ||
      value === "powershell" ||
      value === "cmd" ||
      value === "zsh" ||
      value === "sh" ||
      value === "bash"
    ) {
      return value;
    }
    return fallback;
  };

  const normalizeLocalShellPath = (value: string | undefined, fallback: string): string => {
    if (typeof value !== "string") {
      return fallback;
    }
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : fallback;
  };

  const normalizeShellIntegrationMode = (
    value: "auto" | "off" | "manual" | undefined,
    fallback: AppPreferences["terminal"]["shellIntegration"]
  ): AppPreferences["terminal"]["shellIntegration"] => {
    if (value === "auto" || value === "off" || value === "manual") {
      return value;
    }
    return fallback;
  };

  const normalizeBackgroundOpacity = (value: number | undefined, fallback: number): number => {
    if (!Number.isFinite(value)) {
      return fallback;
    }
    const rounded = Math.round(value as number);
    if (rounded < 30 || rounded > 80) {
      return fallback;
    }
    return rounded;
  };

  const normalizeKeepAliveIntervalSec = (value: number | undefined, fallback: number): number => {
    if (!Number.isInteger(value) || (value ?? 0) < 5 || (value ?? 0) > 600) {
      return fallback;
    }
    return value as number;
  };

  const normalizeBoundedInt = (
    value: number | undefined,
    fallback: number,
    min: number,
    max: number
  ): number => {
    if (!Number.isInteger(value) || (value as number) < min || (value as number) > max) {
      return fallback;
    }
    return value as number;
  };

  const normalizeStringList = (value: string[] | undefined, fallback: string[]): string[] => {
    if (!Array.isArray(value)) {
      return fallback;
    }
    return value.map((entry) => entry.trim()).filter((entry) => entry.length > 0);
  };

  return {
    transfer: {
      uploadDefaultDir:
        patch.transfer?.uploadDefaultDir?.trim() || current.transfer.uploadDefaultDir,
      downloadDefaultDir:
        patch.transfer?.downloadDefaultDir?.trim() || current.transfer.downloadDefaultDir
    },
    remoteEdit: {
      defaultEditorCommand:
        patch.remoteEdit?.defaultEditorCommand !== undefined
          ? patch.remoteEdit.defaultEditorCommand.trim()
          : current.remoteEdit.defaultEditorCommand,
      editorMode: patch.remoteEdit?.editorMode ?? current.remoteEdit.editorMode
    },
    terminal: {
      backgroundColor: normalizeTerminalColor(
        patch.terminal?.backgroundColor,
        current.terminal.backgroundColor
      ),
      foregroundColor: normalizeTerminalColor(
        patch.terminal?.foregroundColor,
        current.terminal.foregroundColor
      ),
      fontSize: normalizeTerminalFontSize(patch.terminal?.fontSize, current.terminal.fontSize),
      lineHeight: normalizeTerminalLineHeight(
        patch.terminal?.lineHeight,
        current.terminal.lineHeight
      ),
      fontFamily: normalizeTerminalFontFamily(
        patch.terminal?.fontFamily,
        current.terminal.fontFamily
      ),
      localShell: {
        mode: normalizeLocalShellMode(
          patch.terminal?.localShell?.mode,
          current.terminal.localShell.mode
        ),
        preset: normalizeLocalShellPreset(
          patch.terminal?.localShell?.preset,
          current.terminal.localShell.preset
        ),
        customPath: normalizeLocalShellPath(
          patch.terminal?.localShell?.customPath,
          current.terminal.localShell.customPath
        )
      },
      oscClipboardWrite: patch.terminal?.oscClipboardWrite ?? current.terminal.oscClipboardWrite,
      oscClipboardRead: patch.terminal?.oscClipboardRead ?? current.terminal.oscClipboardRead,
      oscNotifications: patch.terminal?.oscNotifications ?? current.terminal.oscNotifications,
      oscTitleUpdates: patch.terminal?.oscTitleUpdates ?? current.terminal.oscTitleUpdates,
      hyperlinkConfirm: patch.terminal?.hyperlinkConfirm ?? current.terminal.hyperlinkConfirm,
      shellIntegration: normalizeShellIntegrationMode(
        patch.terminal?.shellIntegration,
        current.terminal.shellIntegration
      ),
      wallpaper: {
        seeThrough: patch.terminal?.wallpaper?.seeThrough ?? current.terminal.wallpaper.seeThrough,
        useWebgl: patch.terminal?.wallpaper?.useWebgl ?? current.terminal.wallpaper.useWebgl
      }
    },
    ssh: {
      keepAliveEnabled: patch.ssh?.keepAliveEnabled ?? current.ssh.keepAliveEnabled,
      keepAliveIntervalSec: normalizeKeepAliveIntervalSec(
        patch.ssh?.keepAliveIntervalSec,
        current.ssh.keepAliveIntervalSec
      )
    },
    connectionManager: {
      // 尺寸由契约层夹过范围，这里只做“没给就保持原值”。
      dialogWidth: patch.connectionManager?.dialogWidth ?? current.connectionManager.dialogWidth,
      dialogHeight: patch.connectionManager?.dialogHeight ?? current.connectionManager.dialogHeight,
      folderColumnWidth:
        patch.connectionManager?.folderColumnWidth ?? current.connectionManager.folderColumnWidth,
      detailColumnWidth:
        patch.connectionManager?.detailColumnWidth ?? current.connectionManager.detailColumnWidth
    },
    window: {
      appearance: normalizeWindowAppearance(patch.window?.appearance, current.window.appearance),
      minimizeToTray: patch.window?.minimizeToTray ?? current.window.minimizeToTray,
      confirmBeforeClose: patch.window?.confirmBeforeClose ?? current.window.confirmBeforeClose,
      backgroundImagePath:
        patch.window?.backgroundImagePath !== undefined
          ? patch.window.backgroundImagePath.trim()
          : current.window.backgroundImagePath,
      backgroundOpacity: normalizeBackgroundOpacity(
        patch.window?.backgroundOpacity,
        current.window.backgroundOpacity
      ),
      leftSidebarDefaultCollapsed:
        patch.window?.leftSidebarDefaultCollapsed ?? current.window.leftSidebarDefaultCollapsed,
      bottomWorkbenchDefaultCollapsed:
        patch.window?.bottomWorkbenchDefaultCollapsed ??
        current.window.bottomWorkbenchDefaultCollapsed
    },
    traceroute: {
      nexttracePath:
        patch.traceroute?.nexttracePath !== undefined
          ? patch.traceroute.nexttracePath
          : current.traceroute.nexttracePath,
      powProvider:
        patch.traceroute?.powProvider !== undefined
          ? patch.traceroute.powProvider
          : current.traceroute.powProvider
    },
    agent: {
      enabled: patch.agent?.enabled !== undefined ? patch.agent.enabled : current.agent.enabled,
      execTimeoutSec: normalizeBoundedInt(
        patch.agent?.execTimeoutSec,
        current.agent.execTimeoutSec,
        1,
        3600
      ),
      blacklist: normalizeStringList(patch.agent?.blacklist, current.agent.blacklist)
    }
  };
};
