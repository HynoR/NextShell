import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  RemoteFileEntry
} from "../../../../../packages/core/src/index";
import {
  SshConnection,
  type RemotePathType,
  type SshDirectoryEntry
} from "../../../../../packages/ssh/src/index";
import type {
  SftpTransferStatusEvent,
  SftpEditSessionInfo
} from "../../../../../packages/shared/src/index";
import type { RemoteEditManager } from "./remote-edit-manager";
import {
  assertLocalTarAvailable,
  buildRemoteRemoveFileCommand,
  buildRemoteTarCheckCommand,
  buildRemoteTarCreateCommand,
  buildRemoteTarExtractCommand,
  createLocalTarGzArchive,
  normalizeArchiveName,
  normalizeRemoteEntryNames
} from "./sftp-archive-utils";
import {
  mapEntryType,
  parseLongname,
  joinRemotePath,
  normalizeError,
  resolveLocalPath,
  SFTP_WARMUP_TIMEOUT_MS
} from "./container-utils";
import { logger } from "../logger";

export interface SftpServiceOptions {
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  ensureConnection: (connectionId: string) => Promise<SshConnection>;
  remoteEditManager: RemoteEditManager;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  sendTransferStatus: (
    sender: WebContents | undefined,
    payload: SftpTransferStatusEvent
  ) => void;
}

export class SftpService {
  private readonly getConnectionOrThrow: SftpServiceOptions["getConnectionOrThrow"];
  private readonly ensureConnection: SftpServiceOptions["ensureConnection"];
  private readonly remoteEditManager: RemoteEditManager;
  private readonly appendAuditLogIfEnabled: SftpServiceOptions["appendAuditLogIfEnabled"];
  private readonly sendTransferStatus: SftpServiceOptions["sendTransferStatus"];

  constructor(options: SftpServiceOptions) {
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.ensureConnection = options.ensureConnection;
    this.remoteEditManager = options.remoteEditManager;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
    this.sendTransferStatus = options.sendTransferStatus;
  }

  // ─── Public Methods ───────────────────────────────────────────────────────

