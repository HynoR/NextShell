import type {
  BatchCommandExecutionResult,
  BatchCommandResultItem,
  CommandExecutionResult,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionProfile,
  SavedCommand,
} from "@nextshell/core";
import type { SshConnection } from "@nextshell/ssh";
import type {
  CommandBatchExecInput,
  SavedCommandListInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  TemplateParamsClearInput,
  TemplateParamsListInput,
  TemplateParamsUpsertInput,
} from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";

import { normalizeError } from "./container-utils";
import { buildRemoteHomeDirCommand, parseRemoteHomeDir } from "./remote-home-dir";

interface CommandServiceOptions {
  connections: CachedConnectionRepository;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  ensureConnection: (connectionId: string) => Promise<SshConnection>;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
}

export class CommandService {
  private readonly connections: CachedConnectionRepository;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly ensureConnection: (connectionId: string) => Promise<SshConnection>;
  private readonly appendAuditLogIfEnabled: CommandServiceOptions["appendAuditLogIfEnabled"];

  constructor(options: CommandServiceOptions) {
    this.connections = options.connections;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.ensureConnection = options.ensureConnection;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
  }

  async execCommand(
    connectionId: string,
    command: string,
  ): Promise<CommandExecutionResult> {
    this.getConnectionOrThrow(connectionId);
    const connection = await this.ensureConnection(connectionId);
    const result = await connection.exec(command);
    const execution: CommandExecutionResult = {
      connectionId,
      command,
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.exitCode,
      executedAt: new Date().toISOString(),
    };
    this.appendAuditLogIfEnabled({
      action: "command.exec",
      level: result.exitCode === 0 ? "info" : "warn",
      connectionId,
      message: "Executed command on remote host",
      metadata: { command, exitCode: result.exitCode },
    });
    return execution;
  }

  async getSessionHomeDir(
    connectionId: string,
  ): Promise<{ path: string } | null> {
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
    retryCount: number,
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
      error: lastError,
    };
  }

  async execBatchCommand(
    input: CommandBatchExecInput,
  ): Promise<BatchCommandExecutionResult> {
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
              error: "Connection not found",
            });
            continue;
          }
          const result = await this.executeCommandWithRetry(connectionId, input.command, input.retryCount);
          results.push(result);
        }
      }),
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
      results: results.sort((a, b) => a.connectionId.localeCompare(b.connectionId)),
    };
    this.appendAuditLogIfEnabled({
      action: "command.exec_batch",
      level: failedCount > 0 ? "warn" : "info",
      message: "Executed batch command",
      metadata: {
        command: input.command,
        total: summary.total,
        successCount,
        failedCount,
        retryCount: input.retryCount,
        maxConcurrency: input.maxConcurrency,
      },
    });
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

  listSavedCommands(query?: SavedCommandListInput): SavedCommand[] {
    return this.connections.listSavedCommands(query ?? {});
  }

  upsertSavedCommand(input: SavedCommandUpsertInput): SavedCommand {
    return this.connections.upsertSavedCommand({
      id: input.id,
      name: input.name,
      description: input.description,
      group: input.group,
      command: input.command,
      isTemplate: input.isTemplate,
    });
  }

  removeSavedCommand(input: SavedCommandRemoveInput): { ok: true } {
    this.connections.clearTemplateParams(input.id);
    this.connections.removeSavedCommand(input.id);
    return { ok: true };
  }

  listTemplateParams(input?: TemplateParamsListInput): CommandTemplateParam[] {
    return this.connections.listTemplateParams(input?.commandId);
  }

  upsertTemplateParams(input: TemplateParamsUpsertInput): { ok: true } {
    this.connections.upsertTemplateParams(input.commandId, input.params);
    return { ok: true };
  }

  clearTemplateParams(input: TemplateParamsClearInput): { ok: true } {
    this.connections.clearTemplateParams(input.commandId);
    return { ok: true };
  }
}
