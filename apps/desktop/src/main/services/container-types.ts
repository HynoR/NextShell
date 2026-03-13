import type { WebContents } from "electron";
import type {
  AppPreferences,
  BackupArchiveMeta,
  BackupConflictPolicy,
  BatchCommandExecutionResult,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionImportEntry,
  ConnectionImportResult,
  ConnectionListQuery,
  ConnectionProfile,
  CloudSyncWorkspaceProfile,
  DeleteMode,
  BackspaceMode,
  NetworkConnection,
  ProcessDetailSnapshot,
  ProxyProfile,
  RecycleBinEntry,
  RemoteFileEntry,
  RestoreConflictPolicy,
  SavedCommand,
  SessionDescriptor,
  SessionStatus,
  SshKeyProfile,
  SystemInfoSnapshot,
  CommandExecutionResult,
  TerminalEncoding,
} from "../../../../../packages/core/src/index";
import type { SshShellChannel } from "../../../../../packages/ssh/src/index";
import type { IPty } from "node-pty";
import type {
  CommandBatchExecInput,
  ConnectionExportInput,
  ConnectionExportBatchInput,
  ConnectionExportBatchResult,
  ConnectionImportFinalShellPreviewInput,
  ConnectionImportPreviewInput,
  ConnectionImportExecuteInput,
  ConnectionUpsertInput,
  DialogOpenDirectoryInput,
  DialogOpenFilesInput,
  DialogOpenPathInput,
  PingResult,
  SavedCommandListInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  SessionOpenInput,
  SettingsUpdateInput,
  SftpEditSessionInfo,
  SshKeyUpsertInput,
  SshKeyRemoveInput,
  StreamDeliveryAckInput,
  TemplateParamsListInput,
  TemplateParamsClearInput,
  TemplateParamsUpsertInput,
  TracerouteEvent,
  UpdateCheckResult,
  ProxyUpsertInput,
  ProxyRemoveInput,
  CloudSyncWorkspaceAddInput,
  CloudSyncWorkspaceUpdateInput,
  CloudSyncWorkspaceRemoveInput,
  CloudSyncSyncNowInput,
  CloudSyncResolveConflictInput,
  ResourceCopyConnectionInput,
  ResourceDangerMoveConnectionInput,
  ResourceDeleteConnectionInput,
  ResourceDeleteSshKeyInput,
  ResourceCopySshKeyInput,
  RecycleBinRestoreInput,
  RecycleBinPurgeInput,
} from "../../../../../packages/shared/src/index";
import type { SystemMonitorController } from "./monitor/system-monitor-controller";
import type { ProcessMonitorController } from "./monitor/process-monitor-controller";
import type { NetworkMonitorController } from "./monitor/network-monitor-controller";
import type { SshConnection } from "../../../../../packages/ssh/src/index";

// ─── Active session types ──────────────────────────────────────────────────
export interface ActiveRemoteSession {
  kind: "remote";
  descriptor: SessionDescriptor;
  channel: SshShellChannel;
  sender: WebContents;
  connectionId: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
}

export interface ActiveLocalSession {
  kind: "local";
  descriptor: SessionDescriptor;
  pty: IPty;
  sender: WebContents;
  terminalEncoding: TerminalEncoding;
}

export type ActiveSession = ActiveRemoteSession | ActiveLocalSession;

// ─── Factory options ───────────────────────────────────────────────────────
export interface CreateServiceContainerOptions {
  dataDir: string;
  keytarServiceName?: string;
}

// ─── Monitor types ─────────────────────────────────────────────────────────
export interface MonitorState {
  selectedNetworkInterface?: string;
  networkInterfaceOptions?: string[];
}

