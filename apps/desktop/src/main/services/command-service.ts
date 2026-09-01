import { randomUUID } from "node:crypto";
import type {
  BatchCommandExecutionResult,
  BatchCommandResultItem,
  CommandExecutionResult,
  CommandHistoryEntry,
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  ScopedCommandItem,
  SavedCommand
} from "@nextshell/core";
import type { SshConnection } from "@nextshell/ssh";
import type {
  CommandBatchExecInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput
} from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";

import { normalizeError } from "./container-utils";
import { buildRemoteHomeDirCommand, parseRemoteHomeDir } from "./remote-home-dir";

interface CommandServiceOptions {
  connections: CachedConnectionRepository;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  ensureConnection: (
    connectionId: string,
    authOverride?: import("@nextshell/shared").SessionAuthOverrideInput
  ) => Promise<SshConnection>;
  listWorkspaces: () => CloudSyncWorkspaceProfile[];
  markWorkspaceCommandsDirty: (workspaceId: string) => void;
}

export interface CommandExecutionOptions {
  cwd?: string;
  signal?: AbortSignal;
  authOverride?: import("@nextshell/shared").SessionAuthOverrideInput;
}

const quotePosix = (value: string): string => `'${value.replaceAll("'", `'\\''`)}'`;
const CWD_MARKER_PREFIX = "\u001eNEXTSHELL_CWD=";
const CWD_MARKER_SUFFIX = "\u001f";

export class CommandService {
  private readonly connections: CachedConnectionRepository;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly ensureConnection: CommandServiceOptions["ensureConnection"];
  private readonly listWorkspaces: () => CloudSyncWorkspaceProfile[];
  private readonly markWorkspaceCommandsDirty: (workspaceId: string) => void;

  constructor(options: CommandServiceOptions) {
    this.connections = options.connections;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.ensureConnection = options.ensureConnection;
    this.listWorkspaces = options.listWorkspaces;
    this.markWorkspaceCommandsDirty = options.markWorkspaceCommandsDirty;
  }

  async execCommand(
    connectionId: string,
    command: string,
    options: CommandExecutionOptions = {}
  ): Promise<CommandExecutionResult & { cwd?: string }> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId, options.authOverride);
    const cwdPrefix = options.cwd ? `cd ${quotePosix(options.cwd)} || exit $?; ` : "";
    const effectiveCommand = `${cwdPrefix}printf '\\036NEXTSHELL_CWD=%s\\037' "$PWD" >&2; ${command}`;
    const result = await connection.exec(effectiveCommand, { signal: options.signal });
    const markerStart = result.stderr.indexOf(CWD_MARKER_PREFIX);
    const markerEnd =
      markerStart >= 0
        ? result.stderr.indexOf(CWD_MARKER_SUFFIX, markerStart + CWD_MARKER_PREFIX.length)
        : -1;
    const actualCwd =
      markerStart >= 0 && markerEnd > markerStart
        ? result.stderr.slice(markerStart + CWD_MARKER_PREFIX.length, markerEnd)
        : options.cwd;
    const cleanStderr =
      markerStart >= 0 && markerEnd >= 0
        ? `${result.stderr.slice(0, markerStart)}${result.stderr.slice(markerEnd + 1)}`
        : result.stderr;
    const execution: CommandExecutionResult & { cwd?: string } = {
      connectionId,
      command,
      stdout: result.stdout,
      stderr: cleanStderr,
      exitCode: result.exitCode,
      executedAt: new Date().toISOString(),
      ...(actualCwd ? { cwd: actualCwd } : {})
    };
    return execution;
  }

  async getSessionHomeDir(connectionId: string): Promise<{ path: string } | null> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId);
    try {
      const result = await connection.exec(buildRemoteHomeDirCommand());
      const homeDir = parseRemoteHomeDir(result.stdout);
      return homeDir ? { path: homeDir } : null;
    } catch {
      return null;
    }
  }

  async executeCommandWithRetry(
    connectionId: string,
    command: string,
    retryCount: number
  ): Promise<BatchCommandResultItem> {
    const maxAttempts = Math.max(1, retryCount + 1);
    let attempts = 0;
    const startedAt = Date.now();
    let lastExecution: CommandExecutionResult | undefined;
    let lastError: string | undefined;
    while (attempts < maxAttempts) {
      attempts += 1;
      try {
        const execution = await this.execCommand(connectionId, command);
        lastExecution = execution;
        if (execution.exitCode === 0) {
          return { ...execution, success: true, attempts, durationMs: Date.now() - startedAt };
        }
        lastError = execution.stderr || `Exit code ${execution.exitCode}`;
      } catch (error) {
        lastError = normalizeError(error);
      }
    }
    const failedAt = new Date().toISOString();
    return {
      connectionId,
      command,
      stdout: lastExecution?.stdout ?? "",
      stderr: lastExecution?.stderr ?? "",
      exitCode: lastExecution?.exitCode ?? -1,
      executedAt: lastExecution?.executedAt ?? failedAt,
      success: false,
      attempts,
      durationMs: Date.now() - startedAt,
      error: lastError
    };
  }

  async execBatchCommand(input: CommandBatchExecInput): Promise<BatchCommandExecutionResult> {
    const startedAt = new Date();
    const uniqueConnectionIds = Array.from(new Set(input.connectionIds));
    const queue = [...uniqueConnectionIds];
    const results: BatchCommandResultItem[] = [];
    const workerCount = Math.max(1, Math.min(input.maxConcurrency, queue.length));
    await Promise.all(
      Array.from({ length: workerCount }, async () => {
        while (queue.length > 0) {
          const connectionId = queue.shift();
          if (!connectionId) return;
          if (!this.connections.getById(connectionId)) {
            results.push({
              connectionId,
              command: input.command,
              stdout: "",
              stderr: "",
              exitCode: -1,
              executedAt: new Date().toISOString(),
              success: false,
              attempts: 0,
              durationMs: 0,
              error: "Connection not found"
            });
            continue;
          }
          const result = await this.executeCommandWithRetry(
            connectionId,
            input.command,
            input.retryCount
          );
          results.push(result);
        }
      })
    );
    const finishedAt = new Date();
    const successCount = results.filter((item) => item.success).length;
    const failedCount = results.length - successCount;
    const summary: BatchCommandExecutionResult = {
      command: input.command,
      startedAt: startedAt.toISOString(),
      finishedAt: finishedAt.toISOString(),
      durationMs: finishedAt.getTime() - startedAt.getTime(),
      total: results.length,
      successCount,
      failedCount,
      results: results.sort((a, b) => a.connectionId.localeCompare(b.connectionId))
    };
    return summary;
  }

  listCommandHistory(): CommandHistoryEntry[] {
    return this.connections.listCommandHistory();
  }

  pushCommandHistory(command: string): CommandHistoryEntry {
    return this.connections.pushCommandHistory(command);
  }

  removeCommandHistory(command: string): { ok: true } {
    this.connections.removeCommandHistory(command);
    return { ok: true };
  }

  clearCommandHistory(): { ok: true } {
    this.connections.clearCommandHistory();
    return { ok: true };
  }

  listScopedSavedCommands(): ScopedCommandItem[] {
    const workspaceNameById = new Map(
      this.listWorkspaces().map((workspace) => [
        workspace.id,
        workspace.displayName || workspace.workspaceName
      ])
    );
    const local = this.connections.listSavedCommands({}).map((command) => ({
      ...command,
      scope: "local" as const
    }));
    const workspaceScoped = this.listWorkspaces().flatMap((workspace) =>
      this.connections.listWorkspaceCommands(workspace.id).map((command) => ({
        id: command.id,
        name: command.name,
        description: command.description,
        group: command.group,
        command: command.command,
        isTemplate: command.isTemplate,
        createdAt: command.createdAt,
        updatedAt: command.updatedAt,
        scope: "workspace" as const,
        workspaceId: workspace.id,
        workspaceName: workspaceNameById.get(workspace.id)
      }))
    );
    return [...local, ...workspaceScoped];
  }

  upsertSavedCommand(input: SavedCommandUpsertInput): SavedCommand {
    if (input.workspaceId) {
      const existing = input.id
        ? this.connections
            .listWorkspaceCommands(input.workspaceId)
            .find((item) => item.id === input.id)
        : undefined;
      const now = new Date().toISOString();
      const saved = this.connections.upsertWorkspaceCommand({
        id: input.id ?? randomUUID(),
        workspaceId: input.workspaceId,
        name: input.name,
        description: input.description,
        group: input.group,
        command: input.command,
        isTemplate: input.isTemplate,
        createdAt: existing?.createdAt ?? now,
        updatedAt: now
      });
      this.markWorkspaceCommandsDirty(input.workspaceId);
      return {
        id: saved.id,
        name: saved.name,
        description: saved.description,
        group: saved.group,
        command: saved.command,
        isTemplate: saved.isTemplate,
        createdAt: saved.createdAt,
        updatedAt: saved.updatedAt
      };
    }
    return this.connections.upsertSavedCommand({
      id: input.id,
      name: input.name,
      description: input.description,
      group: input.group,
      command: input.command,
      isTemplate: input.isTemplate
    });
  }

  removeSavedCommand(input: SavedCommandRemoveInput): { ok: true } {
    if (input.workspaceId) {
      this.connections.removeWorkspaceCommand(input.workspaceId, input.id);
      this.markWorkspaceCommandsDirty(input.workspaceId);
      return { ok: true };
    }
    this.connections.clearTemplateParams(input.id);
    this.connections.removeSavedCommand(input.id);
    return { ok: true };
  }
}
