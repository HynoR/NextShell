import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import type { WebContents } from "electron";
import type { SshConnection } from "../../../../../packages/ssh/src/index";
import type {
  SftpEditStatusEvent,
  SftpEditSessionInfo
} from "../../../../../packages/shared/src/index";
import { IPCChannel } from "../../../../../packages/shared/src/index";
import { logger } from "../logger";

// chokidar v5: loaded via dynamic import at runtime for ESM compatibility.
// Type defined inline since the package exports map lacks a "types" condition.
interface ChokidarWatcher {
  on(event: "change" | "add" | "unlink", listener: (path: string) => void): this;
  on(event: "error", listener: (err: unknown) => void): this;
  close(): Promise<void>;
}

interface ChokidarModule {
  watch(paths: string | string[], options?: Record<string, unknown>): ChokidarWatcher;
}

let chokidarModulePromise: Promise<ChokidarModule> | undefined;

const loadChokidar = async (): Promise<ChokidarModule> => {
  chokidarModulePromise ??= import("chokidar").then((module) => {
    const typedModule = module as unknown as { default?: ChokidarModule };
    return typedModule.default ?? (module as unknown as ChokidarModule);
  });
  return chokidarModulePromise;
};

const MAX_BUILTIN_EDIT_BYTES = 10 * 1024 * 1024; // 10MB — 内置编辑器（内存 + IPC）
const MAX_EXTERNAL_EDIT_BYTES = 50 * 1024 * 1024; // 50MB — 外部编辑器（磁盘下载）
const TEMP_ROOT = path.join(os.tmpdir(), "nextshell-edit");

interface WatcherLike {
  close(): Promise<void>;
}

interface ActiveEditSession {
  editId: string;
  connectionId: string;
  remotePath: string;
  localPath: string;
  watcher: WatcherLike;
  uploading: boolean;
  pendingUpload: boolean;
  sender: WebContents;
  lastActivityAt: number;
  senderDestroyedHandler: () => void;
}

export interface RemoteEditManagerDeps {
  getConnection: (connectionId: string) => Promise<SshConnection>;
  /** 测试可注入假 watcher;默认用 chokidar(awaitWriteFinish 去抖 + atomic 原子写入)。 */
  watch?: (localPath: string, onChange: () => void) => WatcherLike;
}

const defaultEditorCommand = (): string => {
  if (process.platform === "darwin") return "open -t";
  if (process.platform === "win32") return "notepad";
  return "xdg-open";
};

/** Double-quote a path for `shell: true` spawn。cmd 不认反斜杠转义(文件名也不能含双引号),只有 POSIX sh 要转义。 */
const quoteForShell = (value: string): string =>
  process.platform === "win32" ? `"${value}"` : `"${value.replace(/(["\\$`])/g, "\\$1")}"`;

export class RemoteEditManager {
  private readonly sessions = new Map<string, ActiveEditSession>();
  private readonly deps: RemoteEditManagerDeps;

  constructor(deps: RemoteEditManagerDeps) {
    this.deps = deps;
  }

  /** 内置编辑器读取:无状态,读一次返回内容。 */
  async readFile(connectionId: string, remotePath: string): Promise<{ content: string }> {
    const connection = await this.deps.getConnection(connectionId);
    await this.assertFileSizeWithin(
      connection,
      remotePath,
      MAX_BUILTIN_EDIT_BYTES,
      "使用内置编辑器打开"
    );
    // stat 限额挡不住 /proc 这类 st_size=0 的文件,读取本身也带上限(超限时多读 1 字节可观测)。
    const buf = await connection.readFileContent(remotePath, { maxBytes: MAX_BUILTIN_EDIT_BYTES });
    if (buf.length > MAX_BUILTIN_EDIT_BYTES) {
      throw new Error("文件过大，无法使用内置编辑器打开：超过 10MB 限制");
    }
    return { content: buf.toString("utf-8") };
  }

  /** 内置编辑器写回:无状态,写一次即完成。 */
  async writeFile(connectionId: string, remotePath: string, content: string): Promise<void> {
    if (Buffer.byteLength(content, "utf-8") > MAX_BUILTIN_EDIT_BYTES) {
      throw new Error("文件过大，无法使用内置编辑器保存：超过 10MB 限制");
    }
    const connection = await this.deps.getConnection(connectionId);
    await connection.writeFileContent(remotePath, Buffer.from(content, "utf-8"));
  }