export interface SystemMonitorRuntime {
  controller: SystemMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface ProcessMonitorRuntime {
  controller: ProcessMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface NetworkMonitorRuntime {
  controller: NetworkMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface AdhocSessionRuntime {
  connection: SshConnection;
  idleTimer?: ReturnType<typeof setTimeout>;
  lastUsedAt: number;
  disposed: boolean;
}

// ─── Public ServiceContainer interface ─────────────────────────────────────
export interface ServiceContainer {
  listConnections: (query: ConnectionListQuery) => ConnectionProfile[];
  upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  removeConnection: (id: string) => Promise<{ ok: true }>;
  exportConnections: (
    sender: WebContents,
    input: ConnectionExportInput
  ) => Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }>;
  exportConnectionsBatch: (input: ConnectionExportBatchInput) => Promise<ConnectionExportBatchResult>;
  revealConnectionPassword: (connectionId: string, masterPassword?: string) => Promise<{ password: string }>;
  importConnectionsPreview: (input: ConnectionImportPreviewInput) => Promise<ConnectionImportEntry[]>;
  importFinalShellConnectionsPreview: (input: ConnectionImportFinalShellPreviewInput) => Promise<ConnectionImportEntry[]>;
  importConnectionsExecute: (input: ConnectionImportExecuteInput) => Promise<ConnectionImportResult>;
  listSshKeys: () => SshKeyProfile[];
  upsertSshKey: (input: SshKeyUpsertInput) => Promise<SshKeyProfile>;
  removeSshKey: (input: SshKeyRemoveInput) => Promise<{ ok: true }>;
  listProxies: () => ProxyProfile[];
  upsertProxy: (input: ProxyUpsertInput) => Promise<ProxyProfile>;
  removeProxy: (input: ProxyRemoveInput) => Promise<{ ok: true }>;
  checkForUpdate: () => Promise<UpdateCheckResult>;
  pingHost: (host: string) => Promise<PingResult>;
  tracerouteRun: (host: string, sender: WebContents) => Promise<{ ok: true }>;
  tracerouteStop: () => { ok: true };
  getAppPreferences: () => AppPreferences;
  updateAppPreferences: (patch: SettingsUpdateInput) => AppPreferences;
  enableDebugLog: (sender: WebContents) => { ok: true };
  disableDebugLog: (sender: WebContents) => { ok: true };

  // Cloud Sync
  cloudSyncWorkspaceList: () => CloudSyncWorkspaceProfile[];
  cloudSyncWorkspaceAdd: (input: CloudSyncWorkspaceAddInput) => Promise<CloudSyncWorkspaceProfile>;
  cloudSyncWorkspaceUpdate: (input: CloudSyncWorkspaceUpdateInput) => Promise<CloudSyncWorkspaceProfile>;
  cloudSyncWorkspaceRemove: (input: CloudSyncWorkspaceRemoveInput) => Promise<void>;
  cloudSyncStatus: () => { workspaces: Array<{ workspaceId: string; state: string; lastSyncAt: string | null; lastError: string | null; pendingCount: number; conflictCount: number; currentVersion: number | null }> };
  cloudSyncSyncNow: (input: CloudSyncSyncNowInput) => Promise<void>;
  cloudSyncListConflicts: () => Array<{ workspaceId: string; workspaceName: string; resourceType: string; resourceId: string; displayName: string; serverRevision: number; conflictRemoteRevision: number; conflictRemoteDeleted: boolean; conflictDetectedAt: string }>;
  cloudSyncResolveConflict: (input: CloudSyncResolveConflictInput) => Promise<void>;
  openFilesDialog: (
    sender: WebContents,
    input: DialogOpenFilesInput
  ) => Promise<{ canceled: boolean; filePaths: string[] }>;
  openDirectoryDialog: (
    sender: WebContents,
    input: DialogOpenDirectoryInput
  ) => Promise<{ canceled: boolean; filePath?: string }>;
  openLocalPath: (
    sender: WebContents,
    input: DialogOpenPathInput
  ) => Promise<{ ok: boolean; error?: string }>;
  openSession: (
    input: SessionOpenInput,
    sender: WebContents
  ) => Promise<SessionDescriptor>;
  ackStreamDelivery: (input: StreamDeliveryAckInput) => { ok: true };
  writeSession: (sessionId: string, data: string) => { ok: true };
  resizeSession: (sessionId: string, cols: number, rows: number) => { ok: true };
  closeSession: (sessionId: string) => Promise<{ ok: true }>;
  getSystemInfoSnapshot: (connectionId: string) => Promise<SystemInfoSnapshot>;
  startSystemMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopSystemMonitor: (connectionId: string) => { ok: true };
  selectSystemNetworkInterface: (connectionId: string, networkInterface: string) => Promise<{ ok: true }>;
  execCommand: (connectionId: string, command: string) => Promise<CommandExecutionResult>;
  getSessionHomeDir: (connectionId: string) => Promise<{ path: string } | null>;
  execBatchCommand: (input: CommandBatchExecInput) => Promise<BatchCommandExecutionResult>;
  listAuditLogs: (limit: number) => import("../../../../../packages/core/src/index").AuditLogRecord[];
  clearAuditLogs: () => { ok: true; deleted: number };
  listMigrations: () => import("../../../../../packages/core/src/index").MigrationRecord[];
  listRemoteFiles: (connectionId: string, path: string) => Promise<RemoteFileEntry[]>;
  listLocalFiles: (path: string) => Promise<RemoteFileEntry[]>;
  uploadRemoteFile: (
    connectionId: string,
    localPath: string,
    remotePath: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  downloadRemoteFile: (
    connectionId: string,
    remotePath: string,
    localPath: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  uploadRemotePacked: (
    connectionId: string,
    localPaths: string[],
    remoteDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  downloadRemotePacked: (
    connectionId: string,
    remoteDir: string,
    entryNames: string[],
    localDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true; localArchivePath: string }>;
  transferRemotePacked: (
    sourceConnectionId: string,
    sourceDir: string,
    entryNames: string[],
    targetConnectionId: string,
    targetDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  createRemoteDirectory: (connectionId: string, pathName: string) => Promise<{ ok: true }>;
  renameRemoteFile: (connectionId: string, fromPath: string, toPath: string) => Promise<{ ok: true }>;
  deleteRemoteFile: (
    connectionId: string,
    targetPath: string,
    type: RemoteFileEntry["type"]
  ) => Promise<{ ok: true }>;
  listCommandHistory: () => CommandHistoryEntry[];
  pushCommandHistory: (command: string) => CommandHistoryEntry;
  removeCommandHistory: (command: string) => { ok: true };
  clearCommandHistory: () => { ok: true };
  listSavedCommands: (query?: SavedCommandListInput) => SavedCommand[];
  upsertSavedCommand: (input: SavedCommandUpsertInput) => SavedCommand;
  removeSavedCommand: (input: SavedCommandRemoveInput) => { ok: true };
  openRemoteEdit: (
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ) => Promise<{ editId: string; localPath: string }>;
  stopRemoteEdit: (editId: string) => Promise<{ ok: true }>;
  stopAllRemoteEdits: () => Promise<{ ok: true }>;
  listRemoteEdits: () => SftpEditSessionInfo[];
  openBuiltinEdit: (connectionId: string, remotePath: string, sender: WebContents) => Promise<{ editId: string; content: string }>;
  saveBuiltinEdit: (editId: string, connectionId: string, remotePath: string, content: string) => Promise<{ ok: true }>;
  startProcessMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopProcessMonitor: (connectionId: string) => { ok: true };
  getProcessDetail: (connectionId: string, pid: number) => Promise<ProcessDetailSnapshot>;
  killRemoteProcess: (connectionId: string, pid: number, signal: "SIGTERM" | "SIGKILL") => Promise<{ ok: true }>;
  startNetworkMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopNetworkMonitor: (connectionId: string) => { ok: true };
  getNetworkConnections: (connectionId: string, port: number) => Promise<NetworkConnection[]>;
  backupList: () => Promise<BackupArchiveMeta[]>;
  backupRun: (conflictPolicy: BackupConflictPolicy) => Promise<{ ok: true; fileName?: string }>;
  backupRestore: (archiveId: string, conflictPolicy: RestoreConflictPolicy) => Promise<{ ok: true }>;
  masterPasswordSet: (password: string) => Promise<{ ok: true }>;
  masterPasswordUnlock: (password: string) => Promise<{ ok: true }>;
  masterPasswordChange: (oldPassword: string, newPassword: string) => Promise<{ ok: true }>;
  masterPasswordClearRemembered: () => Promise<{ ok: true }>;
  masterPasswordStatus: () => Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }>;
  masterPasswordGetCached: () => Promise<{ password?: string }>;
  backupSetPassword: (password: string) => Promise<{ ok: true }>;
  backupUnlockPassword: (password: string) => Promise<{ ok: true }>;
  backupClearRemembered: () => Promise<{ ok: true }>;
  backupPasswordStatus: () => Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }>;
  listTemplateParams: (input?: TemplateParamsListInput) => CommandTemplateParam[];
  upsertTemplateParams: (input: TemplateParamsUpsertInput) => { ok: true };
  clearTemplateParams: (input: TemplateParamsClearInput) => { ok: true };

  // Resource Operations
  resourceCopyConnection: (input: ResourceCopyConnectionInput) => Promise<ConnectionProfile>;
  resourceDangerMoveConnection: (input: ResourceDangerMoveConnectionInput) => Promise<ConnectionProfile>;
  resourceDeleteConnection: (input: ResourceDeleteConnectionInput) => Promise<void>;
  resourceDeleteSshKey: (input: ResourceDeleteSshKeyInput) => Promise<void>;
  resourceCopySshKey: (input: ResourceCopySshKeyInput) => Promise<SshKeyProfile>;

  // Recycle Bin
  recycleBinList: () => RecycleBinEntry[];
  recycleBinRestore: (input: RecycleBinRestoreInput) => Promise<ConnectionProfile | SshKeyProfile>;
  recycleBinPurge: (input: RecycleBinPurgeInput) => void;
  recycleBinClear: () => void;

  dispose: () => Promise<void>;
}
