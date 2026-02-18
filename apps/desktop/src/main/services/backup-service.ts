import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { BackupArchiveMeta, BackupConflictPolicy, RestoreConflictPolicy } from "../../../../../packages/core/src/index";
import { encryptBackupPayload, decryptBackupPayload } from "../../../../../packages/security/src/index";
import type { ConnectionRepository } from "../../../../../packages/storage/src/index";
import { logger } from "../logger";

const execFileAsync = promisify(execFile);

const APP_VERSION = "0.1.0";
const NSBK_EXTENSION = ".nsbk";
const META_EXTENSION = ".meta.json";
const SNAPSHOT_FILENAME = "nextshell.snapshot.db";

interface ManifestJson {
  appVersion: string;
  timestamp: string;
  deviceId: string;
  dbHash: string;
}

const getDeviceId = (): string => {
  const hostname = os.hostname();
  const platform = os.platform();
  return `${platform}-${hostname}`;
};

const sha256File = (filePath: string): string => {
  const data = fs.readFileSync(filePath);
  return createHash("sha256").update(data).digest("hex");
};

const sha256Buffer = (data: Buffer): string => {
  return createHash("sha256").update(data).digest("hex");
};

/** Locate rclone binary via PATH (macOS / Linux). */
const findRcloneInPath = async (): Promise<string> => {
  // Use `which` on POSIX, `where` on Windows
  const cmd = process.platform === "win32" ? "where" : "which";
  try {
    const { stdout } = await execFileAsync(cmd, ["rclone"]);
    const found = stdout.trim().split(/\r?\n/)[0];
    if (found) return found;
    throw new Error("empty output");
  } catch {
    throw new Error(
      process.platform === "win32"
        ? "未找到 rclone。Windows 下请在设置中心手动指定 rclone 可执行文件路径。"
        : "rclone 未找到，请确认已安装 rclone 且已添加到 PATH。"
    );
  }
};

export interface BackupServiceOptions {
  dataDir: string;
  repo: ConnectionRepository;
  getMasterPassword: () => string | undefined;
}

export class BackupService {
  private readonly dataDir: string;
  private readonly repo: ConnectionRepository;
  private readonly getMasterPassword: () => string | undefined;

  constructor(options: BackupServiceOptions) {
    this.dataDir = options.dataDir;
    this.repo = options.repo;
    this.getMasterPassword = options.getMasterPassword;
  }

  private requirePassword(): string {
    const password = this.getMasterPassword();
    if (!password) {
      throw new Error("云存档密码未解锁。请先在设置中心输入云存档密码。");
    }
    return password;
  }

  private getRemotePath(): string {
    const prefs = this.repo.getAppPreferences();
    const remotePath = prefs.backup.remotePath.trim();
    if (!remotePath) {
      throw new Error("云存档远端路径未配置。请在设置中心配置远端桶路径。");
    }
    return remotePath;
  }

  /** Resolve the rclone binary path: use explicit path from prefs, or auto-detect via PATH. */
  private async resolveRclone(): Promise<string> {
    const prefs = this.repo.getAppPreferences();
    const explicit = prefs.backup.rclonePath.trim();
    if (explicit) {
      if (!fs.existsSync(explicit)) {
        throw new Error(`设置中心指定的 rclone 路径不存在: ${explicit}`);
      }
      return explicit;
    }
    return findRcloneInPath();
  }

  async list(): Promise<BackupArchiveMeta[]> {
    const remotePath = this.getRemotePath();
    const rclone = await this.resolveRclone();

    try {
      const { stdout } = await execFileAsync(rclone, [
        "lsjson",
        remotePath,
        "--include",
        `*${META_EXTENSION}`
      ], { timeout: 30_000 });

      const items = JSON.parse(stdout) as Array<{ Name: string; Size: number; ModTime: string; Path: string }>;
      const metas: BackupArchiveMeta[] = [];

      for (const item of items) {
        try {
          const { stdout: metaContent } = await execFileAsync(rclone, [
            "cat",
            `${remotePath}/${item.Name}`
          ], { timeout: 15_000 });

          const parsed = JSON.parse(metaContent) as Partial<BackupArchiveMeta>;
          if (parsed.id && parsed.timestamp && parsed.fileName) {
            metas.push({
              id: parsed.id ?? "",
              timestamp: parsed.timestamp ?? "",
              deviceId: parsed.deviceId ?? "unknown",
              appVersion: parsed.appVersion ?? "unknown",
              hash: parsed.hash ?? "",
              fileName: parsed.fileName ?? "",
              sizeBytes: parsed.sizeBytes ?? 0
            });
          }
        } catch (error) {
          logger.warn("[Backup] failed to read meta file", { name: item.Name, error });
        }
      }

      return metas.sort((a, b) => b.timestamp.localeCompare(a.timestamp));
    } catch (error) {
      if (error instanceof Error && error.message.includes("directory not found")) {
        return [];
      }
      throw error;
    }
  }

