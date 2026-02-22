import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import type { WebContents } from "electron";
import type { SshConnection } from "../../../../../packages/ssh/src/index";
import type { SftpEditStatusEvent, SftpEditSessionInfo } from "../../../../../packages/shared/src/index";
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

/**
 * Split a command string into tokens, respecting double-quoted segments.
 * e.g. `"C:\Program Files\code.exe" --wait` → [`C:\Program Files\code.exe`, `--wait`]
 */
function tokenizeCommand(command: string): string[] {
  const tokens: string[] = [];
  let current = "";
  let inQuotes = false;

  for (let i = 0; i < command.length; i++) {
    const ch = command[i]!;
    if (ch === '"') {
      inQuotes = !inQuotes;
    } else if (ch === " " && !inQuotes) {
      if (current.length > 0) {
        tokens.push(current);
        current = "";
      }
    } else {
      current += ch;
    }
  }
  if (current.length > 0) tokens.push(current);
  return tokens;
}

const MAX_UPLOAD_RETRIES = 3;
const RETRY_BASE_MS = 300;
const TEMP_ROOT = path.join(os.tmpdir(), "nextshell-edit");
const IDLE_CHECK_INTERVAL_MS = 5 * 60 * 1000; // 5 min
const IDLE_TIMEOUT_MS = 2 * 60 * 60 * 1000;    // 2 hours
const EDITOR_LAUNCH_PROBE_TIMEOUT_MS = 1200;

type EditorCommandSource = "settings" | "visual" | "editor" | "platform-default";

interface ResolvedEditorCommand {
  source: EditorCommandSource;
  command: string;
}

interface PreparedEditorInvocation {
  executable: string;
  args: string[];
}

interface EditorLaunchResult {
  ok: boolean;
  source: EditorCommandSource;
  requestedCommand: string;
  resolvedCommand: string;
  executable: string;
  args: string[];
  probe: "timeout" | "error" | "exit" | "close" | "exception";
  error?: string;
  code?: string;
  exitCode?: number | null;
  signal?: NodeJS.Signals | null;
}

type EditStatus = SftpEditStatusEvent["status"];

interface ActiveEditSession {
  editId: string;
  connectionId: string;
  remotePath: string;
  localPath: string;
  watcher: ChokidarWatcher;
  uploading: boolean;
  pendingUpload: boolean;
  sender: WebContents;
  lastUploadedHash: string;
  lastActivityAt: number;
  senderDestroyedHandler: () => void;
}

export interface RemoteEditManagerDeps {
  getConnection: (connectionId: string) => Promise<SshConnection>;
}

interface BuiltinEditSession {
  editId: string;
  connectionId: string;
  remotePath: string;
  sender: WebContents;
  lastActivityAt: number;
  senderDestroyedHandler: () => void;
}

export class RemoteEditManager {
  private readonly sessions = new Map<string, ActiveEditSession>();
  private readonly builtinSessions = new Map<string, BuiltinEditSession>();
  private readonly deps: RemoteEditManagerDeps;
  private idleTimer?: ReturnType<typeof setInterval>;

  constructor(deps: RemoteEditManagerDeps) {
    this.deps = deps;
    this.startIdleChecker();
  }

  async open(
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ): Promise<{ editId: string; localPath: string }> {
    const existing = this.findByRemotePath(connectionId, remotePath);
    if (existing) {
      const launchResult = await this.spawnEditor(editorCommand, existing.localPath);
      if (!launchResult.ok) {
        throw this.buildEditorLaunchError(launchResult);
      }

      logger.info("[RemoteEdit] reopened with existing session", {
        editId: existing.editId,
        connectionId,
        remotePath,
        source: launchResult.source
      });
      return { editId: existing.editId, localPath: existing.localPath };
    }

    const editId = randomUUID();
    const localPath = this.buildLocalPath(connectionId, remotePath);

    this.sendStatus(sender, {
      editId,
      connectionId,
      remotePath,
      status: "downloading"
    });

    await fsp.mkdir(path.dirname(localPath), { recursive: true });
    const connection = await this.deps.getConnection(connectionId);
    await connection.download(remotePath, localPath);

    // Compute initial hash of downloaded file
    const initialHash = await this.computeFileHash(localPath);

    // Use chokidar for reliable cross-platform file watching
    // atomic: true handles "atomic writes" (write-to-temp → rename) used by most editors
    // awaitWriteFinish ensures we get a single event after write stabilizes (replaces manual debounce)
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

    watcher.on("change", () => {
      this.onFileChanged(editId);
    });

    watcher.on("error", (err: unknown) => {
      logger.error("[RemoteEdit] watcher error", { editId, error: String(err) });
    });

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
      lastUploadedHash: initialHash,
      lastActivityAt: Date.now(),
      senderDestroyedHandler
    };