  async warmupSftp(
    connectionId: string,
    connection: SshConnection
  ): Promise<string | undefined> {
    let timeout: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        connection.list("."),
        new Promise<never>((_resolve, reject) => {
          timeout = setTimeout(() => {
            reject(new Error(`SFTP warmup timed out after ${SFTP_WARMUP_TIMEOUT_MS}ms`));
          }, SFTP_WARMUP_TIMEOUT_MS);
        })
      ]);

      this.appendAuditLogIfEnabled({
        action: "sftp.init_ready",
        level: "info",
        connectionId,
        message: "SFTP warmup completed after SSH session open"
      });
      return undefined;
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[SFTP] warmup failed", { connectionId, reason });
      this.appendAuditLogIfEnabled({
        action: "sftp.init_failed",
        level: "warn",
        connectionId,
        message: "SFTP warmup failed after SSH session open",
        metadata: { reason }
      });
      return `SSH 已连接，但 SFTP 初始化失败：${reason}`;
    } finally {
      if (timeout) {
        clearTimeout(timeout);
      }
    }
  }

  async listRemoteFiles(
    connectionId: string,
    pathName: string
  ): Promise<RemoteFileEntry[]> {
    this.getConnectionOrThrow(connectionId);

    const connection = await this.ensureConnection(connectionId);
    const rows = await connection.list(pathName);

    return rows
      .filter((entry) => entry.name !== "." && entry.name !== "..")
      .map((entry: SshDirectoryEntry) => {
        const parsed = parseLongname(entry.longname);
        const modifiedAt = entry.mtime
          ? new Date(entry.mtime * 1000).toISOString()
          : new Date().toISOString();

        return {
          name: entry.name,
          path: joinRemotePath(pathName, entry.name),
          type: mapEntryType(parsed.permissions),
          size: entry.size,
          permissions: parsed.permissions,
          owner: parsed.owner,
          group: parsed.group,
          modifiedAt
        };
      });
  }

  async listLocalFiles(pathName: string): Promise<RemoteFileEntry[]> {
    const resolvedPath = resolveLocalPath(pathName);
    let rows: fs.Dirent[];
    try {
      rows = await fs.promises.readdir(resolvedPath, { withFileTypes: true });
    } catch (error) {
      throw new Error(`读取本机目录失败：${normalizeError(error)}`);
    }

    const entries = await Promise.all(
      rows
        .filter((entry) => entry.name !== "." && entry.name !== "..")
        .map(async (entry) => {
          const fullPath = path.join(resolvedPath, entry.name);
          const stats = await fs.promises.lstat(fullPath);
          const type: RemoteFileEntry["type"] = entry.isDirectory()
            ? "directory"
            : entry.isSymbolicLink()
              ? "link"
              : "file";

          return {
            name: entry.name,
            path: fullPath,
            type,
            size: stats.size,
            permissions: (stats.mode & 0o777).toString(8).padStart(3, "0"),
            owner: typeof stats.uid === "number" ? String(stats.uid) : "-",
            group: typeof stats.gid === "number" ? String(stats.gid) : "-",
            modifiedAt: stats.mtime.toISOString()
          } satisfies RemoteFileEntry;
        })
    );

    return entries.sort((a, b) => {
      if (a.type === "directory" && b.type !== "directory") return -1;
      if (a.type !== "directory" && b.type === "directory") return 1;
      return a.name.localeCompare(b.name);
    });
  }

  async uploadRemoteFile(
    connectionId: string,
    localPath: string,
    remotePath: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    this.sendTransferStatus(sender, {
      taskId,
      direction: "upload",
      connectionId,
      localPath,
      remotePath,
      status: "running",
      progress: 5
    });
    const connection = await this.ensureConnection(connectionId);
    try {
      await connection.upload(localPath, remotePath);
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath,
        remotePath,
        status: "success",
        progress: 100
      });
      this.appendAuditLogIfEnabled({
        action: "sftp.upload",
        level: "info",
        connectionId,
        message: "Uploaded file to remote host",
        metadata: { localPath, remotePath }
      });
      return { ok: true };
    } catch (error) {
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath,
        remotePath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    }
  }

  async downloadRemoteFile(
    connectionId: string,
    remotePath: string,
    localPath: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    this.sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId,
      localPath,
      remotePath,
      status: "running",
      progress: 5
    });
    const connection = await this.ensureConnection(connectionId);
    try {
      await connection.download(remotePath, localPath);
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath,
        remotePath,
        status: "success",
        progress: 100
      });
      this.appendAuditLogIfEnabled({
        action: "sftp.download",
        level: "info",
        connectionId,
        message: "Downloaded file from remote host",
        metadata: { remotePath, localPath }
      });
      return { ok: true };
    } catch (error) {
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath,
        remotePath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    }
  }

  async uploadRemotePacked(
    connectionId: string,
    localPaths: string[],
    remoteDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    const resolvedLocalPaths = localPaths.map((localPath) => resolveLocalPath(localPath));
    const defaultArchiveBase = resolvedLocalPaths.length === 1
      ? path.basename(resolvedLocalPaths[0]!)
      : `upload-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const normalizedRemoteDir = remoteDir.trim() || "/";
    const remoteDisplayPath = joinRemotePath(normalizedRemoteDir, finalArchiveName);
    const localDisplayPath = resolvedLocalPaths.length === 1
      ? resolvedLocalPaths[0]!
      : `${resolvedLocalPaths[0] ?? ""} (+${resolvedLocalPaths.length - 1} files)`;
    const localArchivePath = path.join(os.tmpdir(), `nextshell-upload-${randomUUID()}-${finalArchiveName}`);
    const remoteArchivePath = `/tmp/nextshell-upload-${randomUUID()}.tar.gz`;
    let localArchiveCreated = false;
    let remoteArchiveCleaned = false;
    let connection: SshConnection | undefined;

    const cleanupRemoteArchive = async (): Promise<void> => {
      if (!connection || remoteArchiveCleaned) {
        return;
      }

      try {
        await connection.exec(buildRemoteRemoveFileCommand(remoteArchivePath));
        remoteArchiveCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Upload] failed to cleanup remote archive", {
          connectionId,
          remoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    this.sendTransferStatus(sender, {
      taskId,
      direction: "upload",
      connectionId,
      localPath: localDisplayPath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始打包上传"
    });

    try {
      await assertLocalTarAvailable();
      connection = await this.ensureConnection(connectionId);
      await this.ensureRemoteTarAvailable(connection, "打包上传");
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "tar 环境检查通过"
      });

      await createLocalTarGzArchive(resolvedLocalPaths, localArchivePath);
      localArchiveCreated = true;
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "本地打包完成"
      });

      await connection.upload(localArchivePath, remoteArchivePath);
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 75,
        message: "压缩包上传完成"
      });

      const extractResult = await connection.exec(
        buildRemoteTarExtractCommand(remoteArchivePath, normalizedRemoteDir)
      );
      if (extractResult.exitCode !== 0) {
        throw new Error(`远端解包失败：${this.pickRemoteCommandError(
          extractResult.stdout,
          extractResult.stderr,
          extractResult.exitCode
        )}`);
      }

      await cleanupRemoteArchive();
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "远端解包完成"
      });

      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      this.appendAuditLogIfEnabled({
        action: "sftp.upload.packed",
        level: "info",
        connectionId,
        message: "Uploaded packed files to remote host",
        metadata: {
          localPaths: resolvedLocalPaths,
          remoteDir: normalizedRemoteDir,
          archiveName: finalArchiveName
        }
      });
      return { ok: true };
    } catch (error) {
      this.sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupRemoteArchive();
      if (localArchiveCreated || fs.existsSync(localArchivePath)) {
        try {
          fs.rmSync(localArchivePath, { force: true });
        } catch (error) {
          logger.warn("[SFTP Packed Upload] failed to cleanup local archive", {
            localArchivePath,
            reason: normalizeError(error)
          });
        }
      }
    }
  }

  async downloadRemotePacked(
    connectionId: string,
    remoteDir: string,
    entryNames: string[],
    localDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true; localArchivePath: string }> {
    this.getConnectionOrThrow(connectionId);
    const normalizedRemoteDir = remoteDir.trim() || "/";
    const normalizedEntryNames = normalizeRemoteEntryNames(entryNames);
    const defaultArchiveBase = normalizedEntryNames.length === 1
      ? normalizedEntryNames[0]!
      : `download-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const resolvedLocalDir = resolveLocalPath(localDir);
    const localArchivePath = path.join(resolvedLocalDir, finalArchiveName);
    const remoteArchivePath = `/tmp/nextshell-download-${randomUUID()}.tar.gz`;
    const remoteDisplayPath = joinRemotePath(normalizedRemoteDir, finalArchiveName);
    let connection: SshConnection | undefined;
    let remoteArchiveCleaned = false;

    const cleanupRemoteArchive = async (): Promise<void> => {
      if (!connection || remoteArchiveCleaned) {
        return;
      }

      try {
        await connection.exec(buildRemoteRemoveFileCommand(remoteArchivePath));
        remoteArchiveCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Download] failed to cleanup remote archive", {
          connectionId,
          remoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    this.sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId,
      localPath: localArchivePath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始打包下载"
    });

    try {
      connection = await this.ensureConnection(connectionId);
      await this.ensureRemoteTarAvailable(connection, "打包下载");
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "远端 tar 环境检查通过"
      });

      const packResult = await connection.exec(
        buildRemoteTarCreateCommand(normalizedRemoteDir, remoteArchivePath, normalizedEntryNames)
      );
      if (packResult.exitCode !== 0) {
        throw new Error(`远端打包失败：${this.pickRemoteCommandError(
          packResult.stdout,
          packResult.stderr,
          packResult.exitCode
        )}`);
      }
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "远端打包完成"
      });

      fs.mkdirSync(path.dirname(localArchivePath), { recursive: true });
      await connection.download(remoteArchivePath, localArchivePath);
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 75,
        message: "压缩包下载完成"
      });

      await cleanupRemoteArchive();
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "远端临时文件已清理"
      });

      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      this.appendAuditLogIfEnabled({
        action: "sftp.download.packed",
        level: "info",
        connectionId,
        message: "Downloaded packed files from remote host",
        metadata: {
          remoteDir: normalizedRemoteDir,
          entryNames: normalizedEntryNames,
          localArchivePath
        }
      });
      return { ok: true, localArchivePath };
    } catch (error) {
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupRemoteArchive();
    }
  }

  async transferRemotePacked(
    sourceConnectionId: string,
    sourceDir: string,
    entryNames: string[],
    targetConnectionId: string,
    targetDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(sourceConnectionId);
    this.getConnectionOrThrow(targetConnectionId);

    const normalizedSourceDir = sourceDir.trim() || "/";
    const normalizedTargetDir = targetDir.trim() || "/";
    const normalizedEntryNames = normalizeRemoteEntryNames(entryNames);
    const defaultArchiveBase = normalizedEntryNames.length === 1
      ? normalizedEntryNames[0]!
      : `transfer-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const sourceRemoteArchivePath = `/tmp/nextshell-transfer-src-${randomUUID()}.tar.gz`;
    const targetRemoteArchivePath = `/tmp/nextshell-transfer-target-${randomUUID()}.tar.gz`;
    const localArchivePath = path.join(
      os.tmpdir(),
      `nextshell-transfer-${randomUUID()}-${finalArchiveName}`
    );
    const remoteDisplayPath = `${targetConnectionId}:${joinRemotePath(normalizedTargetDir, finalArchiveName)}`;
    const localDisplayPath = `${sourceConnectionId}:${joinRemotePath(normalizedSourceDir, finalArchiveName)}`;
    let sourceConnection: SshConnection | undefined;
    let targetConnection: SshConnection | undefined;
    let sourceRemoteCleaned = false;
    let targetRemoteCleaned = false;
    let localArchiveCreated = false;

    const cleanupSourceRemoteArchive = async (): Promise<void> => {
      if (!sourceConnection || sourceRemoteCleaned) {
        return;
      }

      try {
        await sourceConnection.exec(buildRemoteRemoveFileCommand(sourceRemoteArchivePath));
        sourceRemoteCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Transfer] failed to cleanup source remote archive", {
          sourceConnectionId,
          sourceRemoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    const cleanupTargetRemoteArchive = async (): Promise<void> => {
      if (!targetConnection || targetRemoteCleaned) {
        return;
      }

      try {
        await targetConnection.exec(buildRemoteRemoveFileCommand(targetRemoteArchivePath));
        targetRemoteCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Transfer] failed to cleanup target remote archive", {
          targetConnectionId,
          targetRemoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    this.sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId: sourceConnectionId,
      localPath: localDisplayPath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始跨服务器快传"
    });

    try {
      sourceConnection = await this.ensureConnection(sourceConnectionId);
      targetConnection = await this.ensureConnection(targetConnectionId);
      await this.ensureRemoteTarAvailable(sourceConnection, "跨服务器快传");
      await this.ensureRemoteTarAvailable(targetConnection, "跨服务器快传");
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "两端 tar 环境检查通过"
      });

      const packResult = await sourceConnection.exec(
        buildRemoteTarCreateCommand(
          normalizedSourceDir,
          sourceRemoteArchivePath,
          normalizedEntryNames
        )
      );
      if (packResult.exitCode !== 0) {
        throw new Error(`源服务器打包失败：${this.pickRemoteCommandError(
          packResult.stdout,
          packResult.stderr,
          packResult.exitCode
        )}`);
      }
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "源服务器打包完成"
      });

      fs.mkdirSync(path.dirname(localArchivePath), { recursive: true });
      await sourceConnection.download(sourceRemoteArchivePath, localArchivePath);
      localArchiveCreated = true;
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 65,
        message: "中转包已下载到本机"
      });

      await targetConnection.upload(localArchivePath, targetRemoteArchivePath);
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 80,
        message: "中转包已上传到目标服务器"
      });

      const extractResult = await targetConnection.exec(
        buildRemoteTarExtractCommand(targetRemoteArchivePath, normalizedTargetDir)
      );
      if (extractResult.exitCode !== 0) {
        throw new Error(`目标服务器解包失败：${this.pickRemoteCommandError(
          extractResult.stdout,
          extractResult.stderr,
          extractResult.exitCode
        )}`);
      }

      await cleanupSourceRemoteArchive();
      await cleanupTargetRemoteArchive();
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "目标服务器解包完成"
      });

      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      this.appendAuditLogIfEnabled({
        action: "sftp.transfer.packed",
        level: "info",
        connectionId: sourceConnectionId,
        message: "Transferred packed files between remote hosts",
        metadata: {
          sourceConnectionId,
          sourceDir: normalizedSourceDir,
          targetConnectionId,
          targetDir: normalizedTargetDir,
          entryNames: normalizedEntryNames
        }
      });
      return { ok: true };
    } catch (error) {
      this.sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupSourceRemoteArchive();
      await cleanupTargetRemoteArchive();
      if (localArchiveCreated || fs.existsSync(localArchivePath)) {
        try {
          fs.rmSync(localArchivePath, { force: true });
        } catch (error) {
          logger.warn("[SFTP Packed Transfer] failed to cleanup local archive", {
            localArchivePath,
            reason: normalizeError(error)
          });
        }
      }
    }
  }

  async createRemoteDirectory(
    connectionId: string,
    pathName: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId);
    await connection.mkdir(pathName, true);
    this.appendAuditLogIfEnabled({
      action: "sftp.mkdir",
      level: "info",
      connectionId,
      message: "Created remote directory",
      metadata: { pathName }
    });
    return { ok: true };
  }

  async renameRemoteFile(
    connectionId: string,
    fromPath: string,
    toPath: string
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId);
    await connection.rename(fromPath, toPath);
    this.appendAuditLogIfEnabled({
      action: "sftp.rename",
      level: "warn",
      connectionId,
      message: "Renamed remote path",
      metadata: { fromPath, toPath }
    });
    return { ok: true };
  }

  async deleteRemoteFile(
    connectionId: string,
    targetPath: string,
    type: RemoteFileEntry["type"]
  ): Promise<{ ok: true }> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId);

    const normalizedType: RemotePathType =
      type === "directory" ? "directory" : type === "link" ? "link" : "file";

    await connection.remove(targetPath, normalizedType);
    this.appendAuditLogIfEnabled({
      action: "sftp.delete",
      level: "warn",
      connectionId,
      message: "Deleted remote path",
      metadata: { targetPath, type: normalizedType }
    });
    return { ok: true };
  }

  async openRemoteEdit(
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ): Promise<{ editId: string; localPath: string }> {
    this.getConnectionOrThrow(connectionId);
    try {
      const result = await this.remoteEditManager.open(connectionId, remotePath, editorCommand, sender);
      this.appendAuditLogIfEnabled({
        action: "sftp.edit_open",
        level: "info",
        connectionId,
        message: "Opened remote file for live editing",
        metadata: { remotePath, editId: result.editId }
      });
      return result;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      const enriched = error as {
        source?: string;
        code?: string;
        requestedCommand?: string;
        resolvedCommand?: string;
      };
      this.appendAuditLogIfEnabled({
        action: "sftp.edit_open_failed",
        level: "error",
        connectionId,
        message: "Failed to open remote file for live editing",
        metadata: {
          remotePath,
          editorCommand,
          reason,
          commandSource: enriched.source,
          code: enriched.code,
          requestedCommand: enriched.requestedCommand,
          resolvedCommand: enriched.resolvedCommand
        }
      });
      throw error;
    }
  }

  async stopRemoteEdit(editId: string): Promise<{ ok: true }> {
    await this.remoteEditManager.stop(editId);
    this.appendAuditLogIfEnabled({
      action: "sftp.edit_stop",
      level: "info",
      message: "Stopped remote file live editing",
      metadata: { editId }
    });
    return { ok: true };
  }

  async stopAllRemoteEdits(): Promise<{ ok: true }> {
    await this.remoteEditManager.stopAll();
    this.appendAuditLogIfEnabled({
      action: "sftp.edit_stop_all",
      level: "info",
      message: "Stopped all remote file live editing sessions"
    });
    return { ok: true };
  }

  listRemoteEdits(): SftpEditSessionInfo[] {
    return this.remoteEditManager.listSessions();
  }

  async openBuiltinEdit(
    connectionId: string,
    remotePath: string,
    sender: WebContents
  ): Promise<{ editId: string; content: string }> {
    return this.remoteEditManager.openBuiltin(connectionId, remotePath, sender);
  }

  async saveBuiltinEdit(
    editId: string,
    connectionId: string,
    remotePath: string,
    content: string
  ): Promise<{ ok: true }> {
    await this.remoteEditManager.saveBuiltin(editId, connectionId, remotePath, content);
    return { ok: true };
  }

  // ─── Private Helpers ──────────────────────────────────────────────────────

  private async ensureRemoteTarAvailable(
    connection: SshConnection,
    actionLabel: string
  ): Promise<void> {
    const result = await connection.exec(buildRemoteTarCheckCommand());
    if (result.exitCode !== 0) {
      throw new Error(`${actionLabel}失败：远端缺少 tar/gzip 命令`);
    }
  }

  private pickRemoteCommandError(stdout: string, stderr: string, exitCode: number): string {
    return stderr.trim() || stdout.trim() || `exit ${exitCode}`;
  }
}
