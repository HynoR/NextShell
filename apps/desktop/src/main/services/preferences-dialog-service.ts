import fs from "node:fs";
import { BrowserWindow, dialog, shell } from "electron";
import type { OpenDialogOptions, WebContents } from "electron";
import type { AppPreferences } from "@nextshell/core";
import type {
  DebugLogEntry,
  DialogOpenDirectoryInput,
  DialogOpenFilesInput,
  DialogOpenPathInput,
  SettingsUpdateInput,
} from "@nextshell/shared";
import { IPCChannel } from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";
import { mergePreferences } from "./preferences";
import { applyAppearanceToAllWindows } from "../window-theme";
import { resolveLocalPath, parseExternalUrl } from "./container-utils";
import { logger } from "../logger";

const DEBUG_FLUSH_INTERVAL_MS = 200;
const DEBUG_MAX_PENDING = 50;

export interface PreferencesDialogServiceOptions {
  connections: CachedConnectionRepository;
  auditEnabledForSession: boolean;
  getCloudSyncService: () =>
    | { refreshFromPreferences: (opts: { triggerPull: boolean }) => Promise<void> }
    | undefined;
}

export class PreferencesDialogService {
  private readonly connections: CachedConnectionRepository;
  private readonly auditEnabledForSession: boolean;
  private readonly getCloudSyncService: PreferencesDialogServiceOptions["getCloudSyncService"];

  readonly debugSenders = new Set<WebContents>();
  private debugPending: DebugLogEntry[] = [];
  private debugFlushTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(options: PreferencesDialogServiceOptions) {
    this.connections = options.connections;
    this.auditEnabledForSession = options.auditEnabledForSession;
    this.getCloudSyncService = options.getCloudSyncService;
  }

  // ---------------------------------------------------------------------------
  // Preferences
  // ---------------------------------------------------------------------------

  getAppPreferences(): AppPreferences {
    return this.connections.getAppPreferences();
  }

  saveAppPreferencesPatch(
    patch: SettingsUpdateInput,
    options?: { reconfigureCloudSync?: boolean },
  ): AppPreferences {
    const current = this.connections.getAppPreferences();
    const merged = mergePreferences(current, patch);
    const saved = this.connections.saveAppPreferences(merged);

    if (patch.window?.appearance !== undefined) {
      applyAppearanceToAllWindows(saved.window.appearance);
    }

    if (patch.audit?.retentionDays !== undefined && this.auditEnabledForSession) {
      this.purgeExpiredAuditLogs();
    }

    if (options?.reconfigureCloudSync !== false) {
      void this.getCloudSyncService()?.refreshFromPreferences({ triggerPull: false });
    }

    return saved;
  }

  updateAppPreferences(patch: SettingsUpdateInput): AppPreferences {
    return this.saveAppPreferencesPatch(patch);
  }

  // ---------------------------------------------------------------------------
  // Dialogs
  // ---------------------------------------------------------------------------

  async openFilesDialog(
    sender: WebContents,
    input: DialogOpenFilesInput,
  ): Promise<{ canceled: boolean; filePaths: string[] }> {
    const owner = BrowserWindow.fromWebContents(sender);
    const dialogOptions: OpenDialogOptions = {
      title: input.title ?? "选择文件",
      defaultPath: input.defaultPath ? resolveLocalPath(input.defaultPath) : undefined,
      filters: input.filters,
      properties: input.multi ? ["openFile", "multiSelections"] : ["openFile"],
      buttonLabel: "选择",
    };

    const result = owner
      ? await dialog.showOpenDialog(owner, dialogOptions)
      : await dialog.showOpenDialog(dialogOptions);

    return {
      canceled: result.canceled,
      filePaths: result.filePaths,
    };
  }

