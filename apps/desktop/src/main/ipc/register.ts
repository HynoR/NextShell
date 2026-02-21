import { ipcMain } from "electron";
import { ZodError, z } from "zod";
import { logger } from "../logger";
import {
  IPCChannel,
  auditListSchema,
  commandBatchExecSchema,
  commandExecSchema,
  commandHistoryClearSchema,
  commandHistoryListSchema,
  commandHistoryPushSchema,
  commandHistoryRemoveSchema,
  connectionExportSchema,
  connectionExportBatchSchema,
  connectionImportFinalShellPreviewSchema,
  connectionImportPreviewSchema,
  connectionImportExecuteSchema,
  connectionListQuerySchema,
  connectionRemoveSchema,
  connectionUpsertSchema,
  dialogOpenDirectorySchema,
  dialogOpenFilesSchema,
  dialogOpenPathSchema,
  monitorSnapshotSchema,
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
  sftpDeleteSchema,
  sftpDownloadSchema,
  sftpMkdirSchema,
  sessionOpenSchema,
  sessionResizeSchema,
  sessionWriteSchema,
  sftpListSchema,
  storageMigrationsSchema,
  sftpRenameSchema,
  sftpUploadSchema,
  savedCommandListSchema,
  savedCommandRemoveSchema,
  savedCommandUpsertSchema,
  sftpEditOpenSchema,
  sftpEditStopSchema,
  sftpEditOpenBuiltinSchema,
  sftpEditSaveBuiltinSchema,
  backupListSchema,
  backupRunSchema,
  backupRestoreSchema,
  backupPasswordSetSchema,
  backupPasswordUnlockSchema,
  backupPasswordClearRememberedSchema,
  backupPasswordStatusSchema,
  templateParamsListSchema,
  templateParamsUpsertSchema,
  templateParamsClearSchema,
  sshKeyListSchema,
  sshKeyUpsertSchema,
  sshKeyRemoveSchema,
  proxyListSchema,
  proxyUpsertSchema,
  proxyRemoveSchema,
  updateCheckSchema
} from "../../../../../packages/shared/src/index";
import type { ServiceContainer } from "../services/container";

const channels = Object.values(IPCChannel);

const formatValidationError = (error: ZodError): string => {
  return error.issues
    .map((issue) => {
      const path = issue.path.length > 0 ? issue.path.map(String).join(".") : "payload";
      return `${path}: ${issue.message}`;
    })
    .join("; ");
};

const parsePayload = <T>(
  schema: z.ZodType<T>,
  payload: unknown,
  actionLabel: string
): T => {
  try {
    return schema.parse(payload);
  } catch (error) {
    if (error instanceof ZodError) {
      logger.error(`[IPC Validation] ${actionLabel}`, error);
      throw new Error(`${actionLabel} 参数无效：${formatValidationError(error)}`);
    }

    throw error;
  }
};