  async open(
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ): Promise<{ editId: string; localPath: string }> {
    const existing = this.findByRemotePath(connectionId, remotePath);
    if (existing) {
      await this.launchEditor(editorCommand, existing.localPath);
      logger.info("[RemoteEdit] reopened with existing session", {
        editId: existing.editId,
        connectionId,
        remotePath
      });
      return { editId: existing.editId, localPath: existing.localPath };
    }

    const editId = randomUUID();
    const localPath = this.buildLocalPath(connectionId, remotePath);

    this.sendStatus(sender, { editId, connectionId, remotePath, status: "downloading" });

    let watcher: WatcherLike;
    try {
      await fsp.mkdir(path.dirname(localPath), { recursive: true });
      const connection = await this.deps.getConnection(connectionId);
      await this.assertFileSizeWithin(
        connection,
        remotePath,
        MAX_EXTERNAL_EDIT_BYTES,
        "使用外部编辑器打开"
      );
      await connection.download(remotePath, localPath);
      watcher = await this.createWatcher(localPath, () => {
        this.onFileChanged(editId);
      });
    } catch (error) {
      // downloading 已让侧栏出了一行,失败必须用 closed 收回,否则留幽灵行。
      this.sendStatus(sender, { editId, connectionId, remotePath, status: "closed" });
      throw error;
    }

    // Sender lifecycle: auto-cleanup when renderer window closes
    const senderDestroyedHandler = () => {
      logger.info("[RemoteEdit] sender destroyed, cleaning up", { editId });
      const active = this.sessions.get(editId);
      if (active) {
        void this.cleanup(active, false);
      }
    };
    sender.once("destroyed", senderDestroyedHandler);

    const session: ActiveEditSession = {
      editId,
      connectionId,
      remotePath,
      localPath,
      watcher,
      uploading: false,
      pendingUpload: false,
      sender,
      lastActivityAt: Date.now(),
      senderDestroyedHandler
    };

    this.sessions.set(editId, session);

    try {
      await this.launchEditor(editorCommand, localPath);
    } catch (error) {
      await this.cleanup(session, true);
      throw error;
    }

    this.sendStatus(sender, { editId, connectionId, remotePath, status: "editing" });

    logger.info("[RemoteEdit] opened", { editId, connectionId, remotePath, localPath });
    return { editId, localPath };
  }

  async stop(editId: string): Promise<void> {
    const session = this.sessions.get(editId);
    if (!session) return;
    await this.cleanup(session, true);
  }

  async stopAll(): Promise<void> {
    await Promise.all(Array.from(this.sessions.values()).map((s) => this.cleanup(s, true)));
  }

  listSessions(): SftpEditSessionInfo[] {
    return Array.from(this.sessions.values()).map((s) => ({
      editId: s.editId,
      connectionId: s.connectionId,
      remotePath: s.remotePath,
      localPath: s.localPath,
      status: s.uploading ? ("uploading" as const) : ("editing" as const),
      lastActivityAt: s.lastActivityAt
    }));
  }

  async cleanupByConnectionId(connectionId: string): Promise<void> {
    const targets = Array.from(this.sessions.values()).filter(
      (s) => s.connectionId === connectionId
    );
    await Promise.all(targets.map((s) => this.cleanup(s, true)));
  }

  async dispose(): Promise<void> {
    await Promise.all(Array.from(this.sessions.values()).map((s) => this.cleanup(s, false)));

    try {
      await fsp.rm(TEMP_ROOT, { recursive: true, force: true });
    } catch {
      // best-effort
    }
  }

  private async createWatcher(localPath: string, onChange: () => void): Promise<WatcherLike> {
    if (this.deps.watch) {
      return this.deps.watch(localPath, onChange);
    }

    const chokidar = await loadChokidar();
    const watcher = chokidar.watch(localPath, {
      persistent: true,
      ignoreInitial: true,
      atomic: true,
      awaitWriteFinish: {
        stabilityThreshold: 500,
        pollInterval: 100
      }
    });

    watcher.on("change", onChange);
    watcher.on("error", (err: unknown) => {
      logger.error("[RemoteEdit] watcher error", { localPath, error: String(err) });
    });

    return watcher;
  }

  /** 启动外部编辑器;spawn 的 error 事件即失败,成功spawn后立即返回(编辑器常驻运行)。 */
  private launchEditor(editorCommand: string, localPath: string): Promise<void> {
    const command = editorCommand.trim() || defaultEditorCommand();
    const child = spawn(`${command} ${quoteForShell(localPath)}`, {
      shell: true,
      detached: true,
      stdio: "ignore"
    });

    return new Promise((resolve, reject) => {
      child.once("error", (err) => {
        reject(new Error(`外部编辑器启动失败：${err.message}`));
      });
      child.once("spawn", () => {
        child.unref();
        resolve();
      });
    });
  }

