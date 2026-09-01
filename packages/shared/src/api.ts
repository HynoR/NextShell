import type {
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
  ConnectionFolder,
  SavedCommand,
  SessionDescriptor,
  SystemInfoSnapshot,
  SshKeyProfile,
  WorkspaceRepoStatus
} from "../../core/src/index";
import type {
  AppPreferences,
  AppPreferencesPatchInput,
  ConnectionFolderCreateInput,
  SshKeyGenerateInput,
  SshKeyUsageInput,
  SshKeyUsageItem,
  ConnectionFolderListInput,
  ConnectionFolderMoveInput,
  ConnectionFolderRemoveInput,
  ConnectionFolderRenameInput,
  ConnectionFolderReorderInput,
  DebugLogEntry,
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
  AgentClientConfigResult,
  AgentCopyClientConfigInput,
  AgentExportMcpbInput,
  AgentExportMcpbResult,
  AgentInstallClaudeDesktopInput,
  AgentInstallClaudeDesktopResult,
  AgentInstallCursorInput,
  AgentInstallCursorResult,
  AgentActivityEvent,
  AgentDisableInput,
  AgentEnableInput,
  AgentEndpointStatus,
  AgentPromptRequest,
  AgentPromptResponse,
  AgentRotateTokenInput,
  AgentSessionControlEvent,
  AgentSessionFocusEvent,
  AgentSetHaltedInput,
  AgentStatusInput,
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
  TerminalNotificationActionEvent,
  TerminalNotificationInput,
  TerminalProgressInput,
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
  RendererErrorReportInput,
  ResourceCopyConnectionInput,
  RecycleBinRestoreInput,
  RecycleBinPurgeInput
} from "./contracts";
import { IPCChannel } from "./channels";

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
    batchUpdateAuth: (
      payload: ConnectionBatchAuthUpdateInput
    ) => Promise<ConnectionBatchAuthUpdateResult>;
    remove: (payload: ConnectionRemoveInput) => Promise<{ ok: true }>;
    exportToFile: (
      payload: ConnectionExportInput
    ) => Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }>;
    exportBatch: (payload: ConnectionExportBatchInput) => Promise<ConnectionExportBatchResult>;
    revealPassword: (
      payload: ConnectionRevealPasswordInput
    ) => Promise<ConnectionRevealPasswordResult>;
    importPreview: (payload: ConnectionImportPreviewInput) => Promise<ConnectionImportEntry[]>;
    importFinalShellPreview: (
      payload: ConnectionImportFinalShellPreviewInput
    ) => Promise<ConnectionImportEntry[]>;
    importDirectoryPreview: (
      payload: ConnectionImportDirectoryPreviewInput
    ) => Promise<ConnectionImportDirectoryPreviewResult>;
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
  terminal: {
    /** OSC 9 / 777 desktop notification; main shows it only when the window is unfocused. */
    showNotification: (
      payload: TerminalNotificationInput
    ) => Promise<{ ok: boolean; error?: string }>;
    /** OSC 9;4 taskbar progress bar state. */
    setProgress: (payload: TerminalProgressInput) => Promise<{ ok: boolean; error?: string }>;
    /** Emitted when the user clicks a terminal notification; renderer should focus the session. */
    onNotificationAction: (
      listener: (event: TerminalNotificationActionEvent) => void
    ) => SessionEventUnsubscribe;
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
    getNetworkConnections: (
      payload: MonitorNetworkConnectionsInput
    ) => Promise<NetworkConnection[]>;
  };
  command: {
    exec: (payload: CommandExecInput) => Promise<CommandExecutionResult>;
    execBatch: (payload: CommandBatchExecInput) => Promise<BatchCommandExecutionResult>;
  };
  settings: {
    get: () => Promise<AppPreferences>;
    update: (payload: AppPreferencesPatchInput) => Promise<AppPreferences>;
  };
  dialog: {
    openFiles: (
      payload?: DialogOpenFilesInput
    ) => Promise<{ canceled: boolean; filePaths: string[] }>;
    openDirectory: (
      payload?: DialogOpenDirectoryInput
    ) => Promise<{ canceled: boolean; filePath?: string }>;
    openPath: (payload: DialogOpenPathInput) => Promise<{ ok: boolean; error?: string }>;
  };
  sftp: {
    list: (payload: SftpListInput) => Promise<RemoteFileEntry[]>;
    listLocal: (payload: SftpListLocalInput) => Promise<RemoteFileEntry[]>;
    upload: (payload: SftpUploadInput) => Promise<{ ok: true }>;
    download: (payload: SftpDownloadInput) => Promise<{ ok: true }>;
    uploadPacked: (payload: SftpUploadPackedInput) => Promise<{ ok: true }>;
    downloadPacked: (
      payload: SftpDownloadPackedInput
    ) => Promise<{ ok: true; localArchivePath: string }>;
    transferPacked: (payload: SftpTransferPackedInput) => Promise<{ ok: true }>;
    cancelTransfer: (payload: SftpTransferCancelInput) => Promise<{ ok: true; cancelled: boolean }>;
    mkdir: (payload: SftpMkdirInput) => Promise<{ ok: true }>;
    rename: (payload: SftpRenameInput) => Promise<{ ok: true }>;
    remove: (payload: SftpDeleteInput) => Promise<{ ok: true }>;
    editOpen: (payload: SftpEditOpenInput) => Promise<{ editId: string; localPath: string }>;
    editOpenBuiltin: (
      payload: SftpEditOpenBuiltinInput
    ) => Promise<{ editId: string; content: string }>;
    editSaveBuiltin: (payload: SftpEditSaveBuiltinInput) => Promise<{ ok: true }>;
    editStop: (payload: SftpEditStopInput) => Promise<{ ok: true }>;
    editStopAll: () => Promise<{ ok: true }>;
    editList: () => Promise<SftpEditSessionInfo[]>;
    onEditStatus: (listener: (event: SftpEditStatusEvent) => void) => SessionEventUnsubscribe;
    onTransferStatus: (
      listener: (event: SftpTransferStatusEvent) => void
    ) => SessionEventUnsubscribe;
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
  cloudSync: {
    workspaceList: () => Promise<CloudSyncWorkspaceProfile[]>;
    workspaceAdd: (payload: CloudSyncWorkspaceAddInput) => Promise<CloudSyncWorkspaceProfile>;
    workspaceUpdate: (payload: CloudSyncWorkspaceUpdateInput) => Promise<CloudSyncWorkspaceProfile>;
    workspaceRemove: (payload: CloudSyncWorkspaceRemoveInput) => Promise<{ ok: true }>;
    workspaceExportToken: (
      payload: CloudSyncWorkspaceExportTokenInput
    ) => Promise<{ token: string }>;
    workspaceParseToken: (
      payload: CloudSyncWorkspaceParseTokenInput
    ) => Promise<CloudSyncWorkspaceTokenDraft>;
    status: () => Promise<{ workspaces: WorkspaceRepoStatus[] }>;
    syncNow: (payload?: CloudSyncSyncNowInput) => Promise<{ ok: true }>;
    testConnection: (
      payload: CloudSyncTestConnectionInput
    ) => Promise<{ ok: true; displayName?: string }>;
    onStatus: (listener: (event: CloudSyncManagerStatusEvent) => void) => SessionEventUnsubscribe;
    onApplied: (listener: (event: { workspaceId: string }) => void) => SessionEventUnsubscribe;
  };
  agent: {
    /** 端点状态：是否启用、socket 路径、TCP 端口、token、已连客户端、endpoint.json 路径 */
    status: (payload?: AgentStatusInput) => Promise<AgentEndpointStatus>;
    enable: (payload?: AgentEnableInput) => Promise<AgentEndpointStatus>;
    disable: (payload?: AgentDisableInput) => Promise<AgentEndpointStatus>;
    /** 轮换 TCP Bearer token，并断开所有已连客户端 */
    rotateToken: (payload?: AgentRotateTokenInput) => Promise<AgentEndpointStatus>;
    /** 把客户端接入配置（`claude mcp add` 命令与 JSON 片段）写入剪贴板 */
    copyClientConfig: (payload?: AgentCopyClientConfigInput) => Promise<AgentClientConfigResult>;
    /** 打开 Cursor 一键安装 deeplink（`cursor://…/mcp/install`） */
    installCursor: (payload?: AgentInstallCursorInput) => Promise<AgentInstallCursorResult>;
    /** 把 stdio 桥接配置合并写入 claude_desktop_config.json（调用方需先向用户确认） */
    installClaudeDesktop: (
      payload?: AgentInstallClaudeDesktopInput
    ) => Promise<AgentInstallClaudeDesktopResult>;
    /** 导出 Claude Desktop 一键安装包（.mcpb）；弹保存对话框，取消时返回 canceled */
    exportMcpb: (payload?: AgentExportMcpbInput) => Promise<AgentExportMcpbResult>;
    /** 回应主进程发起的确认、选择或文本问询。 */
    respondPrompt: (payload: AgentPromptResponse) => Promise<{ ok: true }>;
    /** 全局断闸：拉下后所有 Agent 工具调用立即被拒，并遗忘全部「本会话始终允许」。 */
    setHalted: (payload: AgentSetHaltedInput) => Promise<AgentEndpointStatus>;
    onPrompt: (listener: (event: AgentPromptRequest) => void) => SessionEventUnsubscribe;
    onActivity: (listener: (event: AgentActivityEvent) => void) => SessionEventUnsubscribe;
    /** 某个会话正被 / 不再被 Agent 驱动，用于标签徽标。 */
    onSessionControl: (
      listener: (event: AgentSessionControlEvent) => void
    ) => SessionEventUnsubscribe;
    /** Agent 请求把界面切到某个标签并置顶窗口。 */
    onSessionFocus: (listener: (event: AgentSessionFocusEvent) => void) => SessionEventUnsubscribe;
  };
  connectionFolder: {
    list: (payload?: ConnectionFolderListInput) => Promise<ConnectionFolder[]>;
    create: (payload: ConnectionFolderCreateInput) => Promise<ConnectionFolder>;
    rename: (payload: ConnectionFolderRenameInput) => Promise<ConnectionFolder>;
    move: (payload: ConnectionFolderMoveInput) => Promise<ConnectionFolder>;
    reorder: (payload: ConnectionFolderReorderInput) => Promise<ConnectionFolder>;
    remove: (payload: ConnectionFolderRemoveInput) => Promise<{ ok: true }>;
  };
  sshKey: {
    list: (payload?: SshKeyListInput) => Promise<SshKeyProfile[]>;
    upsert: (payload: SshKeyUpsertInput) => Promise<SshKeyProfile>;
    remove: (payload: SshKeyRemoveInput) => Promise<{ ok: true }>;
    generate: (payload: SshKeyGenerateInput) => Promise<SshKeyProfile>;
    usage: (payload: SshKeyUsageInput) => Promise<SshKeyUsageItem[]>;
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
    /** Renderer crash/global-error report, appended to the main-process log file. */
    reportRendererError: (payload: RendererErrorReportInput) => Promise<{ ok: true }>;
    /** Main-process batches are unpacked by preload; callers still receive entries in order. */
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

type AsyncApiMethod = (...args: never[]) => Promise<unknown>;

export interface IpcInvokeMethods {
  [IPCChannel.ConnectionList]: NextShellApi["connection"]["list"];
  [IPCChannel.ConnectionUpsert]: NextShellApi["connection"]["upsert"];
  [IPCChannel.ConnectionBatchAuthUpdate]: NextShellApi["connection"]["batchUpdateAuth"];
  [IPCChannel.ConnectionRemove]: NextShellApi["connection"]["remove"];
  [IPCChannel.ConnectionExport]: NextShellApi["connection"]["exportToFile"];
  [IPCChannel.ConnectionExportBatch]: NextShellApi["connection"]["exportBatch"];
  [IPCChannel.ConnectionRevealPassword]: NextShellApi["connection"]["revealPassword"];
  [IPCChannel.ConnectionImportPreview]: NextShellApi["connection"]["importPreview"];
  [IPCChannel.ConnectionImportFinalShellPreview]: NextShellApi["connection"]["importFinalShellPreview"];
  [IPCChannel.ConnectionImportDirectoryPreview]: NextShellApi["connection"]["importDirectoryPreview"];
  [IPCChannel.ConnectionImportExecute]: NextShellApi["connection"]["importExecute"];
  [IPCChannel.SettingsGet]: NextShellApi["settings"]["get"];
  [IPCChannel.SettingsUpdate]: NextShellApi["settings"]["update"];
  [IPCChannel.DialogOpenFiles]: NextShellApi["dialog"]["openFiles"];
  [IPCChannel.DialogOpenDirectory]: NextShellApi["dialog"]["openDirectory"];
  [IPCChannel.DialogOpenPath]: NextShellApi["dialog"]["openPath"];
  [IPCChannel.SessionOpen]: NextShellApi["session"]["open"];
  [IPCChannel.SessionWrite]: NextShellApi["session"]["write"];
  [IPCChannel.SessionResize]: NextShellApi["session"]["resize"];
  [IPCChannel.SessionClose]: NextShellApi["session"]["close"];
  [IPCChannel.SessionGetHomeDir]: NextShellApi["session"]["getHomeDir"];
  [IPCChannel.StreamDeliveryAck]: NextShellApi["session"]["ackData"];
  [IPCChannel.TerminalNotification]: NextShellApi["terminal"]["showNotification"];
  [IPCChannel.TerminalProgress]: NextShellApi["terminal"]["setProgress"];
  [IPCChannel.MonitorSystemInfoSnapshot]: NextShellApi["monitor"]["getSystemInfoSnapshot"];
  [IPCChannel.MonitorSystemStart]: NextShellApi["monitor"]["startSystem"];
  [IPCChannel.MonitorSystemStop]: NextShellApi["monitor"]["stopSystem"];
  [IPCChannel.MonitorSystemSelectInterface]: NextShellApi["monitor"]["selectSystemInterface"];
  [IPCChannel.MonitorProcessStart]: NextShellApi["monitor"]["startProcess"];
  [IPCChannel.MonitorProcessStop]: NextShellApi["monitor"]["stopProcess"];
  [IPCChannel.MonitorProcessDetail]: NextShellApi["monitor"]["getProcessDetail"];
  [IPCChannel.MonitorProcessKill]: NextShellApi["monitor"]["killProcess"];
  [IPCChannel.MonitorNetworkStart]: NextShellApi["monitor"]["startNetwork"];
  [IPCChannel.MonitorNetworkStop]: NextShellApi["monitor"]["stopNetwork"];
  [IPCChannel.MonitorNetworkConnections]: NextShellApi["monitor"]["getNetworkConnections"];
  [IPCChannel.CommandExec]: NextShellApi["command"]["exec"];
  [IPCChannel.CommandBatchExec]: NextShellApi["command"]["execBatch"];
  [IPCChannel.SftpList]: NextShellApi["sftp"]["list"];
  [IPCChannel.SftpListLocal]: NextShellApi["sftp"]["listLocal"];
  [IPCChannel.SftpUpload]: NextShellApi["sftp"]["upload"];
  [IPCChannel.SftpUploadPacked]: NextShellApi["sftp"]["uploadPacked"];
  [IPCChannel.SftpDownload]: NextShellApi["sftp"]["download"];
  [IPCChannel.SftpTransferCancel]: NextShellApi["sftp"]["cancelTransfer"];
  [IPCChannel.SftpDownloadPacked]: NextShellApi["sftp"]["downloadPacked"];
  [IPCChannel.SftpTransferPacked]: NextShellApi["sftp"]["transferPacked"];
  [IPCChannel.SftpMkdir]: NextShellApi["sftp"]["mkdir"];
  [IPCChannel.SftpRename]: NextShellApi["sftp"]["rename"];
  [IPCChannel.SftpDelete]: NextShellApi["sftp"]["remove"];
  [IPCChannel.CommandHistoryList]: NextShellApi["commandHistory"]["list"];
  [IPCChannel.CommandHistoryPush]: NextShellApi["commandHistory"]["push"];
  [IPCChannel.CommandHistoryRemove]: NextShellApi["commandHistory"]["remove"];
  [IPCChannel.CommandHistoryClear]: NextShellApi["commandHistory"]["clear"];
  [IPCChannel.SavedCommandListScoped]: NextShellApi["savedCommand"]["listScoped"];
  [IPCChannel.SavedCommandUpsert]: NextShellApi["savedCommand"]["upsert"];
  [IPCChannel.SavedCommandRemove]: NextShellApi["savedCommand"]["remove"];
  [IPCChannel.SftpEditOpen]: NextShellApi["sftp"]["editOpen"];
  [IPCChannel.SftpEditStop]: NextShellApi["sftp"]["editStop"];
  [IPCChannel.SftpEditStopAll]: NextShellApi["sftp"]["editStopAll"];
  [IPCChannel.SftpEditList]: NextShellApi["sftp"]["editList"];
  [IPCChannel.SftpEditOpenBuiltin]: NextShellApi["sftp"]["editOpenBuiltin"];
  [IPCChannel.SftpEditSaveBuiltin]: NextShellApi["sftp"]["editSaveBuiltin"];
  [IPCChannel.AgentStatus]: NextShellApi["agent"]["status"];
  [IPCChannel.AgentEnable]: NextShellApi["agent"]["enable"];
  [IPCChannel.AgentDisable]: NextShellApi["agent"]["disable"];
  [IPCChannel.AgentRotateToken]: NextShellApi["agent"]["rotateToken"];
  [IPCChannel.AgentCopyClientConfig]: NextShellApi["agent"]["copyClientConfig"];
  [IPCChannel.AgentInstallCursor]: NextShellApi["agent"]["installCursor"];
  [IPCChannel.AgentInstallClaudeDesktop]: NextShellApi["agent"]["installClaudeDesktop"];
  [IPCChannel.AgentExportMcpb]: NextShellApi["agent"]["exportMcpb"];
  [IPCChannel.AgentPromptResponse]: NextShellApi["agent"]["respondPrompt"];
  [IPCChannel.AgentSetHalted]: NextShellApi["agent"]["setHalted"];
  [IPCChannel.CloudSyncWorkspaceList]: NextShellApi["cloudSync"]["workspaceList"];
  [IPCChannel.CloudSyncWorkspaceAdd]: NextShellApi["cloudSync"]["workspaceAdd"];
  [IPCChannel.CloudSyncWorkspaceUpdate]: NextShellApi["cloudSync"]["workspaceUpdate"];
  [IPCChannel.CloudSyncWorkspaceRemove]: NextShellApi["cloudSync"]["workspaceRemove"];
  [IPCChannel.CloudSyncWorkspaceExportToken]: NextShellApi["cloudSync"]["workspaceExportToken"];
  [IPCChannel.CloudSyncWorkspaceParseToken]: NextShellApi["cloudSync"]["workspaceParseToken"];
  [IPCChannel.CloudSyncStatus]: NextShellApi["cloudSync"]["status"];
  [IPCChannel.CloudSyncSyncNow]: NextShellApi["cloudSync"]["syncNow"];
  [IPCChannel.CloudSyncTestConnection]: NextShellApi["cloudSync"]["testConnection"];
  [IPCChannel.ConnectionFolderList]: NextShellApi["connectionFolder"]["list"];
  [IPCChannel.ConnectionFolderCreate]: NextShellApi["connectionFolder"]["create"];
  [IPCChannel.ConnectionFolderRename]: NextShellApi["connectionFolder"]["rename"];
  [IPCChannel.ConnectionFolderMove]: NextShellApi["connectionFolder"]["move"];
  [IPCChannel.ConnectionFolderReorder]: NextShellApi["connectionFolder"]["reorder"];
  [IPCChannel.ConnectionFolderRemove]: NextShellApi["connectionFolder"]["remove"];
  [IPCChannel.SshKeyList]: NextShellApi["sshKey"]["list"];
  [IPCChannel.SshKeyUpsert]: NextShellApi["sshKey"]["upsert"];
  [IPCChannel.SshKeyRemove]: NextShellApi["sshKey"]["remove"];
  [IPCChannel.SshKeyGenerate]: NextShellApi["sshKey"]["generate"];
  [IPCChannel.SshKeyUsage]: NextShellApi["sshKey"]["usage"];
  [IPCChannel.ProxyList]: NextShellApi["proxy"]["list"];
  [IPCChannel.ProxyUpsert]: NextShellApi["proxy"]["upsert"];
  [IPCChannel.ProxyRemove]: NextShellApi["proxy"]["remove"];
  [IPCChannel.UpdateCheck]: NextShellApi["about"]["checkUpdate"];
  [IPCChannel.Ping]: NextShellApi["ping"]["probe"];
  [IPCChannel.TracerouteRun]: NextShellApi["traceroute"]["run"];
  [IPCChannel.TracerouteStop]: NextShellApi["traceroute"]["stop"];
  [IPCChannel.DebugLogEnable]: NextShellApi["debug"]["enableLog"];
  [IPCChannel.DebugLogDisable]: NextShellApi["debug"]["disableLog"];
  [IPCChannel.DebugRendererError]: NextShellApi["debug"]["reportRendererError"];
  [IPCChannel.ResourceCopyConnection]: NextShellApi["resourceOps"]["copyConnection"];
  [IPCChannel.RecycleBinList]: NextShellApi["recycleBin"]["list"];
  [IPCChannel.RecycleBinRestore]: NextShellApi["recycleBin"]["restore"];
  [IPCChannel.RecycleBinPurge]: NextShellApi["recycleBin"]["purge"];
  [IPCChannel.RecycleBinClear]: NextShellApi["recycleBin"]["clear"];
}

export type IpcInvokeChannel = keyof IpcInvokeMethods;

export type IpcInvokePayload<C extends IpcInvokeChannel> =
  IpcInvokeMethods[C] extends AsyncApiMethod
    ? Parameters<IpcInvokeMethods[C]> extends []
      ? Record<string, never>
      : [] extends Parameters<IpcInvokeMethods[C]>
        ? NonNullable<Parameters<IpcInvokeMethods[C]>[0]> | Record<string, never>
        : NonNullable<Parameters<IpcInvokeMethods[C]>[0]>
    : never;

export type IpcInvokeResult<C extends IpcInvokeChannel> = IpcInvokeMethods[C] extends AsyncApiMethod
  ? Awaited<ReturnType<IpcInvokeMethods[C]>>
  : never;
