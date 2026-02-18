import type {
  AuditLogRecord,
  BackupArchiveMeta,
  BatchCommandExecutionResult,
  CommandExecutionResult,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionListQuery,
  ConnectionProfile,
  MigrationRecord,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  RemoteFileEntry,
  SavedCommand,
  SessionDescriptor
} from "../../core/src/index";
import type {
  AppPreferences,
  AppPreferencesPatchInput,
  AuditListInput,
  BackupListInput,
  BackupPasswordClearRememberedInput,
  BackupPasswordSetInput,
  BackupPasswordStatusInput,
  BackupPasswordUnlockInput,
  BackupRestoreInput,
  BackupRunInput,
  CommandBatchExecInput,
  CommandExecInput,
  CommandHistoryClearInput,
  CommandHistoryListInput,
  CommandHistoryPushInput,
  CommandHistoryRemoveInput,
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
  MonitorSystemStartInput,
  MonitorSystemSelectInterfaceInput,
  MonitorSystemStopInput,
  MonitorSnapshotInput,
  SavedCommandListInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  SessionCloseInput,
  SessionDataEvent,
  SessionOpenInput,
  SessionResizeInput,
  SessionStatusEvent,
  StorageMigrationsInput,
  SessionWriteInput,
  SftpDeleteInput,
  SftpDownloadInput,
  SftpEditOpenInput,
  SftpEditStatusEvent,
  SftpEditStopInput,
  SftpEditSessionInfo,
  SftpListInput,
  SftpMkdirInput,
  SftpRenameInput,
  SftpTransferStatusEvent,
  SftpUploadInput,
  TemplateParamsListInput,
  TemplateParamsUpsertInput,
  TemplateParamsClearInput
} from "./contracts";

export type SessionEventUnsubscribe = () => void;

export interface NextShellApi {
  connection: {
    list: (query: ConnectionListQuery) => Promise<ConnectionProfile[]>;
    upsert: (payload: ConnectionUpsertInput) => Promise<ConnectionProfile>;
    remove: (payload: ConnectionRemoveInput) => Promise<{ ok: true }>;
  };
  session: {
    open: (payload: SessionOpenInput) => Promise<SessionDescriptor>;
    write: (payload: SessionWriteInput) => Promise<{ ok: true }>;
    resize: (payload: SessionResizeInput) => Promise<{ ok: true }>;
    close: (payload: SessionCloseInput) => Promise<{ ok: true }>;
    onData: (listener: (event: SessionDataEvent) => void) => SessionEventUnsubscribe;
    onStatus: (listener: (event: SessionStatusEvent) => void) => SessionEventUnsubscribe;
  };
  monitor: {
    snapshot: (payload: MonitorSnapshotInput) => Promise<MonitorSnapshot>;
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
    list: (payload: AuditListInput) => Promise<AuditLogRecord[]>;
  };
  storage: {
    migrations: (payload?: StorageMigrationsInput) => Promise<MigrationRecord[]>;
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
    upload: (payload: SftpUploadInput) => Promise<{ ok: true }>;
    download: (payload: SftpDownloadInput) => Promise<{ ok: true }>;
    mkdir: (payload: SftpMkdirInput) => Promise<{ ok: true }>;
    rename: (payload: SftpRenameInput) => Promise<{ ok: true }>;
    remove: (payload: SftpDeleteInput) => Promise<{ ok: true }>;
    editOpen: (payload: SftpEditOpenInput) => Promise<{ editId: string; localPath: string }>;
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
    list: (payload?: SavedCommandListInput) => Promise<SavedCommand[]>;
    upsert: (payload: SavedCommandUpsertInput) => Promise<SavedCommand>;
    remove: (payload: SavedCommandRemoveInput) => Promise<{ ok: true }>;
  };
  backup: {
    list: (payload?: BackupListInput) => Promise<BackupArchiveMeta[]>;
    run: (payload: BackupRunInput) => Promise<{ ok: true; fileName?: string }>;
    restore: (payload: BackupRestoreInput) => Promise<{ ok: true }>;
    setPassword: (payload: BackupPasswordSetInput) => Promise<{ ok: true }>;
    unlockPassword: (payload: BackupPasswordUnlockInput) => Promise<{ ok: true }>;
    clearRemembered: (payload?: BackupPasswordClearRememberedInput) => Promise<{ ok: true }>;
    passwordStatus: (payload?: BackupPasswordStatusInput) => Promise<{
      isSet: boolean;
      isUnlocked: boolean;
      keytarAvailable: boolean;
    }>;
  };
  templateParams: {
    list: (payload?: TemplateParamsListInput) => Promise<CommandTemplateParam[]>;
    upsert: (payload: TemplateParamsUpsertInput) => Promise<{ ok: true }>;
    clear: (payload: TemplateParamsClearInput) => Promise<{ ok: true }>;
  };
}
