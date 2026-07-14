import { contextBridge, ipcRenderer, webUtils } from "electron";
import type {
  CloudSyncManagerStatusEvent,
  DebugLogEntry,
  SessionDataEvent,
  SessionStatusEvent,
  SftpEditStatusEvent,
  SftpTransferStatusEvent,
  StreamDeliveryAckInput,
  TracerouteEvent
} from "../../../../packages/shared/src/index";
import type {
  MonitorSnapshot,
  ProcessSnapshot,
  NetworkSnapshot
} from "../../../../packages/core/src/index";
import {
  IPCChannel,
  type IpcInvokeChannel,
  type IpcInvokePayload,
  type IpcInvokeResult,
  type NextShellApi
} from "../../../../packages/shared/src/index";
import { WINDOWS_TITLEBAR_SAFE_TOP } from "../shared/window-ui";

const invoke = <C extends IpcInvokeChannel>(
  channel: C,
  payload: IpcInvokePayload<C>
): Promise<IpcInvokeResult<C>> => ipcRenderer.invoke(channel, payload);

const masterPasswordApi: NextShellApi["masterPassword"] = {
  setPassword: (payload) => invoke(IPCChannel.MasterPasswordSet, payload),
  unlockPassword: (payload) => invoke(IPCChannel.MasterPasswordUnlock, payload),
  changePassword: (payload) => invoke(IPCChannel.MasterPasswordChange, payload),
  clearRemembered: () => invoke(IPCChannel.MasterPasswordClearRemembered, {}),
  passwordStatus: () => invoke(IPCChannel.MasterPasswordStatus, {}),
  getCached: () => invoke(IPCChannel.MasterPasswordGetCached, {})
};

const ackStreamDelivery = (payload: StreamDeliveryAckInput): Promise<{ ok: true }> => {
  return invoke(IPCChannel.StreamDeliveryAck, payload);
};

