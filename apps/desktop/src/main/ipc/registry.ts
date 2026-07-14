import type { IpcMainInvokeEvent } from "electron";
import { z } from "zod";
import {
  IPCChannel,
  type IpcInvokeChannel,
  type IpcInvokePayload,
  type IpcInvokeResult,
  auditClearSchema,
  commandBatchExecSchema,
  commandExecSchema,
  connectionBatchAuthUpdateSchema,
  commandHistoryClearSchema,
  commandHistoryListSchema,
  commandHistoryPushSchema,
  commandHistoryRemoveSchema,
  connectionExportSchema,
  connectionExportBatchSchema,
  connectionRevealPasswordSchema,
  connectionImportDirectoryPreviewSchema,
  connectionImportFinalShellPreviewSchema,
  connectionImportPreviewSchema,
  connectionImportExecuteSchema,
  connectionListQuerySchema,
  connectionRemoveSchema,
  connectionUpsertSchema,
  dialogOpenDirectorySchema,
  dialogOpenFilesSchema,
  dialogOpenPathSchema,
  monitorSystemInfoSnapshotSchema,
  monitorSystemStartSchema,
  monitorSystemStopSchema,
  monitorSystemSelectInterfaceSchema,
  monitorProcessStartSchema,
  monitorProcessStopSchema,
  monitorProcessDetailSchema,
  monitorProcessKillSchema,
  monitorNetworkStartSchema,
  monitorNetworkStopSchema,
  monitorNetworkConnectionsSchema,
  settingsGetSchema,
  settingsUpdateSchema,
  sessionCloseSchema,
  sessionGetHomeDirSchema,
  streamDeliveryAckSchema,
  sftpDeleteSchema,
  sftpDownloadSchema,
  sftpDownloadPackedSchema,
  sftpListLocalSchema,
  sftpMkdirSchema,
  sessionOpenSchema,
  sessionResizeSchema,
  sessionWriteSchema,
  sftpListSchema,
  sftpRenameSchema,
  sftpTransferPackedSchema,
  sftpTransferCancelSchema,
  sftpUploadSchema,
  sftpUploadPackedSchema,
  savedCommandRemoveSchema,
  savedCommandUpsertSchema,
  sftpEditOpenSchema,
  sftpEditStopSchema,
  sftpEditOpenBuiltinSchema,
  sftpEditSaveBuiltinSchema,
  backupListSchema,
  backupRunSchema,
  backupRestoreSchema,
  cloudSyncWorkspaceListSchema,
  cloudSyncWorkspaceAddSchema,
  cloudSyncWorkspaceUpdateSchema,
  cloudSyncWorkspaceRemoveSchema,
  cloudSyncWorkspaceExportTokenSchema,
  cloudSyncWorkspaceParseTokenSchema,
  cloudSyncStatusSchema,
  cloudSyncSyncNowSchema,
  cloudSyncListConflictsSchema,
  cloudSyncTestConnectionSchema,
  cloudSyncResolveConflictSchema,
  masterPasswordSetSchema,
  masterPasswordUnlockSchema,
  masterPasswordChangeSchema,
  masterPasswordClearRememberedSchema,
  masterPasswordStatusSchema,
  masterPasswordGetCachedSchema,
  sshKeyListSchema,
  sshKeyUpsertSchema,
  sshKeyRemoveSchema,
  proxyListSchema,
  proxyUpsertSchema,
  proxyRemoveSchema,
  updateCheckSchema,
  pingRequestSchema,
  tracerouteRunSchema,
  resourceCopyConnectionSchema,
  recycleBinListSchema,
  recycleBinRestoreSchema,
  recycleBinPurgeSchema,
  recycleBinClearSchema
} from "../../../../../packages/shared/src/index";
import type { ServiceContainer } from "../services/container-types";

type IpcChannelValue = (typeof IPCChannel)[keyof typeof IPCChannel];

/**
 * One invoke-handler registration.
 *
 * - `schema: null` → the legacy handler never called parsePayload; `dispatch`
 *   receives `undefined` as input and must ignore it (label is unused then).
 * - `coerceEmptyPayload` → the legacy handler parsed `payload ?? {}` instead
 *   of `payload`.
 * - The public shape is erased for iteration; `define` performs the channel,
 *   schema input, and dispatch result checks before that erasure.
 */
