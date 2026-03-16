import type { AppPreferences, AppPreferencesPatch } from "../../../../../packages/core/src/index";
import {
  normalizeBatchMaxConcurrency,
  normalizeBatchRetryCount
} from "../../../../../packages/core/src/index";

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

  const normalizeLocalShellPath = (
    value: string | undefined,
    fallback: string
  ): string => {
    if (typeof value !== "string") {
      return fallback;
    }
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : fallback;
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
    commandCenter: {
      rememberTemplateParams:
        patch.commandCenter?.rememberTemplateParams ?? current.commandCenter.rememberTemplateParams,
      batchMaxConcurrency: normalizeBatchMaxConcurrency(
        patch.commandCenter?.batchMaxConcurrency,
        current.commandCenter.batchMaxConcurrency
      ),
      batchRetryCount: normalizeBatchRetryCount(
        patch.commandCenter?.batchRetryCount,
        current.commandCenter.batchRetryCount
      )
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
      fontSize: normalizeTerminalFontSize(
        patch.terminal?.fontSize,
        current.terminal.fontSize
      ),
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
      }
    },
    ssh: {
      keepAliveEnabled: patch.ssh?.keepAliveEnabled ?? current.ssh.keepAliveEnabled,
      keepAliveIntervalSec: normalizeKeepAliveIntervalSec(
        patch.ssh?.keepAliveIntervalSec,
        current.ssh.keepAliveIntervalSec
      )
    },
    backup: {
      remotePath:
        patch.backup?.remotePath !== undefined
          ? patch.backup.remotePath
          : current.backup.remotePath,
      rclonePath:
        patch.backup?.rclonePath !== undefined ? patch.backup.rclonePath : current.backup.rclonePath,
      defaultBackupConflictPolicy:
        patch.backup?.defaultBackupConflictPolicy ?? current.backup.defaultBackupConflictPolicy,
      defaultRestoreConflictPolicy:
        patch.backup?.defaultRestoreConflictPolicy ?? current.backup.defaultRestoreConflictPolicy,
      rememberPassword: patch.backup?.rememberPassword ?? current.backup.rememberPassword,
      lastBackupAt:
        patch.backup?.lastBackupAt !== undefined ? patch.backup.lastBackupAt : current.backup.lastBackupAt
    },
    window: {
      appearance: normalizeWindowAppearance(
        patch.window?.appearance,
        current.window.appearance
      ),
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
        patch.window?.bottomWorkbenchDefaultCollapsed ?? current.window.bottomWorkbenchDefaultCollapsed
    },
    traceroute: {
      nexttracePath:
        patch.traceroute?.nexttracePath !== undefined
          ? patch.traceroute.nexttracePath
          : current.traceroute.nexttracePath,
      protocol:
        patch.traceroute?.protocol !== undefined ? patch.traceroute.protocol : current.traceroute.protocol,
      port: patch.traceroute?.port !== undefined ? patch.traceroute.port : current.traceroute.port,
      queries:
        patch.traceroute?.queries !== undefined ? patch.traceroute.queries : current.traceroute.queries,
      maxHops:
        patch.traceroute?.maxHops !== undefined ? patch.traceroute.maxHops : current.traceroute.maxHops,
      ipVersion:
        patch.traceroute?.ipVersion !== undefined ? patch.traceroute.ipVersion : current.traceroute.ipVersion,
      dataProvider:
        patch.traceroute?.dataProvider !== undefined
          ? patch.traceroute.dataProvider
          : current.traceroute.dataProvider,
      noRdns:
        patch.traceroute?.noRdns !== undefined ? patch.traceroute.noRdns : current.traceroute.noRdns,
      language:
        patch.traceroute?.language !== undefined ? patch.traceroute.language : current.traceroute.language,
      powProvider:
        patch.traceroute?.powProvider !== undefined
          ? patch.traceroute.powProvider
          : current.traceroute.powProvider,
      showTracerouteTab:
        patch.traceroute?.showTracerouteTab !== undefined
          ? patch.traceroute.showTracerouteTab
          : current.traceroute.showTracerouteTab
    },
    audit: {
      enabled:
        patch.audit?.enabled !== undefined
          ? patch.audit.enabled
          : current.audit.enabled,
      retentionDays:
        patch.audit?.retentionDays !== undefined &&
        Number.isInteger(patch.audit.retentionDays) &&
        patch.audit.retentionDays >= 0 &&
        patch.audit.retentionDays <= 365
          ? patch.audit.retentionDays
          : current.audit.retentionDays
    },
    ai: {
      enabled: patch.ai?.enabled ?? current.ai.enabled,
      activeProviderId:
        patch.ai?.activeProviderId !== undefined
          ? patch.ai.activeProviderId
          : current.ai.activeProviderId,
      providers: patch.ai?.providers ?? current.ai.providers,
      systemPromptOverride:
        patch.ai?.systemPromptOverride !== undefined
          ? patch.ai.systemPromptOverride
          : current.ai.systemPromptOverride,
      executionTimeoutSec:
        patch.ai?.executionTimeoutSec !== undefined &&
        Number.isInteger(patch.ai.executionTimeoutSec) &&
        patch.ai.executionTimeoutSec >= 5 &&
        patch.ai.executionTimeoutSec <= 300
          ? patch.ai.executionTimeoutSec
          : current.ai.executionTimeoutSec
    }
  };
};
