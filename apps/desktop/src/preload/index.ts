import { contextBridge, ipcRenderer } from "electron";
import type {
  DebugLogEntry,
  SessionDataEvent,
  SessionStatusEvent,
  SftpEditStatusEvent,
  SftpTransferStatusEvent
} from "../../../../packages/shared/src/index";
import type {
  MonitorSnapshot,
  ProcessSnapshot,
  NetworkSnapshot
} from "../../../../packages/core/src/index";
import {
  IPCChannel,
  type NextShellApi
} from "../../../../packages/shared/src/index";
import { WINDOWS_TITLEBAR_SAFE_TOP } from "../shared/window-ui";

const masterPasswordApi: NextShellApi["masterPassword"] = {
  setPassword: (payload) => ipcRenderer.invoke(IPCChannel.MasterPasswordSet, payload),
  unlockPassword: (payload) => ipcRenderer.invoke(IPCChannel.MasterPasswordUnlock, payload),
  clearRemembered: () => ipcRenderer.invoke(IPCChannel.MasterPasswordClearRemembered, {}),
  passwordStatus: () => ipcRenderer.invoke(IPCChannel.MasterPasswordStatus, {}),
  getCached: () => ipcRenderer.invoke(IPCChannel.MasterPasswordGetCached, {})
};