  private findByRemotePath(
    connectionId: string,
    remotePath: string
  ): ActiveEditSession | undefined {
    for (const session of this.sessions.values()) {
      if (session.connectionId === connectionId && session.remotePath === remotePath) {
        return session;
      }
    }
    return undefined;
  }

  private async assertFileSizeWithin(
    connection: SshConnection,
    remotePath: string,
    maxBytes: number,
    label: string
  ): Promise<void> {
    const stats = await connection.stat(remotePath);
    if (stats.size > maxBytes) {
      const sizeMB = (stats.size / (1024 * 1024)).toFixed(1);
      const limitMB = (maxBytes / (1024 * 1024)).toFixed(0);
      throw new Error(`文件过大，无法${label}：${sizeMB}MB 超过 ${limitMB}MB 限制`);
    }
  }

  private buildLocalPath(connectionId: string, remotePath: string): string {
    const connShort = connectionId.slice(0, 8);
    const pathHash = createHash("md5").update(remotePath).digest("hex").slice(0, 8);
    const fileName = path.posix.basename(remotePath);
    return path.join(TEMP_ROOT, connShort, `${pathHash}-${fileName}`);
  }

  private onFileChanged(editId: string): void {
    const session = this.sessions.get(editId);
    if (!session) return;

    session.lastActivityAt = Date.now();
    // chokidar's awaitWriteFinish already debounces, trigger upload directly
    void this.triggerUpload(session);
  }

  /** change → 直接上传,失败一次即置 error;上传中再来变更就排队一次。 */
  private async triggerUpload(session: ActiveEditSession): Promise<void> {
    // stop() 之后 finally 里的排队重传不能再跑:本地文件已删,只会弹一个假「上传失败」。
    if (!this.sessions.has(session.editId)) return;
    if (session.uploading) {
      session.pendingUpload = true;
      return;
    }

    session.uploading = true;
    session.pendingUpload = false;

    this.sendStatus(session.sender, {
      editId: session.editId,
      connectionId: session.connectionId,
      remotePath: session.remotePath,
      status: "uploading"
    });

    try {
      const connection = await this.deps.getConnection(session.connectionId);
      await connection.upload(session.localPath, session.remotePath);

      this.sendStatus(session.sender, {
        editId: session.editId,
        connectionId: session.connectionId,
        remotePath: session.remotePath,
        status: "synced"
      });

      logger.info("[RemoteEdit] synced", {
        editId: session.editId,
        remotePath: session.remotePath
      });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err);
      this.sendStatus(session.sender, {
        editId: session.editId,
        connectionId: session.connectionId,
        remotePath: session.remotePath,
        status: "error",
        message: `上传失败: ${errMsg}`
      });

      logger.error("[RemoteEdit] upload failed", {
        editId: session.editId,
        remotePath: session.remotePath,
        error: errMsg
      });
    } finally {
      session.uploading = false;
      if (session.pendingUpload) {
        session.pendingUpload = false;
        void this.triggerUpload(session);
      }
    }
  }

  private async cleanup(session: ActiveEditSession, notify: boolean): Promise<void> {
    if (!this.sessions.has(session.editId)) return;
    this.sessions.delete(session.editId);

    // Remove sender destroyed listener to prevent double-cleanup
    try {
      session.sender.removeListener("destroyed", session.senderDestroyedHandler);
    } catch {
      // sender may already be destroyed
    }

    try {
      await session.watcher.close();
    } catch {
      // already closed
    }

    try {
      await fsp.unlink(session.localPath);
    } catch {
      // file may already be gone
    }

    if (notify) {
      this.sendStatus(session.sender, {
        editId: session.editId,
        connectionId: session.connectionId,
        remotePath: session.remotePath,
        status: "closed"
      });
    }

    logger.info("[RemoteEdit] closed", {
      editId: session.editId,
      connectionId: session.connectionId,
      remotePath: session.remotePath
    });
  }

  private sendStatus(
    sender: WebContents,
    payload: Omit<SftpEditStatusEvent, "message"> & { message?: string }
  ): void {
    try {
      if (!sender.isDestroyed()) {
        sender.send(IPCChannel.SftpEditStatus, payload);
      }
    } catch {
      // renderer may have closed
    }
  }
}