  async run(conflictPolicy: BackupConflictPolicy = "skip"): Promise<{ ok: true; fileName?: string }> {
    const password = this.requirePassword();
    const remotePath = this.getRemotePath();
    const rclone = await this.resolveRclone();

    // Conflict check
    if (conflictPolicy === "skip") {
      const existing = await this.list();
      const prefs = this.repo.getAppPreferences();
      if (existing.length > 0 && prefs.backup.lastBackupAt) {
        const latestRemote = existing[0];
        if (latestRemote && latestRemote.timestamp > prefs.backup.lastBackupAt) {
          logger.info("[Backup] skip: remote has newer backup", {
            remote: latestRemote.timestamp,
            local: prefs.backup.lastBackupAt
          });
          return { ok: true };
        }
      }
    }

    // 1. Create consistent snapshot
    const tmpDir = path.join(this.dataDir, "backup-tmp", randomUUID());
    fs.mkdirSync(tmpDir, { recursive: true });
    const snapshotPath = path.join(tmpDir, SNAPSHOT_FILENAME);
    let fileName: string | undefined;
    let archiveUploaded = false;
    let metaUploaded = false;

    try {
      try {
        await this.repo.backupDatabase(snapshotPath);
      } catch (error) {
        throw new Error(`数据库快照失败: ${error instanceof Error ? error.message : String(error)}`);
      }

      if (!fs.existsSync(snapshotPath)) {
        throw new Error("数据库快照无效：未生成快照文件。");
      }
      const snapshotStats = fs.statSync(snapshotPath);
      if (!snapshotStats.isFile() || snapshotStats.size <= 0) {
        throw new Error("数据库快照无效：快照文件为空。");
      }

      // 2. Read snapshot and create manifest
      const timestamp = new Date().toISOString();
      const deviceId = getDeviceId();
      const dbHash = sha256File(snapshotPath);
      const archiveId = randomUUID();

      const manifest: ManifestJson = {
        appVersion: APP_VERSION,
        timestamp,
        deviceId,
        dbHash
      };

      // 3. Create archive payload: [manifestLength(4)] [manifest(N)] [snapshotDb(...)]
      const snapshotData = fs.readFileSync(snapshotPath);
      const manifestJson = JSON.stringify(manifest);
      const manifestBuf = Buffer.from(manifestJson, "utf8");
      const manifestLenBuf = Buffer.alloc(4);
      manifestLenBuf.writeUInt32BE(manifestBuf.length, 0);
      const payload = Buffer.concat([manifestLenBuf, manifestBuf, snapshotData]);

      // 4. Encrypt
      const encrypted = encryptBackupPayload(payload, password);
      fileName = `${timestamp.replace(/[:.]/g, "-")}-${deviceId}-${archiveId}${NSBK_EXTENSION}`;
      const encryptedPath = path.join(tmpDir, fileName);
      fs.writeFileSync(encryptedPath, encrypted);

      // 5. Create meta file
      const meta: BackupArchiveMeta = {
        id: archiveId,
        timestamp,
        deviceId,
        appVersion: APP_VERSION,
        hash: sha256Buffer(encrypted),
        fileName,
        sizeBytes: encrypted.length
      };
      const metaFileName = `${fileName}${META_EXTENSION}`;
      const metaPath = path.join(tmpDir, metaFileName);
      fs.writeFileSync(metaPath, JSON.stringify(meta, null, 2));

      // 6. Upload (and compensate if data uploaded but metadata failed)
      try {
        await execFileAsync(rclone, [
          "copyto",
          encryptedPath,
          `${remotePath}/${fileName}`
        ], { timeout: 120_000 });
        archiveUploaded = true;

        await execFileAsync(rclone, [
          "copyto",
          metaPath,
          `${remotePath}/${metaFileName}`
        ], { timeout: 30_000 });
        metaUploaded = true;
      } catch (error) {
        if (archiveUploaded && !metaUploaded && fileName) {
          try {
            await execFileAsync(rclone, ["deletefile", `${remotePath}/${fileName}`], { timeout: 30_000 });
          } catch (rollbackError) {
            logger.warn("[Backup] failed to rollback partially uploaded archive", {
              fileName,
              error: rollbackError
            });
          }
        }
        throw new Error(`上传失败: ${error instanceof Error ? error.message : String(error)}`);
      }

      // 7. Update lastBackupAt
      const prefs = this.repo.getAppPreferences();
      this.repo.saveAppPreferences({
        ...prefs,
        backup: { ...prefs.backup, lastBackupAt: timestamp }
      });

      logger.info("[Backup] completed", { fileName, archiveId });
      return { ok: true, fileName };
    } finally {
      this.cleanupTmp(tmpDir);
    }
  }

