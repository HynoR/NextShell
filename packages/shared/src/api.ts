import type {
  BackupArchiveMeta,
  BatchCommandExecutionResult,
  CommandExecutionResult,
  CommandHistoryEntry,
  ConnectionImportEntry,
  ConnectionImportResult,
  ConnectionListQuery,
  ConnectionProfile,
  CloudSyncWorkspaceProfile,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  ProxyProfile,
  RecycleBinEntry,
  RemoteFileEntry,
  ScopedCommandItem,
  SavedCommand,
  SessionDescriptor,
  SystemInfoSnapshot,
  SshKeyProfile,
  WorkspaceRepoConflict,
  WorkspaceRepoStatus
} from "../../core/src/index";
import type {
  AppPreferences,
  AppPreferencesPatchInput,
  AuditClearInput,
  DebugLogEntry,
  BackupListInput,
  BackupRestoreInput,
  BackupRunInput,
  CommandBatchExecInput,
  CommandExecInput,
  ConnectionBatchAuthUpdateInput,
  ConnectionBatchAuthUpdateResult,
  CommandHistoryClearInput,
  CommandHistoryListInput,
  CommandHistoryPushInput,
  CommandHistoryRemoveInput,
  ConnectionExportInput,
  ConnectionExportBatchInput,
  ConnectionExportBatchResult,
  ConnectionRevealPasswordInput,
  ConnectionRevealPasswordResult,
  ConnectionImportExecuteInput,
  ConnectionImportDirectoryPreviewInput,
  ConnectionImportDirectoryPreviewResult,
  ConnectionImportFinalShellPreviewInput,
  ConnectionImportPreviewInput,
  MasterPasswordCachedResult,
  MasterPasswordChangeInput,
  MasterPasswordClearRememberedInput,
  MasterPasswordGetCachedInput,
  MasterPasswordSetInput,
  MasterPasswordStatusInput,
  MasterPasswordStatusResult,
  MasterPasswordUnlockInput,
  ConnectionRemoveInput,
  ConnectionUpsertInput,
  DialogOpenDirectoryInput,
  DialogOpenFilesInput,
  DialogOpenPathInput,
  MonitorNetworkConnectionsInput,
  MonitorNetworkStartInput,
  MonitorNetworkStopInput,
  MonitorProcessKillInput,
  MonitorProcessDetailInput,
  MonitorProcessStartInput,
  MonitorProcessStopInput,
  MonitorSystemInfoSnapshotInput,
  MonitorSystemStartInput,
  MonitorSystemSelectInterfaceInput,
  MonitorSystemStopInput,
  ProxyListInput,
  ProxyRemoveInput,
  ProxyUpsertInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  SessionCloseInput,
  SessionDataEvent,
  SessionGetHomeDirInput,
  SessionOpenInput,
  SessionResizeInput,
  SessionStatusEvent,
  StreamDeliveryAckInput,
  SessionWriteInput,
  SftpDeleteInput,
  SftpDownloadInput,
  SftpDownloadPackedInput,
  SftpEditOpenInput,
  SftpEditOpenBuiltinInput,
  SftpEditSaveBuiltinInput,
  SftpEditStatusEvent,
  SftpEditStopInput,
  SftpEditSessionInfo,
  SftpListInput,
  SftpListLocalInput,
  SftpMkdirInput,
  SftpRenameInput,
  SftpTransferCancelInput,
  SftpTransferPackedInput,
  SftpTransferStatusEvent,
  SftpUploadInput,
  SftpUploadPackedInput,
  SshKeyListInput,
  SshKeyRemoveInput,
  SshKeyUpsertInput,
  TracerouteRunInput,
  TracerouteEvent,
  UpdateCheckResult,
  PingRequestInput,
  PingResult,
  CloudSyncWorkspaceAddInput,
  CloudSyncWorkspaceUpdateInput,
  CloudSyncWorkspaceRemoveInput,
  CloudSyncWorkspaceTokenDraft,
  CloudSyncWorkspaceExportTokenInput,
  CloudSyncWorkspaceParseTokenInput,
  CloudSyncTestConnectionInput,
  CloudSyncSyncNowInput,
  CloudSyncResolveConflictInput,
  ResourceCopyConnectionInput,
  RecycleBinRestoreInput,
  RecycleBinPurgeInput
} from "./contracts";