const api: NextShellApi = {
  connection: {
    list: (query) => ipcRenderer.invoke(IPCChannel.ConnectionList, query),
    upsert: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionUpsert, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionRemove, payload),
    exportToFile: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionExport, payload),
    exportBatch: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionExportBatch, payload),
    revealPassword: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionRevealPassword, payload),
    importPreview: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionImportPreview, payload),
    importFinalShellPreview: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionImportFinalShellPreview, payload),
    importExecute: (payload) => ipcRenderer.invoke(IPCChannel.ConnectionImportExecute, payload)
  },
  session: {
    open: (payload) => ipcRenderer.invoke(IPCChannel.SessionOpen, payload),
    write: (payload) => ipcRenderer.invoke(IPCChannel.SessionWrite, payload),
    resize: (payload) => ipcRenderer.invoke(IPCChannel.SessionResize, payload),
    close: (payload) => ipcRenderer.invoke(IPCChannel.SessionClose, payload),
    getCwd: (payload) => ipcRenderer.invoke(IPCChannel.SessionGetCwd, payload),
    onData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: SessionDataEvent) => {
        listener(payload);
      };

      ipcRenderer.on(IPCChannel.SessionData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.SessionData, handler);
      };
    },
    onStatus: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: SessionStatusEvent) => {
        listener(payload);
      };

      ipcRenderer.on(IPCChannel.SessionStatus, handler);
      return () => {
        ipcRenderer.off(IPCChannel.SessionStatus, handler);
      };
    }
  },
  monitor: {
    snapshot: (payload) => ipcRenderer.invoke(IPCChannel.MonitorSnapshot, payload),
    getSystemInfoSnapshot: (payload) => ipcRenderer.invoke(IPCChannel.MonitorSystemInfoSnapshot, payload),
    startSystem: (payload) => ipcRenderer.invoke(IPCChannel.MonitorSystemStart, payload),
    stopSystem: (payload) => ipcRenderer.invoke(IPCChannel.MonitorSystemStop, payload),
    selectSystemInterface: (payload) => ipcRenderer.invoke(IPCChannel.MonitorSystemSelectInterface, payload),
    onSystemData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: MonitorSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorSystemData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorSystemData, handler);
      };
    },
    startProcess: (payload) => ipcRenderer.invoke(IPCChannel.MonitorProcessStart, payload),
    stopProcess: (payload) => ipcRenderer.invoke(IPCChannel.MonitorProcessStop, payload),
    onProcessData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: ProcessSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorProcessData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorProcessData, handler);
      };
    },
    getProcessDetail: (payload) => ipcRenderer.invoke(IPCChannel.MonitorProcessDetail, payload),
    killProcess: (payload) => ipcRenderer.invoke(IPCChannel.MonitorProcessKill, payload),
    startNetwork: (payload) => ipcRenderer.invoke(IPCChannel.MonitorNetworkStart, payload),
    stopNetwork: (payload) => ipcRenderer.invoke(IPCChannel.MonitorNetworkStop, payload),
    onNetworkData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: NetworkSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorNetworkData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorNetworkData, handler);
      };
    },
    getNetworkConnections: (payload) => ipcRenderer.invoke(IPCChannel.MonitorNetworkConnections, payload)
  },
  command: {
    exec: (payload) => ipcRenderer.invoke(IPCChannel.CommandExec, payload),
    execBatch: (payload) => ipcRenderer.invoke(IPCChannel.CommandBatchExec, payload)
  },
  audit: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.AuditList, payload)
  },
  storage: {
    migrations: (payload) => ipcRenderer.invoke(IPCChannel.StorageMigrations, payload ?? {})
  },
  settings: {
    get: () => ipcRenderer.invoke(IPCChannel.SettingsGet, {}),
    update: (payload) => ipcRenderer.invoke(IPCChannel.SettingsUpdate, payload)
  },
  dialog: {
    openFiles: (payload) => ipcRenderer.invoke(IPCChannel.DialogOpenFiles, payload ?? {}),
    openDirectory: (payload) => ipcRenderer.invoke(IPCChannel.DialogOpenDirectory, payload ?? {}),
    openPath: (payload) => ipcRenderer.invoke(IPCChannel.DialogOpenPath, payload)
  },
  sftp: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.SftpList, payload),
    upload: (payload) => ipcRenderer.invoke(IPCChannel.SftpUpload, payload),
    download: (payload) => ipcRenderer.invoke(IPCChannel.SftpDownload, payload),
    mkdir: (payload) => ipcRenderer.invoke(IPCChannel.SftpMkdir, payload),
    rename: (payload) => ipcRenderer.invoke(IPCChannel.SftpRename, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.SftpDelete, payload),
    editOpen: (payload) => ipcRenderer.invoke(IPCChannel.SftpEditOpen, payload),
    editOpenBuiltin: (payload) => ipcRenderer.invoke(IPCChannel.SftpEditOpenBuiltin, payload),
    editSaveBuiltin: (payload) => ipcRenderer.invoke(IPCChannel.SftpEditSaveBuiltin, payload),
    editStop: (payload) => ipcRenderer.invoke(IPCChannel.SftpEditStop, payload),
    editStopAll: () => ipcRenderer.invoke(IPCChannel.SftpEditStopAll, {}),
    editList: () => ipcRenderer.invoke(IPCChannel.SftpEditList, {}),
    onEditStatus: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: SftpEditStatusEvent) => {
        listener(payload);
      };

      ipcRenderer.on(IPCChannel.SftpEditStatus, handler);
      return () => {
        ipcRenderer.off(IPCChannel.SftpEditStatus, handler);
      };
    },
    onTransferStatus: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: SftpTransferStatusEvent) => {
        listener(payload);
      };

      ipcRenderer.on(IPCChannel.SftpTransferStatus, handler);
      return () => {
        ipcRenderer.off(IPCChannel.SftpTransferStatus, handler);
      };
    }
  },
  commandHistory: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.CommandHistoryList, payload ?? {}),
    push: (payload) => ipcRenderer.invoke(IPCChannel.CommandHistoryPush, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.CommandHistoryRemove, payload),
    clear: (payload) => ipcRenderer.invoke(IPCChannel.CommandHistoryClear, payload ?? {})
  },
  savedCommand: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.SavedCommandList, payload ?? {}),
    upsert: (payload) => ipcRenderer.invoke(IPCChannel.SavedCommandUpsert, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.SavedCommandRemove, payload)
  },
  backup: {
    list: () => ipcRenderer.invoke(IPCChannel.BackupList, {}),
    run: (payload) => ipcRenderer.invoke(IPCChannel.BackupRun, payload ?? {}),
    restore: (payload) => ipcRenderer.invoke(IPCChannel.BackupRestore, payload),
    setPassword: (payload) => masterPasswordApi.setPassword(payload),
    unlockPassword: (payload) => masterPasswordApi.unlockPassword(payload),
    clearRemembered: () => masterPasswordApi.clearRemembered(),
    passwordStatus: () => masterPasswordApi.passwordStatus()
  },
  masterPassword: masterPasswordApi,
  templateParams: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.TemplateParamsList, payload ?? {}),
    upsert: (payload) => ipcRenderer.invoke(IPCChannel.TemplateParamsUpsert, payload),
    clear: (payload) => ipcRenderer.invoke(IPCChannel.TemplateParamsClear, payload)
  },
  sshKey: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.SshKeyList, payload ?? {}),
    upsert: (payload) => ipcRenderer.invoke(IPCChannel.SshKeyUpsert, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.SshKeyRemove, payload)
  },
  proxy: {
    list: (payload) => ipcRenderer.invoke(IPCChannel.ProxyList, payload ?? {}),
    upsert: (payload) => ipcRenderer.invoke(IPCChannel.ProxyUpsert, payload),
    remove: (payload) => ipcRenderer.invoke(IPCChannel.ProxyRemove, payload)
  },
  about: {
    checkUpdate: () => ipcRenderer.invoke(IPCChannel.UpdateCheck, {})
  },
  ping: {
    probe: (payload: { host: string }) => ipcRenderer.invoke(IPCChannel.Ping, payload)
  },
  debug: {
    enableLog: () => ipcRenderer.invoke(IPCChannel.DebugLogEnable, {}),
    disableLog: () => ipcRenderer.invoke(IPCChannel.DebugLogDisable, {}),
    onLogEvent: (listener: (entry: DebugLogEntry) => void) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: DebugLogEntry) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.DebugLogEvent, handler);
      return () => {
        ipcRenderer.off(IPCChannel.DebugLogEvent, handler);
      };
    }
  },
  platform: process.platform,
  ui: {
    titlebarSafeTop: WINDOWS_TITLEBAR_SAFE_TOP
  }
};

contextBridge.exposeInMainWorld("nextshell", api);