  async restore(archiveId: string, conflictPolicy: RestoreConflictPolicy = "skip_older"): Promise<{ ok: true }> {
    const password = this.requirePassword();
    const remotePath = this.getRemotePath();
    const rclone = await this.resolveRclone();

    // Find the archive meta
    const archives = await this.list();
    const target = archives.find((a) => a.id === archiveId);
    if (!target) {
      throw new Error("所选存档不存在或已被删除。");
    }

    // Conflict check
    if (conflictPolicy === "skip_older") {
      const prefs = this.repo.getAppPreferences();
      if (prefs.backup.lastBackupAt && target.timestamp <= prefs.backup.lastBackupAt) {
        throw new Error("所选存档不晚于本地数据版本，已跳过（可选择强制还原）。");
      }
    }

    // Download
    const tmpDir = path.join(this.dataDir, "restore-tmp", randomUUID());
    fs.mkdirSync(tmpDir, { recursive: true });
    const downloadPath = path.join(tmpDir, target.fileName);

    try {
      try {
        await execFileAsync(rclone, [
          "copyto",
          `${remotePath}/${target.fileName}`,
          downloadPath
        ], { timeout: 120_000 });
      } catch (error) {
        throw new Error(`下载失败: ${error instanceof Error ? error.message : String(error)}`);
      }

      // Decrypt & verify
      const encrypted = fs.readFileSync(downloadPath);
      let payload: Buffer;
      try {
        payload = decryptBackupPayload(encrypted, password);
      } catch {
        throw new Error("解密失败：云存档密码错误或数据已损坏。");
      }

      // Parse: [manifestLength(4)] [manifest(N)] [snapshotDb(...)]
      if (payload.length < 4) {
        throw new Error("备份数据格式无效。");
      }
      const manifestLen = payload.readUInt32BE(0);
      if (payload.length < 4 + manifestLen) {
        throw new Error("备份数据格式无效（manifest 长度异常）。");
      }
      const manifestJson = payload.subarray(4, 4 + manifestLen).toString("utf8");
      const snapshotData = payload.subarray(4 + manifestLen);

      let manifest: ManifestJson;
      try {
        manifest = JSON.parse(manifestJson) as ManifestJson;
      } catch {
        throw new Error("备份 manifest 解析失败。");
      }

      // Verify hash
      const computedHash = sha256Buffer(snapshotData);
      if (computedHash !== manifest.dbHash) {
        throw new Error("备份数据校验失败：数据库哈希不匹配。");
      }

      // Write restore-pending marker + snapshot
      const restoreSnapshotPath = path.join(this.dataDir, "restore-pending.db");
      fs.writeFileSync(restoreSnapshotPath, snapshotData);

      const restoreMarkerPath = path.join(this.dataDir, "restore-pending");
      fs.writeFileSync(restoreMarkerPath, JSON.stringify({
        timestamp: manifest.timestamp,
        deviceId: manifest.deviceId,
        appVersion: manifest.appVersion,
        restoredAt: new Date().toISOString()
      }));

      logger.info("[Backup] restore prepared, pending restart", { archiveId });
      return { ok: true };
    } finally {
      this.cleanupTmp(tmpDir);
    }
  }

  private cleanupTmp(tmpDir: string): void {
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {
      // ignore cleanup errors
    }
  }
}

/**
 * Check if a restore is pending and apply it before opening the database.
 * Call this early in app startup, before new SQLiteConnectionRepository(dbPath).
 */
export const applyPendingRestore = (dataDir: string, dbPath: string): boolean => {
  const markerPath = path.join(dataDir, "restore-pending");
  const snapshotPath = path.join(dataDir, "restore-pending.db");

  if (!fs.existsSync(markerPath) || !fs.existsSync(snapshotPath)) {
    return false;
  }

  try {
    // Remove WAL/SHM files
    const walPath = `${dbPath}-wal`;
    const shmPath = `${dbPath}-shm`;
    if (fs.existsSync(walPath)) {
      fs.unlinkSync(walPath);
    }
    if (fs.existsSync(shmPath)) {
      fs.unlinkSync(shmPath);
    }

    // Replace database
    fs.copyFileSync(snapshotPath, dbPath);

    // Cleanup restore files
    fs.unlinkSync(markerPath);
    fs.unlinkSync(snapshotPath);

    logger.info("[Backup] restore applied on startup");
    return true;
  } catch (error) {
    logger.error("[Backup] failed to apply restore", error);
    // Don't delete marker — let user retry
    return false;
  }
};