const api: NextShellApi = {
  getFilePathForDrop: (file: File): string => {
    return webUtils.getPathForFile(file);
  },
  connection: {
    list: (query) => invoke(IPCChannel.ConnectionList, query),
    upsert: (payload) => invoke(IPCChannel.ConnectionUpsert, payload),
    batchUpdateAuth: (payload) => invoke(IPCChannel.ConnectionBatchAuthUpdate, payload),
    remove: (payload) => invoke(IPCChannel.ConnectionRemove, payload),
    exportToFile: (payload) => invoke(IPCChannel.ConnectionExport, payload),
    exportBatch: (payload) => invoke(IPCChannel.ConnectionExportBatch, payload),
    revealPassword: (payload) => invoke(IPCChannel.ConnectionRevealPassword, payload),
    importPreview: (payload) => invoke(IPCChannel.ConnectionImportPreview, payload),
    importFinalShellPreview: (payload) => invoke(IPCChannel.ConnectionImportFinalShellPreview, payload),
    importDirectoryPreview: (payload) => invoke(IPCChannel.ConnectionImportDirectoryPreview, payload),
    importExecute: (payload) => invoke(IPCChannel.ConnectionImportExecute, payload)
  },
  session: {
    open: (payload) => invoke(IPCChannel.SessionOpen, payload),
    write: (payload) => invoke(IPCChannel.SessionWrite, payload),
    resize: (payload) => invoke(IPCChannel.SessionResize, payload),
    close: (payload) => invoke(IPCChannel.SessionClose, payload),
    getHomeDir: (payload) => invoke(IPCChannel.SessionGetHomeDir, payload),
    ackData: (payload) => ackStreamDelivery(payload),
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
    getSystemInfoSnapshot: (payload) => invoke(IPCChannel.MonitorSystemInfoSnapshot, payload),
    startSystem: (payload) => invoke(IPCChannel.MonitorSystemStart, payload),
    stopSystem: (payload) => invoke(IPCChannel.MonitorSystemStop, payload),
    selectSystemInterface: (payload) => invoke(IPCChannel.MonitorSystemSelectInterface, payload),
    onSystemData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: MonitorSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorSystemData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorSystemData, handler);
      };
    },
    startProcess: (payload) => invoke(IPCChannel.MonitorProcessStart, payload),
    stopProcess: (payload) => invoke(IPCChannel.MonitorProcessStop, payload),
    onProcessData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: ProcessSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorProcessData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorProcessData, handler);
      };
    },
    getProcessDetail: (payload) => invoke(IPCChannel.MonitorProcessDetail, payload),
    killProcess: (payload) => invoke(IPCChannel.MonitorProcessKill, payload),
    startNetwork: (payload) => invoke(IPCChannel.MonitorNetworkStart, payload),
    stopNetwork: (payload) => invoke(IPCChannel.MonitorNetworkStop, payload),
    onNetworkData: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: NetworkSnapshot) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.MonitorNetworkData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.MonitorNetworkData, handler);
      };
    },
    getNetworkConnections: (payload) => invoke(IPCChannel.MonitorNetworkConnections, payload)
  },
  command: {
    exec: (payload) => invoke(IPCChannel.CommandExec, payload),
    execBatch: (payload) => invoke(IPCChannel.CommandBatchExec, payload)
  },
  audit: {
    clear: (payload) => invoke(IPCChannel.AuditClear, payload ?? {})
  },
  settings: {
    get: () => invoke(IPCChannel.SettingsGet, {}),
    update: (payload) => invoke(IPCChannel.SettingsUpdate, payload)
  },
  dialog: {
    openFiles: (payload) => invoke(IPCChannel.DialogOpenFiles, payload ?? {}),
    openDirectory: (payload) => invoke(IPCChannel.DialogOpenDirectory, payload ?? {}),
    openPath: (payload) => invoke(IPCChannel.DialogOpenPath, payload)
  },
  sftp: {
    list: (payload) => invoke(IPCChannel.SftpList, payload),
    listLocal: (payload) => invoke(IPCChannel.SftpListLocal, payload),
    upload: (payload) => invoke(IPCChannel.SftpUpload, payload),
    download: (payload) => invoke(IPCChannel.SftpDownload, payload),
    uploadPacked: (payload) => invoke(IPCChannel.SftpUploadPacked, payload),
    downloadPacked: (payload) => invoke(IPCChannel.SftpDownloadPacked, payload),
    transferPacked: (payload) => invoke(IPCChannel.SftpTransferPacked, payload),
    cancelTransfer: (payload) => invoke(IPCChannel.SftpTransferCancel, payload),
    mkdir: (payload) => invoke(IPCChannel.SftpMkdir, payload),
    rename: (payload) => invoke(IPCChannel.SftpRename, payload),
    remove: (payload) => invoke(IPCChannel.SftpDelete, payload),
    editOpen: (payload) => invoke(IPCChannel.SftpEditOpen, payload),
    editOpenBuiltin: (payload) => invoke(IPCChannel.SftpEditOpenBuiltin, payload),
    editSaveBuiltin: (payload) => invoke(IPCChannel.SftpEditSaveBuiltin, payload),
    editStop: (payload) => invoke(IPCChannel.SftpEditStop, payload),
    editStopAll: () => invoke(IPCChannel.SftpEditStopAll, {}),
    editList: () => invoke(IPCChannel.SftpEditList, {}),
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
    list: (payload) => invoke(IPCChannel.CommandHistoryList, payload ?? {}),
    push: (payload) => invoke(IPCChannel.CommandHistoryPush, payload),
    remove: (payload) => invoke(IPCChannel.CommandHistoryRemove, payload),
    clear: (payload) => invoke(IPCChannel.CommandHistoryClear, payload ?? {})
  },
  savedCommand: {
    listScoped: () => invoke(IPCChannel.SavedCommandListScoped, {}),
    upsert: (payload) => invoke(IPCChannel.SavedCommandUpsert, payload),
    remove: (payload) => invoke(IPCChannel.SavedCommandRemove, payload)
  },
  backup: {
    list: () => invoke(IPCChannel.BackupList, {}),
    run: (payload) => invoke(IPCChannel.BackupRun, payload ?? {}),
    restore: (payload) => invoke(IPCChannel.BackupRestore, payload)
  },
  cloudSync: {
    workspaceList: () => invoke(IPCChannel.CloudSyncWorkspaceList, {}),
    workspaceAdd: (payload) => invoke(IPCChannel.CloudSyncWorkspaceAdd, payload),
    workspaceUpdate: (payload) => invoke(IPCChannel.CloudSyncWorkspaceUpdate, payload),
    workspaceRemove: (payload) => invoke(IPCChannel.CloudSyncWorkspaceRemove, payload),
    workspaceExportToken: (payload) => invoke(IPCChannel.CloudSyncWorkspaceExportToken, payload),
    workspaceParseToken: (payload) => invoke(IPCChannel.CloudSyncWorkspaceParseToken, payload),
    status: () => invoke(IPCChannel.CloudSyncStatus, {}),
    syncNow: (payload) => invoke(IPCChannel.CloudSyncSyncNow, payload ?? {}),
    listConflicts: () => invoke(IPCChannel.CloudSyncListConflicts, {}),
    testConnection: (payload) => invoke(IPCChannel.CloudSyncTestConnection, payload),
    resolveConflict: (payload) => invoke(IPCChannel.CloudSyncResolveConflict, payload),
    onStatus: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: CloudSyncManagerStatusEvent) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.CloudSyncStatusEvent, handler);
      return () => { ipcRenderer.off(IPCChannel.CloudSyncStatusEvent, handler); };
    },
    onApplied: (listener) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: { workspaceId: string }) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.CloudSyncAppliedEvent, handler);
      return () => { ipcRenderer.off(IPCChannel.CloudSyncAppliedEvent, handler); };
    }
  },
  masterPassword: masterPasswordApi,
  sshKey: {
    list: (payload) => invoke(IPCChannel.SshKeyList, payload ?? {}),
    upsert: (payload) => invoke(IPCChannel.SshKeyUpsert, payload),
    remove: (payload) => invoke(IPCChannel.SshKeyRemove, payload)
  },
  proxy: {
    list: (payload) => invoke(IPCChannel.ProxyList, payload ?? {}),
    upsert: (payload) => invoke(IPCChannel.ProxyUpsert, payload),
    remove: (payload) => invoke(IPCChannel.ProxyRemove, payload)
  },
  about: {
    checkUpdate: () => invoke(IPCChannel.UpdateCheck, {})
  },
  ping: {
    probe: (payload: { host: string }) => invoke(IPCChannel.Ping, payload)
  },
  traceroute: {
    run: (payload: { host: string }) => invoke(IPCChannel.TracerouteRun, payload),
    stop: () => invoke(IPCChannel.TracerouteStop, {}),
    onData: (listener: (event: TracerouteEvent) => void) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: TracerouteEvent) => {
        listener(payload);
      };
      ipcRenderer.on(IPCChannel.TracerouteData, handler);
      return () => {
        ipcRenderer.off(IPCChannel.TracerouteData, handler);
      };
    }
  },
  debug: {
    enableLog: () => invoke(IPCChannel.DebugLogEnable, {}),
    disableLog: () => invoke(IPCChannel.DebugLogDisable, {}),
    onLogEvent: (listener: (entry: DebugLogEntry) => void) => {
      const handler = (_event: Electron.IpcRendererEvent, payload: DebugLogEntry[]) => {
        for (const entry of payload) {
          listener(entry);
        }
      };
      ipcRenderer.on(IPCChannel.DebugLogEvent, handler);
      return () => {
        ipcRenderer.off(IPCChannel.DebugLogEvent, handler);
      };
    }
  },
  resourceOps: {
    copyConnection: (payload) => invoke(IPCChannel.ResourceCopyConnection, payload)
  },
  recycleBin: {
    list: () => invoke(IPCChannel.RecycleBinList, {}),
    restore: (payload) => invoke(IPCChannel.RecycleBinRestore, payload),
    purge: (payload) => invoke(IPCChannel.RecycleBinPurge, payload),
    clear: () => invoke(IPCChannel.RecycleBinClear, {})
  },
  platform: process.platform,
  ui: {
    titlebarSafeTop: WINDOWS_TITLEBAR_SAFE_TOP
  }
};

contextBridge.exposeInMainWorld("nextshell", api);