  async openDirectoryDialog(
    sender: WebContents,
    input: DialogOpenDirectoryInput,
  ): Promise<{ canceled: boolean; filePath?: string }> {
    const owner = BrowserWindow.fromWebContents(sender);
    const dialogOptions: OpenDialogOptions = {
      title: input.title ?? "选择目录",
      defaultPath: input.defaultPath ? resolveLocalPath(input.defaultPath) : undefined,
      properties: ["openDirectory", "createDirectory"],
      buttonLabel: "选择",
    };

    const result = owner
      ? await dialog.showOpenDialog(owner, dialogOptions)
      : await dialog.showOpenDialog(dialogOptions);

    return {
      canceled: result.canceled,
      filePath: result.filePaths[0],
    };
  }

  async openLocalPath(
    sender: WebContents,
    input: DialogOpenPathInput,
  ): Promise<{ ok: boolean; error?: string }> {
    const owner = BrowserWindow.fromWebContents(sender);
    const externalUrl = parseExternalUrl(input.path);

    if (externalUrl) {
      if (input.revealInFolder) {
        return { ok: false, error: "URL 不支持在文件夹中显示。" };
      }
      try {
        await shell.openExternal(externalUrl.toString());
        return { ok: true };
      } catch (error) {
        const message = error instanceof Error ? error.message : "打开链接失败";
        if (owner) {
          void dialog.showMessageBox(owner, {
            type: "error",
            title: "打开链接失败",
            message,
          });
        }
        return { ok: false, error: message };
      }
    }

    const targetPath = resolveLocalPath(input.path);
    if (!targetPath || !fs.existsSync(targetPath)) {
      if (owner) {
        void dialog.showMessageBox(owner, {
          type: "error",
          title: "打开本地文件失败",
          message: "文件不存在或路径无效。",
        });
      }
      return { ok: false, error: "文件不存在或路径无效。" };
    }

    if (input.revealInFolder) {
      shell.showItemInFolder(targetPath);
      return { ok: true };
    }

    const error = await shell.openPath(targetPath);
    return error ? { ok: false, error } : { ok: true };
  }

  // ---------------------------------------------------------------------------
  // Debug log
  // ---------------------------------------------------------------------------

  enableDebugLog(sender: WebContents): { ok: true } {
    this.debugSenders.add(sender);
    sender.once("destroyed", () => {
      this.debugSenders.delete(sender);
    });
    return { ok: true };
  }

  disableDebugLog(sender: WebContents): { ok: true } {
    this.debugSenders.delete(sender);
    return { ok: true };
  }

  emitDebugLog(entry: DebugLogEntry): void {
    if (this.debugSenders.size === 0) return;

    if (this.debugPending.length >= DEBUG_MAX_PENDING) {
      this.debugPending.shift();
    }
    this.debugPending.push(entry);

    if (!this.debugFlushTimer) {
      this.debugFlushTimer = setTimeout(() => this.flushDebugLog(), DEBUG_FLUSH_INTERVAL_MS);
    }
  }

  flushDebugLog(): void {
    this.debugFlushTimer = undefined;
    if (this.debugPending.length === 0 || this.debugSenders.size === 0) return;

    const batch = this.debugPending.splice(0);
    for (const sender of this.debugSenders) {
      if (sender.isDestroyed()) {
        this.debugSenders.delete(sender);
      } else {
        for (const entry of batch) {
          sender.send(IPCChannel.DebugLogEvent, entry);
        }
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  dispose(): void {
    if (this.debugFlushTimer) {
      clearTimeout(this.debugFlushTimer);
      this.debugFlushTimer = undefined;
    }
    this.debugPending = [];
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  private purgeExpiredAuditLogs(allowWhenDisabled = false): void {
    try {
      if (!this.auditEnabledForSession && !allowWhenDisabled) return;

      const prefs = this.connections.getAppPreferences();
      const days = prefs.audit.retentionDays;
      if (days > 0) {
        const deleted = this.connections.purgeExpiredAuditLogs(days);
        if (deleted > 0) {
          logger.info(`[Audit] purged ${deleted} expired audit log(s) (retention=${days}d)`);
        }
      }
    } catch (error) {
      logger.warn("[Audit] failed to purge expired logs", error);
    }
  }
}