export interface IpcInvokeEntry {
  channel: IpcChannelValue;
  schema: z.ZodType | null;
  label: string;
  coerceEmptyPayload?: boolean;
  dispatch(services: ServiceContainer, input: unknown, event: IpcMainInvokeEvent): unknown;
}

type SchemaForChannel<C extends IpcInvokeChannel, S extends z.ZodType | null> = S extends z.ZodType
  ? Exclude<IpcInvokePayload<C>, Record<string, never>> extends z.output<S> | z.input<S>
    ? S
    : never
  : S;

type ParsedInput<S extends z.ZodType | null> = S extends z.ZodType ? z.output<S> : undefined;

interface TypedIpcInvokeEntry<C extends IpcInvokeChannel, S extends z.ZodType | null> {
  channel: C;
  schema: SchemaForChannel<C, S>;
  label: string;
  coerceEmptyPayload?: boolean;
  dispatch(
    services: ServiceContainer,
    input: ParsedInput<S>,
    event: IpcMainInvokeEvent
  ): IpcInvokeResult<C> | Promise<IpcInvokeResult<C>>;
}

/** Type-safe entry constructor: the shared channel map locks schema input and
 * dispatch output to the public preload API contract. */
const define = <C extends IpcInvokeChannel, S extends z.ZodType | null>(
  entry: TypedIpcInvokeEntry<C, S>
): IpcInvokeEntry => entry as unknown as IpcInvokeEntry;

const emptyObjectSchema = z.object({});