export const registerIpcHandlers = (services: ServiceContainer): void => {
  for (const channel of channels) {
    ipcMain.removeHandler(channel);
  }

  ipcMain.handle(IPCChannel.ConnectionList, (_event, payload) => {
    const query = parsePayload(connectionListQuerySchema, payload ?? {}, "连接查询");
    return services.listConnections(query);
  });

  ipcMain.handle(IPCChannel.ConnectionUpsert, (_event, payload) => {
    const input = parsePayload(connectionUpsertSchema, payload, "连接保存");
    return services.upsertConnection(input);
  });

  ipcMain.handle(IPCChannel.ConnectionRemove, (_event, payload) => {
    const input = parsePayload(connectionRemoveSchema, payload, "连接删除");
    return services.removeConnection(input.id);
  });

  ipcMain.handle(IPCChannel.ConnectionExport, (event, payload) => {
    const input = parsePayload(connectionExportSchema, payload, "连接导出");
    return services.exportConnections(event.sender, input);
  });

  ipcMain.handle(IPCChannel.ConnectionExportBatch, (_event, payload) => {
    const input = parsePayload(connectionExportBatchSchema, payload, "连接批量导出");
    return services.exportConnectionsBatch(input);
  });

  ipcMain.handle(IPCChannel.ConnectionImportPreview, (_event, payload) => {
    const input = parsePayload(connectionImportPreviewSchema, payload, "连接导入预览");
    return services.importConnectionsPreview(input);
  });

  ipcMain.handle(IPCChannel.ConnectionImportFinalShellPreview, (_event, payload) => {
    const input = parsePayload(connectionImportFinalShellPreviewSchema, payload, "FinalShell 导入预览");
    return services.importFinalShellConnectionsPreview(input);
  });

  ipcMain.handle(IPCChannel.ConnectionImportExecute, (_event, payload) => {
    const input = parsePayload(connectionImportExecuteSchema, payload, "连接导入执行");
    return services.importConnectionsExecute(input);
  });

  ipcMain.handle(IPCChannel.SettingsGet, (_event, payload) => {
    parsePayload(settingsGetSchema, payload ?? {}, "设置读取");
    return services.getAppPreferences();
  });

  ipcMain.handle(IPCChannel.SettingsUpdate, (_event, payload) => {
    const input = parsePayload(settingsUpdateSchema, payload, "设置更新");
    return services.updateAppPreferences(input);
  });

  ipcMain.handle(IPCChannel.DialogOpenFiles, (event, payload) => {
    const input = parsePayload(dialogOpenFilesSchema, payload ?? {}, "打开文件选择器");
    return services.openFilesDialog(event.sender, input);
  });

  ipcMain.handle(IPCChannel.DialogOpenDirectory, (event, payload) => {
    const input = parsePayload(dialogOpenDirectorySchema, payload ?? {}, "打开目录选择器");
    return services.openDirectoryDialog(event.sender, input);
  });

  ipcMain.handle(IPCChannel.DialogOpenPath, (event, payload) => {
    const input = parsePayload(dialogOpenPathSchema, payload, "打开路径");
    return services.openLocalPath(event.sender, input);
  });

  ipcMain.handle(IPCChannel.SessionOpen, (event, payload) => {
    const input = parsePayload(sessionOpenSchema, payload, "会话打开");
    return services.openSession(input.connectionId, event.sender, input.sessionId, input.authOverride);
  });

  ipcMain.handle(IPCChannel.SessionWrite, (_event, payload) => {
    const input = parsePayload(sessionWriteSchema, payload, "会话写入");
    return services.writeSession(input.sessionId, input.data);
  });

  ipcMain.handle(IPCChannel.SessionResize, (_event, payload) => {
    const input = parsePayload(sessionResizeSchema, payload, "会话尺寸调整");
    return services.resizeSession(input.sessionId, input.cols, input.rows);
  });

  ipcMain.handle(IPCChannel.SessionClose, (_event, payload) => {
    const input = parsePayload(sessionCloseSchema, payload, "会话关闭");
    return services.closeSession(input.sessionId);
  });

  ipcMain.handle(IPCChannel.MonitorSnapshot, (_event, payload) => {
    const input = parsePayload(monitorSnapshotSchema, payload, "监控快照");
    return services.getMonitorSnapshot(input.connectionId);
  });

  ipcMain.handle(IPCChannel.MonitorSystemInfoSnapshot, (_event, payload) => {
    const input = parsePayload(monitorSystemInfoSnapshotSchema, payload, "系统信息快照");
    return services.getSystemInfoSnapshot(input.connectionId);
  });

  ipcMain.handle(IPCChannel.MonitorSystemStart, (event, payload) => {
    const input = parsePayload(monitorSystemStartSchema, payload, "系统监控启动");
    return services.startSystemMonitor(input.connectionId, event.sender);
  });

  ipcMain.handle(IPCChannel.MonitorSystemStop, (_event, payload) => {
    const input = parsePayload(monitorSystemStopSchema, payload, "系统监控停止");
    return services.stopSystemMonitor(input.connectionId);
  });

  ipcMain.handle(IPCChannel.MonitorSystemSelectInterface, (_event, payload) => {
    const input = parsePayload(monitorSystemSelectInterfaceSchema, payload, "系统监控网卡切换");
    return services.selectSystemNetworkInterface(input.connectionId, input.networkInterface);
  });

  ipcMain.handle(IPCChannel.CommandExec, (_event, payload) => {
    const input = parsePayload(commandExecSchema, payload, "命令执行");
    return services.execCommand(input.connectionId, input.command);
  });

  ipcMain.handle(IPCChannel.CommandBatchExec, (_event, payload) => {
    const input = parsePayload(commandBatchExecSchema, payload, "批量命令执行");
    return services.execBatchCommand(input);
  });

  ipcMain.handle(IPCChannel.AuditList, (_event, payload) => {
    const input = parsePayload(auditListSchema, payload ?? {}, "审计日志查询");
    return services.listAuditLogs(input.limit);
  });

  ipcMain.handle(IPCChannel.StorageMigrations, (_event, payload) => {
    parsePayload(storageMigrationsSchema, payload ?? {}, "迁移记录查询");
    return services.listMigrations();
  });

  ipcMain.handle(IPCChannel.SftpList, (_event, payload) => {
    const input = parsePayload(sftpListSchema, payload, "文件列表");
    return services.listRemoteFiles(input.connectionId, input.path);
  });

  ipcMain.handle(IPCChannel.SftpUpload, (event, payload) => {
    const input = parsePayload(sftpUploadSchema, payload, "文件上传");
    return services.uploadRemoteFile(
      input.connectionId,
      input.localPath,
      input.remotePath,
      event.sender,
      input.taskId
    );
  });

  ipcMain.handle(IPCChannel.SftpDownload, (event, payload) => {
    const input = parsePayload(sftpDownloadSchema, payload, "文件下载");
    return services.downloadRemoteFile(
      input.connectionId,
      input.remotePath,
      input.localPath,
      event.sender,
      input.taskId
    );
  });

  ipcMain.handle(IPCChannel.SftpMkdir, (_event, payload) => {
    const input = parsePayload(sftpMkdirSchema, payload, "创建目录");
    return services.createRemoteDirectory(input.connectionId, input.path);
  });

  ipcMain.handle(IPCChannel.SftpRename, (_event, payload) => {
    const input = parsePayload(sftpRenameSchema, payload, "重命名");
    return services.renameRemoteFile(input.connectionId, input.fromPath, input.toPath);
  });

  ipcMain.handle(IPCChannel.SftpDelete, (_event, payload) => {
    const input = parsePayload(sftpDeleteSchema, payload, "文件删除");
    return services.deleteRemoteFile(input.connectionId, input.path, input.type);
  });

  ipcMain.handle(IPCChannel.CommandHistoryList, (_event, payload) => {
    parsePayload(commandHistoryListSchema, payload ?? {}, "命令历史查询");
    return services.listCommandHistory();
  });

  ipcMain.handle(IPCChannel.CommandHistoryPush, (_event, payload) => {
    const input = parsePayload(commandHistoryPushSchema, payload, "命令历史新增");
    return services.pushCommandHistory(input.command);
  });

  ipcMain.handle(IPCChannel.CommandHistoryRemove, (_event, payload) => {
    const input = parsePayload(commandHistoryRemoveSchema, payload, "命令历史删除");
    return services.removeCommandHistory(input.command);
  });

  ipcMain.handle(IPCChannel.CommandHistoryClear, (_event, payload) => {
    parsePayload(commandHistoryClearSchema, payload ?? {}, "命令历史清空");
    return services.clearCommandHistory();
  });

  ipcMain.handle(IPCChannel.SavedCommandList, (_event, payload) => {
    const input = parsePayload(savedCommandListSchema, payload ?? {}, "命令库列表");
    return services.listSavedCommands(input);
  });

  ipcMain.handle(IPCChannel.SavedCommandUpsert, (_event, payload) => {
    const input = parsePayload(savedCommandUpsertSchema, payload, "命令库保存");
    return services.upsertSavedCommand(input);
  });

  ipcMain.handle(IPCChannel.SavedCommandRemove, (_event, payload) => {
    const input = parsePayload(savedCommandRemoveSchema, payload, "命令库删除");
    return services.removeSavedCommand(input);
  });

  ipcMain.handle(IPCChannel.SftpEditOpen, (event, payload) => {
    const input = parsePayload(sftpEditOpenSchema, payload, "远端编辑");
    return services.openRemoteEdit(input.connectionId, input.remotePath, input.editorCommand, event.sender);
  });

  ipcMain.handle(IPCChannel.SftpEditStop, (_event, payload) => {
    const input = parsePayload(sftpEditStopSchema, payload, "停止编辑");
    return services.stopRemoteEdit(input.editId);
  });

  ipcMain.handle(IPCChannel.SftpEditStopAll, (_event, payload) => {
    parsePayload(z.object({}), payload ?? {}, "停止所有编辑");
    return services.stopAllRemoteEdits();
  });

  ipcMain.handle(IPCChannel.SftpEditList, (_event, payload) => {
    parsePayload(z.object({}), payload ?? {}, "编辑列表");
    return services.listRemoteEdits();
  });

  ipcMain.handle(IPCChannel.SftpEditOpenBuiltin, (event, payload) => {
    const input = parsePayload(sftpEditOpenBuiltinSchema, payload, "内置编辑打开");
    return services.openBuiltinEdit(input.connectionId, input.remotePath, event.sender);
  });

  ipcMain.handle(IPCChannel.SftpEditSaveBuiltin, (_event, payload) => {
    const input = parsePayload(sftpEditSaveBuiltinSchema, payload, "内置编辑保存");
    return services.saveBuiltinEdit(input.editId, input.connectionId, input.remotePath, input.content);
  });

  ipcMain.handle(IPCChannel.MonitorProcessStart, (event, payload) => {
    const input = parsePayload(monitorProcessStartSchema, payload, "进程监控启动");
    return services.startProcessMonitor(input.connectionId, event.sender);
  });

  ipcMain.handle(IPCChannel.MonitorProcessStop, (_event, payload) => {
    const input = parsePayload(monitorProcessStopSchema, payload, "进程监控停止");
    return services.stopProcessMonitor(input.connectionId);
  });

  ipcMain.handle(IPCChannel.MonitorProcessDetail, (_event, payload) => {
    const input = parsePayload(monitorProcessDetailSchema, payload, "进程详情");
    return services.getProcessDetail(input.connectionId, input.pid);
  });

  ipcMain.handle(IPCChannel.MonitorProcessKill, (_event, payload) => {
    const input = parsePayload(monitorProcessKillSchema, payload, "终止进程");
    return services.killRemoteProcess(input.connectionId, input.pid, input.signal);
  });

  ipcMain.handle(IPCChannel.MonitorNetworkStart, (event, payload) => {
    const input = parsePayload(monitorNetworkStartSchema, payload, "网络监控启动");
    return services.startNetworkMonitor(input.connectionId, event.sender);
  });

  ipcMain.handle(IPCChannel.MonitorNetworkStop, (_event, payload) => {
    const input = parsePayload(monitorNetworkStopSchema, payload, "网络监控停止");
    return services.stopNetworkMonitor(input.connectionId);
  });

  ipcMain.handle(IPCChannel.MonitorNetworkConnections, (_event, payload) => {
    const input = parsePayload(monitorNetworkConnectionsSchema, payload, "网络连接查询");
    return services.getNetworkConnections(input.connectionId, input.port);
  });

  // ─── Backup & Password ────────────────────────────────────────────────────

  ipcMain.handle(IPCChannel.BackupList, (_event, payload) => {
    parsePayload(backupListSchema, payload ?? {}, "备份列表");
    return services.backupList();
  });

  ipcMain.handle(IPCChannel.BackupRun, (_event, payload) => {
    const input = parsePayload(backupRunSchema, payload ?? {}, "执行备份");
    return services.backupRun(input.conflictPolicy);
  });

  ipcMain.handle(IPCChannel.BackupRestore, (_event, payload) => {
    const input = parsePayload(backupRestoreSchema, payload, "还原存档");
    return services.backupRestore(input.archiveId, input.conflictPolicy);
  });

  ipcMain.handle(IPCChannel.BackupPasswordSet, (_event, payload) => {
    const input = parsePayload(backupPasswordSetSchema, payload, "设置云存档密码");
    return services.backupSetPassword(input.password);
  });

  ipcMain.handle(IPCChannel.BackupPasswordUnlock, (_event, payload) => {
    const input = parsePayload(backupPasswordUnlockSchema, payload, "解锁云存档密码");
    return services.backupUnlockPassword(input.password);
  });

  ipcMain.handle(IPCChannel.BackupPasswordClearRemembered, (_event, payload) => {
    parsePayload(backupPasswordClearRememberedSchema, payload ?? {}, "清除记住的密码");
    return services.backupClearRemembered();
  });

  ipcMain.handle(IPCChannel.BackupPasswordStatus, (_event, payload) => {
    parsePayload(backupPasswordStatusSchema, payload ?? {}, "密码状态查询");
    return services.backupPasswordStatus();
  });

  // ─── Template Params ──────────────────────────────────────────────────────

  ipcMain.handle(IPCChannel.TemplateParamsList, (_event, payload) => {
    const input = parsePayload(templateParamsListSchema, payload ?? {}, "模板参数列表");
    return services.listTemplateParams(input);
  });

  ipcMain.handle(IPCChannel.TemplateParamsUpsert, (_event, payload) => {
    const input = parsePayload(templateParamsUpsertSchema, payload, "模板参数保存");
    return services.upsertTemplateParams(input);
  });

  ipcMain.handle(IPCChannel.TemplateParamsClear, (_event, payload) => {
    const input = parsePayload(templateParamsClearSchema, payload, "模板参数清除");
    return services.clearTemplateParams(input);
  });

  // ─── SSH Keys ─────────────────────────────────────────────────────────────

  ipcMain.handle(IPCChannel.SshKeyList, (_event, payload) => {
    parsePayload(sshKeyListSchema, payload ?? {}, "密钥列表");
    return services.listSshKeys();
  });

  ipcMain.handle(IPCChannel.SshKeyUpsert, (_event, payload) => {
    const input = parsePayload(sshKeyUpsertSchema, payload, "密钥保存");
    return services.upsertSshKey(input);
  });

  ipcMain.handle(IPCChannel.SshKeyRemove, (_event, payload) => {
    const input = parsePayload(sshKeyRemoveSchema, payload, "密钥删除");
    return services.removeSshKey(input);
  });

  // ─── Proxies ──────────────────────────────────────────────────────────────

  ipcMain.handle(IPCChannel.ProxyList, (_event, payload) => {
    parsePayload(proxyListSchema, payload ?? {}, "代理列表");
    return services.listProxies();
  });

  ipcMain.handle(IPCChannel.ProxyUpsert, (_event, payload) => {
    const input = parsePayload(proxyUpsertSchema, payload, "代理保存");
    return services.upsertProxy(input);
  });

  ipcMain.handle(IPCChannel.ProxyRemove, (_event, payload) => {
    const input = parsePayload(proxyRemoveSchema, payload, "代理删除");
    return services.removeProxy(input);
  });

  // ─── Update Check ─────────────────────────────────────────────────────────

  ipcMain.handle(IPCChannel.UpdateCheck, async (_event, payload) => {
    parsePayload(updateCheckSchema, payload ?? {}, "检查更新");
    return services.checkForUpdate();
  });
};