    this.sessions.set(editId, session);

    const launchResult = await this.spawnEditor(editorCommand, localPath);
    if (!launchResult.ok) {
      await this.cleanup(session, false);
      throw this.buildEditorLaunchError(launchResult);
    }

    this.sendStatus(sender, {
      editId,
      connectionId,
      remotePath,
      status: "editing"
    });

    logger.info("[RemoteEdit] opened", {
      editId,
      connectionId,
      remotePath,
      localPath,
      source: launchResult.source
    });
    return { editId, localPath };
  }

  async stop(editId: string): Promise<void> {
    const session = this.sessions.get(editId);
    if (!session) return;
    await this.cleanup(session, true);
  }

  async stopAll(): Promise<void> {
    await Promise.all(
      Array.from(this.sessions.values()).map((s) => this.cleanup(s, true))
    );
  }

  async openBuiltin(
    connectionId: string,
    remotePath: string,
    sender: WebContents
  ): Promise<{ editId: string; content: string }> {
    // Check existing external edit sessions
    const existingExternal = this.findByRemotePath(connectionId, remotePath);
    if (existingExternal) {
      const connection = await this.deps.getConnection(connectionId);
      const buf = await connection.readFileContent(remotePath);
      return { editId: existingExternal.editId, content: buf.toString("utf-8") };
    }

    // Check existing builtin sessions
    for (const session of this.builtinSessions.values()) {
      if (session.connectionId === connectionId && session.remotePath === remotePath) {
        const connection = await this.deps.getConnection(connectionId);
        const buf = await connection.readFileContent(remotePath);
        return { editId: session.editId, content: buf.toString("utf-8") };
      }
    }

    const editId = randomUUID();

    this.sendStatus(sender, {
      editId,
      connectionId,
      remotePath,
      status: "downloading"
    });

    const connection = await this.deps.getConnection(connectionId);
    const buf = await connection.readFileContent(remotePath);
    const content = buf.toString("utf-8");

    const senderDestroyedHandler = () => {
      this.builtinSessions.delete(editId);
    };
    sender.once("destroyed", senderDestroyedHandler);

    this.builtinSessions.set(editId, {
      editId,
      connectionId,
      remotePath,
      sender,
      lastActivityAt: Date.now(),
      senderDestroyedHandler
    });

    this.sendStatus(sender, {
      editId,
      connectionId,
      remotePath,
      status: "editing"
    });

    logger.info("[RemoteEdit] opened builtin", { editId, connectionId, remotePath });
    return { editId, content };
  }

  async saveBuiltin(editId: string, connectionId: string, remotePath: string, content: string): Promise<void> {
    const session = this.builtinSessions.get(editId);

    if (session) {
      this.sendStatus(session.sender, {
        editId,
        connectionId: session.connectionId,
        remotePath: session.remotePath,
        status: "uploading"
      });
    }

    const connection = await this.deps.getConnection(connectionId);
    await connection.writeFileContent(remotePath, Buffer.from(content, "utf-8"));

    if (session) {
      session.lastActivityAt = Date.now();
      this.sendStatus(session.sender, {
        editId,
        connectionId: session.connectionId,
        remotePath: session.remotePath,
        status: "synced"
      });
    }

    logger.info("[RemoteEdit] saved builtin", { editId, connectionId, remotePath });
  }

  listSessions(): SftpEditSessionInfo[] {
    const external = Array.from(this.sessions.values()).map((s) => ({
      editId: s.editId,
      connectionId: s.connectionId,
      remotePath: s.remotePath,
      localPath: s.localPath,
      status: s.uploading ? "uploading" as const : "editing" as const,
      lastActivityAt: s.lastActivityAt
    }));

    const builtin = Array.from(this.builtinSessions.values()).map((s) => ({
      editId: s.editId,
      connectionId: s.connectionId,
      remotePath: s.remotePath,
      localPath: "",
      status: "editing" as const,
      lastActivityAt: s.lastActivityAt
    }));

    return [...external, ...builtin];
  }

  async cleanupByConnectionId(connectionId: string): Promise<void> {
    const targets = Array.from(this.sessions.values()).filter(
      (s) => s.connectionId === connectionId
    );

    await Promise.all(targets.map((s) => this.cleanup(s, true)));

    for (const [id, session] of this.builtinSessions) {
      if (session.connectionId === connectionId) {
        try {
          session.sender.removeListener("destroyed", session.senderDestroyedHandler);
        } catch { /* sender may already be destroyed */ }
        this.builtinSessions.delete(id);
      }
    }
  }

  async dispose(): Promise<void> {
    if (this.idleTimer) {
      clearInterval(this.idleTimer);
      this.idleTimer = undefined;
    }

    await Promise.all(
      Array.from(this.sessions.values()).map((s) => this.cleanup(s, false))
    );

    for (const [id, session] of this.builtinSessions) {
      try {
        session.sender.removeListener("destroyed", session.senderDestroyedHandler);
      } catch { /* sender may already be destroyed */ }
      this.builtinSessions.delete(id);
    }

    try {
      await fsp.rm(TEMP_ROOT, { recursive: true, force: true });
    } catch {
      // best-effort
    }
  }

  private startIdleChecker(): void {
    this.idleTimer = setInterval(() => {
      const now = Date.now();
      for (const session of this.sessions.values()) {
        if (now - session.lastActivityAt > IDLE_TIMEOUT_MS) {
          logger.info("[RemoteEdit] session idle timeout", {
            editId: session.editId,
            remotePath: session.remotePath,
            idleMinutes: Math.round((now - session.lastActivityAt) / 60000)
          });
          this.sendStatus(session.sender, {
            editId: session.editId,
            connectionId: session.connectionId,
            remotePath: session.remotePath,
            status: "closed",
            message: "已超时自动关闭（2小时无活动）"
          });
          void this.cleanup(session, false);
        }
      }
    }, IDLE_CHECK_INTERVAL_MS);
  }

  private findByRemotePath(connectionId: string, remotePath: string): ActiveEditSession | undefined {
    for (const session of this.sessions.values()) {
      if (session.connectionId === connectionId && session.remotePath === remotePath) {
        return session;
      }
    }
    return undefined;
  }

  private buildLocalPath(connectionId: string, remotePath: string): string {
    const connShort = connectionId.slice(0, 8);
    const pathHash = createHash("md5").update(remotePath).digest("hex").slice(0, 8);
    const fileName = path.posix.basename(remotePath);
    return path.join(TEMP_ROOT, connShort, `${pathHash}-${fileName}`);
  }

  private resolveEditorCommand(editorCommand: string): ResolvedEditorCommand {
    const fromSettings = editorCommand.trim();
    if (fromSettings) {
      return { source: "settings", command: fromSettings };
    }

    const fromVisual = (process.env["VISUAL"] ?? "").trim();
    if (fromVisual) {
      return { source: "visual", command: fromVisual };
    }

    const fromEditor = (process.env["EDITOR"] ?? "").trim();
    if (fromEditor) {
      return { source: "editor", command: fromEditor };
    }

    if (process.platform === "darwin") {
      return { source: "platform-default", command: "open -t" };
    }
    if (process.platform === "win32") {
      return { source: "platform-default", command: "notepad" };
    }
    return { source: "platform-default", command: "xdg-open" };
  }

  private prepareEditorInvocation(command: string, localPath: string): PreparedEditorInvocation {
    const parts = tokenizeCommand(command.trim());
    if (parts.length === 0) {
      throw new Error("editor command is empty");
    }

    const executable = parts[0]!;
    const extraArgs = parts.slice(1);

    // macOS app bundle support: "/Applications/Foo.app" => open -a "/Applications/Foo.app" <file>
    if (process.platform === "darwin" && executable.toLowerCase().endsWith(".app")) {
      const args = ["-a", executable, localPath];
      if (extraArgs.length > 0) {
        args.push("--args", ...extraArgs);
      }
      return { executable: "open", args };
    }

    return {
      executable,
      args: [...extraArgs, localPath]
    };
  }

  private buildEditorLaunchError(result: EditorLaunchResult): Error {
    const detail = result.error ?? "未知错误";
    const message = `外部编辑器启动失败（来源: ${result.source}）：${detail}`;
    const error = new Error(message) as Error & {
      source?: EditorCommandSource;
      code?: string;
      requestedCommand?: string;
      resolvedCommand?: string;
      executable?: string;
      args?: string[];
    };
    error.source = result.source;
    error.code = result.code;
    error.requestedCommand = result.requestedCommand;
    error.resolvedCommand = result.resolvedCommand;
    error.executable = result.executable;
    error.args = result.args;
    return error;
  }

  private async spawnEditor(editorCommand: string, localPath: string): Promise<EditorLaunchResult> {
    const resolved = this.resolveEditorCommand(editorCommand);
    let prepared: PreparedEditorInvocation;

    try {
      prepared = this.prepareEditorInvocation(resolved.command, localPath);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      return {
        ok: false,
        source: resolved.source,
        requestedCommand: editorCommand,
        resolvedCommand: resolved.command,
        executable: "",
        args: [],
        probe: "exception",
        error: reason
      };
    }

    try {
      const child = spawn(prepared.executable, prepared.args, {
        detached: true,
        stdio: "ignore",
        shell: process.platform === "win32"
      });

      const probeResult = await new Promise<{
        ok: boolean;
        probe: EditorLaunchResult["probe"];
        error?: string;
        code?: string;
        exitCode?: number | null;
        signal?: NodeJS.Signals | null;
      }>((resolve) => {
        let settled = false;
        const finish = (value: {
          ok: boolean;
          probe: EditorLaunchResult["probe"];
          error?: string;
          code?: string;
          exitCode?: number | null;
          signal?: NodeJS.Signals | null;
        }) => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          resolve(value);
        };

        const timer = setTimeout(() => {
          finish({ ok: true, probe: "timeout" });
        }, EDITOR_LAUNCH_PROBE_TIMEOUT_MS);

        child.once("error", (err) => {
          const error = err instanceof Error ? err.message : String(err);
          const code = (err as NodeJS.ErrnoException).code;
          finish({ ok: false, probe: "error", error, code });
        });

        child.once("exit", (exitCode, signal) => {
          if (exitCode === 0) {
            finish({ ok: true, probe: "exit", exitCode, signal });
            return;
          }
          finish({
            ok: false,
            probe: "exit",
            exitCode,
            signal,
            error: `进程退出码异常: ${String(exitCode)}`
          });
        });

        child.once("close", (exitCode, signal) => {
          if (exitCode === 0) {
            finish({ ok: true, probe: "close", exitCode, signal });
            return;
          }
          finish({
            ok: false,
            probe: "close",
            exitCode,
            signal,
            error: `进程关闭码异常: ${String(exitCode)}`
          });
        });
      });

      child.unref();

      const result: EditorLaunchResult = {
        ok: probeResult.ok,
        source: resolved.source,
        requestedCommand: editorCommand,
        resolvedCommand: resolved.command,
        executable: prepared.executable,
        args: prepared.args,
        probe: probeResult.probe,
        error: probeResult.error,
        code: probeResult.code,
        exitCode: probeResult.exitCode,
        signal: probeResult.signal
      };

      if (!result.ok) {
        logger.error("[RemoteEdit] editor launch failed", {
          source: result.source,
          requestedCommand: result.requestedCommand,
          resolvedCommand: result.resolvedCommand,
          executable: result.executable,
          args: result.args,
          probe: result.probe,
          error: result.error,
          code: result.code
        });
      } else {
        logger.info("[RemoteEdit] editor launch success", {
          source: result.source,
          resolvedCommand: result.resolvedCommand,
          executable: result.executable,
          args: result.args,
          probe: result.probe
        });
      }

      return result;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      const result: EditorLaunchResult = {
        ok: false,
        source: resolved.source,
        requestedCommand: editorCommand,
        resolvedCommand: resolved.command,
        executable: prepared.executable,
        args: prepared.args,
        probe: "exception",
        error: reason
      };
      logger.error("[RemoteEdit] failed to spawn editor", {
        source: result.source,
        requestedCommand: result.requestedCommand,
        resolvedCommand: result.resolvedCommand,
        executable: result.executable,
        args: result.args,
        error: result.error
      });
      return result;
    }
  }

  private async computeFileHash(filePath: string): Promise<string> {
    try {
      const content = await fsp.readFile(filePath);
      return createHash("md5").update(content).digest("hex");
    } catch {
      return "";
    }
  }

  private onFileChanged(editId: string): void {
    const session = this.sessions.get(editId);
    if (!session) return;

    session.lastActivityAt = Date.now();
    // chokidar's awaitWriteFinish already debounces, trigger upload directly
    void this.triggerUpload(session);
  }

  private async triggerUpload(session: ActiveEditSession): Promise<void> {
    if (session.uploading) {
      session.pendingUpload = true;
      return;
    }

    // MD5 hash dedup: skip upload if file content hasn't actually changed
    const currentHash = await this.computeFileHash(session.localPath);
    if (currentHash && currentHash === session.lastUploadedHash) {
      logger.info("[RemoteEdit] skipping upload, content unchanged", {
        editId: session.editId,
        remotePath: session.remotePath
      });
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

    let lastError: unknown;
    for (let attempt = 0; attempt < MAX_UPLOAD_RETRIES; attempt++) {
      try {
        const connection = await this.deps.getConnection(session.connectionId);
        await connection.upload(session.localPath, session.remotePath);

        // Update hash after successful upload
        session.lastUploadedHash = currentHash;

        this.sendStatus(session.sender, {
          editId: session.editId,
          connectionId: session.connectionId,
          remotePath: session.remotePath,
          status: "synced"
        });

        logger.info("[RemoteEdit] synced", {
          editId: session.editId,
          remotePath: session.remotePath,
          attempt: attempt + 1
        });

        session.uploading = false;

        if (session.pendingUpload) {
          session.pendingUpload = false;
          void this.triggerUpload(session);
        }
        return;
      } catch (err) {
        lastError = err;
        if (attempt < MAX_UPLOAD_RETRIES - 1) {
          await new Promise((r) => setTimeout(r, RETRY_BASE_MS * Math.pow(2, attempt)));
        }
      }
    }

    session.uploading = false;

    const errMsg = lastError instanceof Error ? lastError.message : String(lastError);
    this.sendStatus(session.sender, {
      editId: session.editId,
      connectionId: session.connectionId,
      remotePath: session.remotePath,
      status: "error",
      message: `上传失败: ${errMsg}`
    });

    logger.error("[RemoteEdit] upload failed after retries", {
      editId: session.editId,
      remotePath: session.remotePath,
      error: errMsg
    });

    if (session.pendingUpload) {
      session.pendingUpload = false;
      void this.triggerUpload(session);
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

  private sendStatus(sender: WebContents, payload: Omit<SftpEditStatusEvent, "message"> & { message?: string }): void {
    try {
      if (!sender.isDestroyed()) {
        sender.send(IPCChannel.SftpEditStatus, payload);
      }
    } catch {
      // renderer may have closed
    }
  }
}