export const ipcInvokeRegistry: ReadonlyArray<IpcInvokeEntry> = [
  // ─── Connections ──────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.ConnectionList,
    schema: connectionListQuerySchema,
    label: "连接查询",
    coerceEmptyPayload: true,
    dispatch: (services, query) => services.connections.listConnections(query)
  }),
  define({
    channel: IPCChannel.ConnectionUpsert,
    schema: connectionUpsertSchema,
    label: "连接保存",
    dispatch: (services, input) => services.connections.upsertConnection(input)
  }),
  define({
    channel: IPCChannel.ConnectionBatchAuthUpdate,
    schema: connectionBatchAuthUpdateSchema,
    label: "批量绑定认证",
    dispatch: (services, input) => services.connections.batchUpdateConnectionAuth(input)
  }),
  define({
    channel: IPCChannel.ConnectionRemove,
    schema: connectionRemoveSchema,
    label: "连接删除",
    // Orchestration: recycle-bin snapshot + runtime cleanup across services.
    dispatch: (services, input) => services.removeConnection(input.id)
  }),
  define({
    channel: IPCChannel.ConnectionExport,
    schema: connectionExportSchema,
    label: "连接导出",
    dispatch: (services, input, event) =>
      services.importExport.exportConnections(event.sender, input)
  }),
  define({
    channel: IPCChannel.ConnectionExportBatch,
    schema: connectionExportBatchSchema,
    label: "连接批量导出",
    dispatch: (services, input) => services.importExport.exportConnectionsBatch(input)
  }),
  define({
    channel: IPCChannel.ConnectionRevealPassword,
    schema: connectionRevealPasswordSchema,
    label: "查看连接密码",
    dispatch: (services, input) =>
      services.backupPassword.revealConnectionPassword(input.connectionId, input.masterPassword)
  }),
  define({
    channel: IPCChannel.ConnectionImportPreview,
    schema: connectionImportPreviewSchema,
    label: "连接导入预览",
    dispatch: (services, input) => services.importExport.importConnectionsPreview(input)
  }),
  define({
    channel: IPCChannel.ConnectionImportFinalShellPreview,
    schema: connectionImportFinalShellPreviewSchema,
    label: "FinalShell 导入预览",
    dispatch: (services, input) => services.importExport.importFinalShellConnectionsPreview(input)
  }),
  define({
    channel: IPCChannel.ConnectionImportDirectoryPreview,
    schema: connectionImportDirectoryPreviewSchema,
    label: "连接目录导入预览",
    dispatch: (services, input) => services.importExport.importConnectionsDirectoryPreview(input)
  }),
  define({
    channel: IPCChannel.ConnectionImportExecute,
    schema: connectionImportExecuteSchema,
    label: "连接导入执行",
    dispatch: (services, input) => services.importExport.importConnectionsExecute(input)
  }),

  // ─── Settings & Dialogs ───────────────────────────────────────────────────
  define({
    channel: IPCChannel.SettingsGet,
    schema: settingsGetSchema,
    label: "设置读取",
    coerceEmptyPayload: true,
    dispatch: (services) => services.getAppPreferences()
  }),
  define({
    channel: IPCChannel.SettingsUpdate,
    schema: settingsUpdateSchema,
    label: "设置更新",
    dispatch: (services, input) => services.preferences.updateAppPreferences(input)
  }),
  define({
    channel: IPCChannel.DialogOpenFiles,
    schema: dialogOpenFilesSchema,
    label: "打开文件选择器",
    coerceEmptyPayload: true,
    dispatch: (services, input, event) => services.preferences.openFilesDialog(event.sender, input)
  }),
  define({
    channel: IPCChannel.DialogOpenDirectory,
    schema: dialogOpenDirectorySchema,
    label: "打开目录选择器",
    coerceEmptyPayload: true,
    dispatch: (services, input, event) =>
      services.preferences.openDirectoryDialog(event.sender, input)
  }),
  define({
    channel: IPCChannel.DialogOpenPath,
    schema: dialogOpenPathSchema,
    label: "打开路径",
    dispatch: (services, input, event) => services.preferences.openLocalPath(event.sender, input)
  }),

  // ─── Sessions ─────────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.SessionOpen,
    schema: sessionOpenSchema,
    label: "会话打开",
    dispatch: (services, input, event) => services.sessions.openSession(input, event.sender)
  }),
  define({
    channel: IPCChannel.SessionWrite,
    schema: sessionWriteSchema,
    label: "会话写入",
    dispatch: (services, input) => services.sessions.writeSession(input.sessionId, input.data)
  }),
  define({
    channel: IPCChannel.SessionResize,
    schema: sessionResizeSchema,
    label: "会话尺寸调整",
    dispatch: (services, input) =>
      services.sessions.resizeSession(input.sessionId, input.cols, input.rows)
  }),
  define({
    channel: IPCChannel.SessionClose,
    schema: sessionCloseSchema,
    label: "会话关闭",
    dispatch: (services, input) => services.sessions.closeSession(input.sessionId)
  }),
  define({
    channel: IPCChannel.SessionGetHomeDir,
    schema: sessionGetHomeDirSchema,
    label: "获取远端 Home 目录",
    dispatch: (services, input) => services.commands.getSessionHomeDir(input.connectionId)
  }),
  define({
    channel: IPCChannel.StreamDeliveryAck,
    schema: streamDeliveryAckSchema,
    label: "流式消息确认",
    dispatch: (services, input) => services.sessions.ackStreamDelivery(input)
  }),

  // ─── System Monitor ───────────────────────────────────────────────────────
  define({
    channel: IPCChannel.MonitorSystemInfoSnapshot,
    schema: monitorSystemInfoSnapshotSchema,
    label: "系统信息快照",
    dispatch: (services, input) => services.monitors.getSystemInfoSnapshot(input.connectionId)
  }),
  define({
    channel: IPCChannel.MonitorSystemStart,
    schema: monitorSystemStartSchema,
    label: "系统监控启动",
    dispatch: (services, input, event) =>
      services.monitors.startSystemMonitor(input.connectionId, event.sender)
  }),
  define({
    channel: IPCChannel.MonitorSystemStop,
    schema: monitorSystemStopSchema,
    label: "系统监控停止",
    dispatch: (services, input) => services.monitors.stopSystemMonitor(input.connectionId)
  }),
  define({
    channel: IPCChannel.MonitorSystemSelectInterface,
    schema: monitorSystemSelectInterfaceSchema,
    label: "系统监控网卡切换",
    dispatch: (services, input) =>
      services.monitors.selectSystemNetworkInterface(input.connectionId, input.networkInterface)
  }),

  // ─── Commands & Audit ─────────────────────────────────────────────────────
  define({
    channel: IPCChannel.CommandExec,
    schema: commandExecSchema,
    label: "命令执行",
    dispatch: (services, input) => services.commands.execCommand(input.connectionId, input.command)
  }),
  define({
    channel: IPCChannel.CommandBatchExec,
    schema: commandBatchExecSchema,
    label: "批量命令执行",
    dispatch: (services, input) => services.commands.execBatchCommand(input)
  }),
  define({
    channel: IPCChannel.AuditClear,
    schema: auditClearSchema,
    label: "审计日志清空",
    coerceEmptyPayload: true,
    dispatch: (services) => services.connections.clearAuditLogs()
  }),

  // ─── SFTP ─────────────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.SftpList,
    schema: sftpListSchema,
    label: "文件列表",
    dispatch: (services, input) => services.sftp.listRemoteFiles(input.connectionId, input.path)
  }),
  define({
    channel: IPCChannel.SftpListLocal,
    schema: sftpListLocalSchema,
    label: "本机文件列表",
    dispatch: (services, input) => services.sftp.listLocalFiles(input.path)
  }),
  define({
    channel: IPCChannel.SftpUpload,
    schema: sftpUploadSchema,
    label: "文件上传",
    dispatch: (services, input, event) =>
      services.sftp.uploadRemoteFile(
        input.connectionId,
        input.localPath,
        input.remotePath,
        event.sender,
        input.taskId
      )
  }),
  define({
    channel: IPCChannel.SftpUploadPacked,
    schema: sftpUploadPackedSchema,
    label: "打包上传",
    dispatch: (services, input, event) =>
      services.sftp.uploadRemotePacked(
        input.connectionId,
        input.localPaths,
        input.remoteDir,
        input.archiveName,
        event.sender,
        input.taskId
      )
  }),
  define({
    channel: IPCChannel.SftpDownload,
    schema: sftpDownloadSchema,
    label: "文件下载",
    dispatch: (services, input, event) =>
      services.sftp.downloadRemoteFile(
        input.connectionId,
        input.remotePath,
        input.localPath,
        event.sender,
        input.taskId
      )
  }),
  define({
    channel: IPCChannel.SftpTransferCancel,
    schema: sftpTransferCancelSchema,
    label: "取消传输",
    dispatch: (services, input) => services.sftp.cancelTransfer(input.taskId)
  }),
  define({
    channel: IPCChannel.SftpDownloadPacked,
    schema: sftpDownloadPackedSchema,
    label: "打包下载",
    dispatch: (services, input, event) =>
      services.sftp.downloadRemotePacked(
        input.connectionId,
        input.remoteDir,
        input.entryNames,
        input.localDir,
        input.archiveName,
        event.sender,
        input.taskId
      )
  }),
  define({
    channel: IPCChannel.SftpTransferPacked,
    schema: sftpTransferPackedSchema,
    label: "快传打包中转",
    dispatch: (services, input, event) =>
      services.sftp.transferRemotePacked(
        input.sourceConnectionId,
        input.sourceDir,
        input.entryNames,
        input.targetConnectionId,
        input.targetDir,
        input.archiveName,
        event.sender,
        input.taskId
      )
  }),
  define({
    channel: IPCChannel.SftpMkdir,
    schema: sftpMkdirSchema,
    label: "创建目录",
    dispatch: (services, input) =>
      services.sftp.createRemoteDirectory(input.connectionId, input.path)
  }),
  define({
    channel: IPCChannel.SftpRename,
    schema: sftpRenameSchema,
    label: "重命名",
    dispatch: (services, input) =>
      services.sftp.renameRemoteFile(input.connectionId, input.fromPath, input.toPath)
  }),
  define({
    channel: IPCChannel.SftpDelete,
    schema: sftpDeleteSchema,
    label: "文件删除",
    dispatch: (services, input) =>
      services.sftp.deleteRemoteFile(input.connectionId, input.path, input.type)
  }),

  // ─── Command History & Saved Commands ─────────────────────────────────────
  define({
    channel: IPCChannel.CommandHistoryList,
    schema: commandHistoryListSchema,
    label: "命令历史查询",
    coerceEmptyPayload: true,
    dispatch: (services) => services.commands.listCommandHistory()
  }),
  define({
    channel: IPCChannel.CommandHistoryPush,
    schema: commandHistoryPushSchema,
    label: "命令历史新增",
    dispatch: (services, input) => services.commands.pushCommandHistory(input.command)
  }),
  define({
    channel: IPCChannel.CommandHistoryRemove,
    schema: commandHistoryRemoveSchema,
    label: "命令历史删除",
    dispatch: (services, input) => services.commands.removeCommandHistory(input.command)
  }),
  define({
    channel: IPCChannel.CommandHistoryClear,
    schema: commandHistoryClearSchema,
    label: "命令历史清空",
    coerceEmptyPayload: true,
    dispatch: (services) => services.commands.clearCommandHistory()
  }),
  define({
    channel: IPCChannel.SavedCommandListScoped,
    schema: emptyObjectSchema,
    label: "命令库聚合列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.commands.listScopedSavedCommands()
  }),
  define({
    channel: IPCChannel.SavedCommandUpsert,
    schema: savedCommandUpsertSchema,
    label: "命令库保存",
    dispatch: (services, input) => services.commands.upsertSavedCommand(input)
  }),
  define({
    channel: IPCChannel.SavedCommandRemove,
    schema: savedCommandRemoveSchema,
    label: "命令库删除",
    dispatch: (services, input) => services.commands.removeSavedCommand(input)
  }),

  // ─── Remote Edit ──────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.SftpEditOpen,
    schema: sftpEditOpenSchema,
    label: "远端编辑",
    dispatch: (services, input, event) =>
      services.sftp.openRemoteEdit(
        input.connectionId,
        input.remotePath,
        input.editorCommand,
        event.sender
      )
  }),
  define({
    channel: IPCChannel.SftpEditStop,
    schema: sftpEditStopSchema,
    label: "停止编辑",
    dispatch: (services, input) => services.sftp.stopRemoteEdit(input.editId)
  }),
  define({
    channel: IPCChannel.SftpEditStopAll,
    schema: emptyObjectSchema,
    label: "停止所有编辑",
    coerceEmptyPayload: true,
    dispatch: (services) => services.sftp.stopAllRemoteEdits()
  }),
  define({
    channel: IPCChannel.SftpEditList,
    schema: emptyObjectSchema,
    label: "编辑列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.sftp.listRemoteEdits()
  }),
  define({
    channel: IPCChannel.SftpEditOpenBuiltin,
    schema: sftpEditOpenBuiltinSchema,
    label: "内置编辑打开",
    dispatch: (services, input, event) =>
      services.sftp.openBuiltinEdit(input.connectionId, input.remotePath, event.sender)
  }),
  define({
    channel: IPCChannel.SftpEditSaveBuiltin,
    schema: sftpEditSaveBuiltinSchema,
    label: "内置编辑保存",
    dispatch: (services, input) =>
      services.sftp.saveBuiltinEdit(
        input.editId,
        input.connectionId,
        input.remotePath,
        input.content
      )
  }),

  // ─── Process & Network Monitor ────────────────────────────────────────────
  define({
    channel: IPCChannel.MonitorProcessStart,
    schema: monitorProcessStartSchema,
    label: "进程监控启动",
    dispatch: (services, input, event) =>
      services.monitors.startProcessMonitor(input.connectionId, event.sender)
  }),
  define({
    channel: IPCChannel.MonitorProcessStop,
    schema: monitorProcessStopSchema,
    label: "进程监控停止",
    dispatch: (services, input) => services.monitors.stopProcessMonitor(input.connectionId)
  }),
  define({
    channel: IPCChannel.MonitorProcessDetail,
    schema: monitorProcessDetailSchema,
    label: "进程详情",
    dispatch: (services, input) => services.monitors.getProcessDetail(input.connectionId, input.pid)
  }),
  define({
    channel: IPCChannel.MonitorProcessKill,
    schema: monitorProcessKillSchema,
    label: "终止进程",
    dispatch: (services, input) =>
      services.monitors.killRemoteProcess(input.connectionId, input.pid, input.signal)
  }),
  define({
    channel: IPCChannel.MonitorNetworkStart,
    schema: monitorNetworkStartSchema,
    label: "网络监控启动",
    dispatch: (services, input, event) =>
      services.monitors.startNetworkMonitor(input.connectionId, event.sender)
  }),
  define({
    channel: IPCChannel.MonitorNetworkStop,
    schema: monitorNetworkStopSchema,
    label: "网络监控停止",
    dispatch: (services, input) => services.monitors.stopNetworkMonitor(input.connectionId)
  }),
  define({
    channel: IPCChannel.MonitorNetworkConnections,
    schema: monitorNetworkConnectionsSchema,
    label: "网络连接查询",
    dispatch: (services, input) =>
      services.monitors.getNetworkConnections(input.connectionId, input.port)
  }),

  // ─── Backup & Master Password ─────────────────────────────────────────────
  define({
    channel: IPCChannel.BackupList,
    schema: backupListSchema,
    label: "备份列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.backupPassword.backupList()
  }),
  define({
    channel: IPCChannel.BackupRun,
    schema: backupRunSchema,
    label: "执行备份",
    coerceEmptyPayload: true,
    dispatch: (services, input) => services.backupPassword.backupRun(input.conflictPolicy)
  }),
  define({
    channel: IPCChannel.BackupRestore,
    schema: backupRestoreSchema,
    label: "还原存档",
    dispatch: (services, input) =>
      services.backupPassword.backupRestore(input.archiveId, input.conflictPolicy)
  }),
  define({
    channel: IPCChannel.MasterPasswordSet,
    schema: masterPasswordSetSchema,
    label: "设置主密码",
    dispatch: (services, input) => services.backupPassword.masterPasswordSet(input.password)
  }),
  define({
    channel: IPCChannel.MasterPasswordUnlock,
    schema: masterPasswordUnlockSchema,
    label: "解锁主密码",
    dispatch: (services, input) => services.backupPassword.masterPasswordUnlock(input.password)
  }),
  define({
    channel: IPCChannel.MasterPasswordChange,
    schema: masterPasswordChangeSchema,
    label: "修改主密码",
    dispatch: (services, input) =>
      services.backupPassword.masterPasswordChange(input.oldPassword, input.newPassword)
  }),
  define({
    channel: IPCChannel.MasterPasswordClearRemembered,
    schema: masterPasswordClearRememberedSchema,
    label: "清除记住的主密码",
    coerceEmptyPayload: true,
    dispatch: (services) => services.backupPassword.masterPasswordClearRemembered()
  }),
  define({
    channel: IPCChannel.MasterPasswordStatus,
    schema: masterPasswordStatusSchema,
    label: "主密码状态查询",
    coerceEmptyPayload: true,
    dispatch: (services) => services.backupPassword.masterPasswordStatus()
  }),
  define({
    channel: IPCChannel.MasterPasswordGetCached,
    schema: masterPasswordGetCachedSchema,
    label: "获取主密码缓存",
    coerceEmptyPayload: true,
    dispatch: (services) => services.backupPassword.masterPasswordGetCached()
  }),

  // ─── Cloud Sync ───────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.CloudSyncWorkspaceList,
    schema: cloudSyncWorkspaceListSchema,
    label: "云同步工作区列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.cloudSync.listWorkspaces()
  }),
  define({
    channel: IPCChannel.CloudSyncWorkspaceAdd,
    schema: cloudSyncWorkspaceAddSchema,
    label: "云同步添加工作区",
    dispatch: (services, input) => services.cloudSync.addWorkspace(input)
  }),
  define({
    channel: IPCChannel.CloudSyncWorkspaceUpdate,
    schema: cloudSyncWorkspaceUpdateSchema,
    label: "云同步更新工作区",
    dispatch: (services, input) => services.cloudSync.updateWorkspace({ ...input, id: input.id })
  }),
  define({
    channel: IPCChannel.CloudSyncWorkspaceRemove,
    schema: cloudSyncWorkspaceRemoveSchema,
    label: "云同步删除工作区",
    dispatch: async (services, input) => {
      await services.cloudSync.removeWorkspace(input.id);
      return { ok: true as const };
    }
  }),
  define({
    channel: IPCChannel.CloudSyncWorkspaceExportToken,
    schema: cloudSyncWorkspaceExportTokenSchema,
    label: "云同步导出工作区 token",
    dispatch: (services, input) => services.cloudSync.exportWorkspaceToken(input.id)
  }),
  define({
    channel: IPCChannel.CloudSyncWorkspaceParseToken,
    schema: cloudSyncWorkspaceParseTokenSchema,
    label: "云同步解析工作区 token",
    dispatch: (services, input) => services.cloudSync.parseWorkspaceToken(input.token)
  }),
  define({
    channel: IPCChannel.CloudSyncStatus,
    schema: cloudSyncStatusSchema,
    label: "云同步状态",
    coerceEmptyPayload: true,
    dispatch: (services) => services.cloudSync.getStatus()
  }),
  define({
    channel: IPCChannel.CloudSyncSyncNow,
    schema: cloudSyncSyncNowSchema,
    label: "云同步立即同步",
    coerceEmptyPayload: true,
    dispatch: async (services, input) => {
      await services.cloudSync.syncNow(input.workspaceId);
      return { ok: true as const };
    }
  }),
  define({
    channel: IPCChannel.CloudSyncListConflicts,
    schema: cloudSyncListConflictsSchema,
    label: "云同步冲突列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.cloudSync.listConflicts()
  }),
  define({
    channel: IPCChannel.CloudSyncTestConnection,
    schema: cloudSyncTestConnectionSchema,
    label: "云同步连接测试",
    dispatch: (services, input) => services.cloudSync.testConnection(input)
  }),
  define({
    channel: IPCChannel.CloudSyncResolveConflict,
    schema: cloudSyncResolveConflictSchema,
    label: "云同步冲突处理",
    dispatch: async (services, input) => {
      await services.cloudSync.resolveConflict(
        input.workspaceId,
        input.resourceType,
        input.resourceId,
        input.strategy
      );
      return { ok: true as const };
    }
  }),

  // ─── SSH Keys ─────────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.SshKeyList,
    schema: sshKeyListSchema,
    label: "密钥列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.connections.listSshKeys()
  }),
  define({
    channel: IPCChannel.SshKeyUpsert,
    schema: sshKeyUpsertSchema,
    label: "密钥保存",
    dispatch: (services, input) => services.connections.upsertSshKey(input)
  }),
  define({
    channel: IPCChannel.SshKeyRemove,
    schema: sshKeyRemoveSchema,
    label: "密钥删除",
    // Recycle-bin deletion path (not ConnectionService.removeSshKey).
    dispatch: async (services, input) => {
      await services.resourceOps.deleteSshKey({ id: input.id, force: input.force });
      return { ok: true as const };
    }
  }),

  // ─── Proxies ──────────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.ProxyList,
    schema: proxyListSchema,
    label: "代理列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.connections.listProxies()
  }),
  define({
    channel: IPCChannel.ProxyUpsert,
    schema: proxyUpsertSchema,
    label: "代理保存",
    dispatch: (services, input) => services.connections.upsertProxy(input)
  }),
  define({
    channel: IPCChannel.ProxyRemove,
    schema: proxyRemoveSchema,
    label: "代理删除",
    dispatch: (services, input) => services.connections.removeProxy(input)
  }),

  // ─── Network Tools ────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.UpdateCheck,
    schema: updateCheckSchema,
    label: "检查更新",
    coerceEmptyPayload: true,
    dispatch: (services) => services.networkTools.checkForUpdate()
  }),
  define({
    channel: IPCChannel.Ping,
    schema: pingRequestSchema,
    label: "Ping",
    dispatch: (services, input) => services.networkTools.pingHost(input.host)
  }),
  define({
    channel: IPCChannel.TracerouteRun,
    schema: tracerouteRunSchema,
    label: "路由追踪",
    dispatch: (services, input, event) =>
      services.networkTools.tracerouteRun(input.host, event.sender)
  }),
  define({
    channel: IPCChannel.TracerouteStop,
    schema: null,
    label: "路由追踪停止",
    dispatch: (services) => services.networkTools.tracerouteStop()
  }),

  // ─── Debug Log ────────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.DebugLogEnable,
    schema: null,
    label: "调试日志开启",
    dispatch: (services, _input, event) => services.preferences.enableDebugLog(event.sender)
  }),
  define({
    channel: IPCChannel.DebugLogDisable,
    schema: null,
    label: "调试日志关闭",
    dispatch: (services, _input, event) => services.preferences.disableDebugLog(event.sender)
  }),

  // ─── Resource Operations ──────────────────────────────────────────────────
  define({
    channel: IPCChannel.ResourceCopyConnection,
    schema: resourceCopyConnectionSchema,
    label: "复制连接",
    dispatch: (services, input) => services.resourceOps.copyConnection(input)
  }),

  // ─── Recycle Bin ──────────────────────────────────────────────────────────
  define({
    channel: IPCChannel.RecycleBinList,
    schema: recycleBinListSchema,
    label: "回收站列表",
    coerceEmptyPayload: true,
    dispatch: (services) => services.recycleBinList()
  }),
  define({
    channel: IPCChannel.RecycleBinRestore,
    schema: recycleBinRestoreSchema,
    label: "回收站恢复",
    dispatch: (services, input) => services.resourceOps.restoreFromRecycleBin(input)
  }),
  define({
    channel: IPCChannel.RecycleBinPurge,
    schema: recycleBinPurgeSchema,
    label: "回收站永久删除",
    dispatch: (services, input) => {
      services.resourceOps.purgeRecycleBinEntry(input.id);
      return { ok: true as const };
    }
  }),
  define({
    channel: IPCChannel.RecycleBinClear,
    schema: recycleBinClearSchema,
    label: "清空回收站",
    coerceEmptyPayload: true,
    dispatch: (services) => services.recycleBinClear()
  })
];