export type SessionEventUnsubscribe = () => void;

export type CloudSyncRuntimeState = WorkspaceRepoStatus["state"];

export type CloudSyncRuntimeStatusEvent = WorkspaceRepoStatus;

export interface CloudSyncManagerStatusEvent {
  workspaces: CloudSyncRuntimeStatusEvent[];
}

export interface NextShellApi {
  /** Resolve the native file-system path for a File obtained from a drag-and-drop.
   *  Uses Electron's `webUtils.getPathForFile` in the preload — required under sandbox mode
   *  where `File.path` is always empty. */
  getFilePathForDrop: (file: File) => string;
  /** Current OS platform, set synchronously from process.platform in the preload. */
  platform: string;
  /** UI layout constants published by preload for renderer-safe spacing. */
  ui: {
    /** Safe top inset for native title bar overlay interactions on Windows. */
    titlebarSafeTop: number;
  };
  connection: {
    list: (query: ConnectionListQuery) => Promise<ConnectionProfile[]>;
    upsert: (payload: ConnectionUpsertInput) => Promise<ConnectionProfile>;
    batchUpdateAuth: (payload: ConnectionBatchAuthUpdateInput) => Promise<ConnectionBatchAuthUpdateResult>;
    remove: (payload: ConnectionRemoveInput) => Promise<{ ok: true }>;
    exportToFile: (payload: ConnectionExportInput) => Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }>;
    exportBatch: (payload: ConnectionExportBatchInput) => Promise<ConnectionExportBatchResult>;
    revealPassword: (payload: ConnectionRevealPasswordInput) => Promise<ConnectionRevealPasswordResult>;
    importPreview: (payload: ConnectionImportPreviewInput) => Promise<ConnectionImportEntry[]>;
    importFinalShellPreview: (payload: ConnectionImportFinalShellPreviewInput) => Promise<ConnectionImportEntry[]>;
    importDirectoryPreview: (payload: ConnectionImportDirectoryPreviewInput) => Promise<ConnectionImportDirectoryPreviewResult>;
    importExecute: (payload: ConnectionImportExecuteInput) => Promise<ConnectionImportResult>;
  };
  session: {
    open: (payload: SessionOpenInput) => Promise<SessionDescriptor>;
    write: (payload: SessionWriteInput) => Promise<{ ok: true }>;
    resize: (payload: SessionResizeInput) => Promise<{ ok: true }>;
    close: (payload: SessionCloseInput) => Promise<{ ok: true }>;
    getHomeDir: (payload: SessionGetHomeDirInput) => Promise<{ path: string } | null>;
    ackData: (payload: StreamDeliveryAckInput) => Promise<{ ok: true }>;
    onData: (listener: (event: SessionDataEvent) => void) => SessionEventUnsubscribe;
    onStatus: (listener: (event: SessionStatusEvent) => void) => SessionEventUnsubscribe;
  };
  monitor: {
    getSystemInfoSnapshot: (payload: MonitorSystemInfoSnapshotInput) => Promise<SystemInfoSnapshot>;
    startSystem: (payload: MonitorSystemStartInput) => Promise<{ ok: true }>;
    stopSystem: (payload: MonitorSystemStopInput) => Promise<{ ok: true }>;
    selectSystemInterface: (payload: MonitorSystemSelectInterfaceInput) => Promise<{ ok: true }>;
    onSystemData: (listener: (event: MonitorSnapshot) => void) => SessionEventUnsubscribe;
    startProcess: (payload: MonitorProcessStartInput) => Promise<{ ok: true }>;
    stopProcess: (payload: MonitorProcessStopInput) => Promise<{ ok: true }>;
    onProcessData: (listener: (event: ProcessSnapshot) => void) => SessionEventUnsubscribe;
    getProcessDetail: (payload: MonitorProcessDetailInput) => Promise<ProcessDetailSnapshot>;
    killProcess: (payload: MonitorProcessKillInput) => Promise<{ ok: true }>;
    startNetwork: (payload: MonitorNetworkStartInput) => Promise<{ ok: true }>;
    stopNetwork: (payload: MonitorNetworkStopInput) => Promise<{ ok: true }>;
    onNetworkData: (listener: (event: NetworkSnapshot) => void) => SessionEventUnsubscribe;
    getNetworkConnections: (payload: MonitorNetworkConnectionsInput) => Promise<NetworkConnection[]>;
  };
  command: {
    exec: (payload: CommandExecInput) => Promise<CommandExecutionResult>;
    execBatch: (payload: CommandBatchExecInput) => Promise<BatchCommandExecutionResult>;
  };
  audit: {
    clear: (payload?: AuditClearInput) => Promise<{ ok: true; deleted: number }>;
  };
  settings: {
    get: () => Promise<AppPreferences>;
    update: (payload: AppPreferencesPatchInput) => Promise<AppPreferences>;
  };
  dialog: {
    openFiles: (payload?: DialogOpenFilesInput) => Promise<{ canceled: boolean; filePaths: string[] }>;
    openDirectory: (payload?: DialogOpenDirectoryInput) => Promise<{ canceled: boolean; filePath?: string }>;
    openPath: (payload: DialogOpenPathInput) => Promise<{ ok: boolean; error?: string }>;
  };
  sftp: {
    list: (payload: SftpListInput) => Promise<RemoteFileEntry[]>;
    listLocal: (payload: SftpListLocalInput) => Promise<RemoteFileEntry[]>;
    upload: (payload: SftpUploadInput) => Promise<{ ok: true }>;
    download: (payload: SftpDownloadInput) => Promise<{ ok: true }>;
    uploadPacked: (payload: SftpUploadPackedInput) => Promise<{ ok: true }>;
    downloadPacked: (payload: SftpDownloadPackedInput) => Promise<{ ok: true; localArchivePath: string }>;
    transferPacked: (payload: SftpTransferPackedInput) => Promise<{ ok: true }>;
    cancelTransfer: (payload: SftpTransferCancelInput) => Promise<{ ok: true; cancelled: boolean }>;
    mkdir: (payload: SftpMkdirInput) => Promise<{ ok: true }>;
    rename: (payload: SftpRenameInput) => Promise<{ ok: true }>;
    remove: (payload: SftpDeleteInput) => Promise<{ ok: true }>;
    editOpen: (payload: SftpEditOpenInput) => Promise<{ editId: string; localPath: string }>;
    editOpenBuiltin: (payload: SftpEditOpenBuiltinInput) => Promise<{ editId: string; content: string }>;
    editSaveBuiltin: (payload: SftpEditSaveBuiltinInput) => Promise<{ ok: true }>;
    editStop: (payload: SftpEditStopInput) => Promise<{ ok: true }>;
    editStopAll: () => Promise<{ ok: true }>;
    editList: () => Promise<SftpEditSessionInfo[]>;
    onEditStatus: (listener: (event: SftpEditStatusEvent) => void) => SessionEventUnsubscribe;
    onTransferStatus: (listener: (event: SftpTransferStatusEvent) => void) => SessionEventUnsubscribe;
  };
  commandHistory: {
    list: (payload?: CommandHistoryListInput) => Promise<CommandHistoryEntry[]>;
    push: (payload: CommandHistoryPushInput) => Promise<CommandHistoryEntry>;
    remove: (payload: CommandHistoryRemoveInput) => Promise<{ ok: true }>;
    clear: (payload?: CommandHistoryClearInput) => Promise<{ ok: true }>;
  };
  savedCommand: {
    listScoped: () => Promise<ScopedCommandItem[]>;
    upsert: (payload: SavedCommandUpsertInput) => Promise<SavedCommand>;
    remove: (payload: SavedCommandRemoveInput) => Promise<{ ok: true }>;
  };
  backup: {
    list: (payload?: BackupListInput) => Promise<BackupArchiveMeta[]>;
    run: (payload: BackupRunInput) => Promise<{ ok: true; fileName?: string }>;
    restore: (payload: BackupRestoreInput) => Promise<{ ok: true }>;
  };
  cloudSync: {
    workspaceList: () => Promise<CloudSyncWorkspaceProfile[]>;
    workspaceAdd: (payload: CloudSyncWorkspaceAddInput) => Promise<CloudSyncWorkspaceProfile>;
    workspaceUpdate: (payload: CloudSyncWorkspaceUpdateInput) => Promise<CloudSyncWorkspaceProfile>;
    workspaceRemove: (payload: CloudSyncWorkspaceRemoveInput) => Promise<{ ok: true }>;
    workspaceExportToken: (payload: CloudSyncWorkspaceExportTokenInput) => Promise<{ token: string }>;
    workspaceParseToken: (payload: CloudSyncWorkspaceParseTokenInput) => Promise<CloudSyncWorkspaceTokenDraft>;
    status: () => Promise<{ workspaces: WorkspaceRepoStatus[] }>;
    syncNow: (payload?: CloudSyncSyncNowInput) => Promise<{ ok: true }>;
    listConflicts: () => Promise<Array<WorkspaceRepoConflict & { workspaceName: string }>>;
    testConnection: (payload: CloudSyncTestConnectionInput) => Promise<{ ok: true; displayName?: string }>;
    resolveConflict: (payload: CloudSyncResolveConflictInput) => Promise<{ ok: true }>;
    onStatus: (listener: (event: CloudSyncManagerStatusEvent) => void) => SessionEventUnsubscribe;
    onApplied: (listener: (event: { workspaceId: string }) => void) => SessionEventUnsubscribe;
  };
  masterPassword: {
    setPassword: (payload: MasterPasswordSetInput) => Promise<{ ok: true }>;
    unlockPassword: (payload: MasterPasswordUnlockInput) => Promise<{ ok: true }>;
    changePassword: (payload: MasterPasswordChangeInput) => Promise<{ ok: true }>;
    clearRemembered: (payload?: MasterPasswordClearRememberedInput) => Promise<{ ok: true }>;
    passwordStatus: (payload?: MasterPasswordStatusInput) => Promise<MasterPasswordStatusResult>;
    getCached: (payload?: MasterPasswordGetCachedInput) => Promise<MasterPasswordCachedResult>;
  };
  sshKey: {
    list: (payload?: SshKeyListInput) => Promise<SshKeyProfile[]>;
    upsert: (payload: SshKeyUpsertInput) => Promise<SshKeyProfile>;
    remove: (payload: SshKeyRemoveInput) => Promise<{ ok: true }>;
  };
  proxy: {
    list: (payload?: ProxyListInput) => Promise<ProxyProfile[]>;
    upsert: (payload: ProxyUpsertInput) => Promise<ProxyProfile>;
    remove: (payload: ProxyRemoveInput) => Promise<{ ok: true }>;
  };
  about: {
    checkUpdate: () => Promise<UpdateCheckResult>;
  };
  ping: {
    probe: (payload: PingRequestInput) => Promise<PingResult>;
  };
  traceroute: {
    run: (payload: TracerouteRunInput) => Promise<{ ok: true }>;
    stop: () => Promise<{ ok: true }>;
    onData: (listener: (event: TracerouteEvent) => void) => SessionEventUnsubscribe;
  };
  debug: {
    enableLog: () => Promise<{ ok: true }>;
    disableLog: () => Promise<{ ok: true }>;
    onLogEvent: (listener: (entry: DebugLogEntry) => void) => SessionEventUnsubscribe;
  };
  resourceOps: {
    copyConnection: (payload: ResourceCopyConnectionInput) => Promise<ConnectionProfile>;
  };
  recycleBin: {
    list: () => Promise<RecycleBinEntry[]>;
    restore: (payload: RecycleBinRestoreInput) => Promise<ConnectionProfile | SshKeyProfile>;
    purge: (payload: RecycleBinPurgeInput) => Promise<{ ok: true }>;
    clear: () => Promise<{ ok: true; deleted: number }>;
  };
}
