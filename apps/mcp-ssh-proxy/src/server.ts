import { randomUUID } from "node:crypto";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import {
  buildSshConnectOptions,
  type ConnectionTargetAmbiguousError,
  type ConnectionTargetNotFoundError,
  type ReadonlyCredentialContext,
  type ServerSummary,
  listServerSummaries,
  resolveConnectionTarget,
  searchServerSummaries
} from "@nextshell/runtime";
import type { ExecResult, SshConnectOptions } from "@nextshell/ssh";
import { SshConnection } from "@nextshell/ssh";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";

const DEFAULT_IDLE_TIMEOUT_MS = 10 * 60_000;
const REAP_INTERVAL_MS = 60_000;

type RemoteExecutor = Pick<SshConnection, "exec" | "close">;
type ConnectionFactory = (options: SshConnectOptions) => Promise<RemoteExecutor>;

interface SessionRecord {
  id: string;
  server: ServerSummary;
  executor: RemoteExecutor;
  createdAt: string;
  lastUsedAt: number;
}

interface NextShellMcpServerOptions {
  context: ReadonlyCredentialContext;
  createConnection?: ConnectionFactory;
  idleTimeoutMs?: number;
}

const textResult = <T extends object>(text: string, structuredContent: T): CallToolResult => ({
  content: [{ type: "text", text }],
  structuredContent: structuredContent as Record<string, unknown>
});

const normalizeErrorMessage = (error: unknown): string => {
  if (error instanceof Error && error.message.trim().length > 0) {
    return error.message;
  }
  return "Unknown error";
};

class SessionManager {
  private readonly sessions = new Map<string, SessionRecord>();
  private readonly idleTimeoutMs: number;
  private readonly timer: ReturnType<typeof setInterval>;
  private readonly createConnection: ConnectionFactory;
  private readonly context: ReadonlyCredentialContext;

  constructor(options: NextShellMcpServerOptions) {
    this.context = options.context;
    this.createConnection = options.createConnection ?? ((connectOptions) => SshConnection.connect(connectOptions));
    this.idleTimeoutMs = options.idleTimeoutMs ?? DEFAULT_IDLE_TIMEOUT_MS;
    this.timer = setInterval(() => {
      void this.reapExpiredSessions();
    }, REAP_INTERVAL_MS);
    this.timer.unref?.();
  }

  async connect(target: string): Promise<
    | { ok: true; sessionId: string; server: ServerSummary; connectedAt: string }
    | { ok: false; reason: "not_found" | "ambiguous"; message: string; candidates: ServerSummary[] }
  > {
    try {
      const resolved = resolveConnectionTarget(this.context, target);
      const sshOptions = await buildSshConnectOptions(this.context, resolved.connection);
      const executor = await this.createConnection(sshOptions);
      const sessionId = randomUUID();
      const connectedAt = new Date().toISOString();
      this.sessions.set(sessionId, {
        id: sessionId,
        server: resolved.summary,
        executor,
        createdAt: connectedAt,
        lastUsedAt: Date.now()
      });
      return {
        ok: true,
        sessionId,
        server: resolved.summary,
        connectedAt
      };
    } catch (error) {
      const notFoundError = error as ConnectionTargetNotFoundError;
      if (notFoundError?.name === "ConnectionTargetNotFoundError") {
        return {
          ok: false,
          reason: "not_found",
          message: normalizeErrorMessage(error),
          candidates: []
        };
      }

      const ambiguousError = error as ConnectionTargetAmbiguousError;
      if (ambiguousError?.name === "ConnectionTargetAmbiguousError") {
        return {
          ok: false,
          reason: "ambiguous",
          message: normalizeErrorMessage(error),
          candidates: ambiguousError.candidates
        };
      }

      throw error;
    }
  }

  async exec(sessionId: string, command: string): Promise<
    | ({ ok: true; sessionId: string; command: string; executedAt: string } & ExecResult)
    | { ok: false; reason: "session_not_found"; message: string }
  > {
    const session = this.sessions.get(sessionId);
    if (!session) {
      return {
        ok: false,
        reason: "session_not_found",
        message: `Session not found: ${sessionId}`
      };
    }

    session.lastUsedAt = Date.now();
    const result = await session.executor.exec(command);
    return {
      ok: true,
      sessionId,
      command,
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.exitCode,
      executedAt: new Date().toISOString()
    };
  }

  async disconnect(sessionId: string): Promise<{ ok: true; disconnected: boolean; sessionId: string }> {
    const session = this.sessions.get(sessionId);
    if (!session) {
      return {
        ok: true,
        disconnected: false,
        sessionId
      };
    }

    this.sessions.delete(sessionId);
    await session.executor.close();
    return {
      ok: true,
      disconnected: true,
      sessionId
    };
  }

  async closeAll(): Promise<void> {
    clearInterval(this.timer);
    const sessions = Array.from(this.sessions.values());
    this.sessions.clear();
    await Promise.all(sessions.map((session) => session.executor.close().catch(() => undefined)));
  }

  private async reapExpiredSessions(): Promise<void> {
    const now = Date.now();
    const expiredSessionIds = Array.from(this.sessions.values())
      .filter((session) => now - session.lastUsedAt >= this.idleTimeoutMs)
      .map((session) => session.id);

    await Promise.all(expiredSessionIds.map((sessionId) => this.disconnect(sessionId)));
  }
}

export interface NextShellMcpRuntime {
  server: McpServer;
  close: () => Promise<void>;
}

export const createNextShellMcpServer = (options: NextShellMcpServerOptions): NextShellMcpRuntime => {
  const server = new McpServer({
    name: "nextshell-mcp-ssh-proxy",
    version: "0.1.0"
  });
  const sessions = new SessionManager(options);

  server.registerTool(
    "nextshell/list",
    {
      title: "List NextShell Servers",
      description: "List SSH server summaries available from the local NextShell credential store.",
      inputSchema: {}
    },
    async () => {
      const servers = listServerSummaries(options.context);
      return textResult(
        servers.length > 0
          ? `Found ${servers.length} server${servers.length === 1 ? "" : "s"}.`
          : "No servers found in the local NextShell store.",
        { servers }
      );
    }
  );

  server.registerTool(
    "nextshell/search",
    {
      title: "Search NextShell Servers",
      description: "Search NextShell connections by name, host, group path, or tags.",
      inputSchema: {
        query: z.string().trim().min(1),
        limit: z.number().int().min(1).max(100).optional()
      }
    },
    async ({ query, limit }) => {
      const servers = searchServerSummaries(options.context, query, limit);
      return textResult(
        servers.length > 0
          ? `Found ${servers.length} matching server${servers.length === 1 ? "" : "s"} for "${query}".`
          : `No servers matched "${query}".`,
        { query, servers }
      );
    }
  );

  server.registerTool(
    "nextshell/connect",
    {
      title: "Connect To A NextShell Server",
      description: "Resolve a target using NextShell data, then create a reusable SSH exec session.",
      inputSchema: {
        target: z.string().trim().min(1)
      }
    },
    async ({ target }) => {
      const result = await sessions.connect(target);
      if (!result.ok) {
        return textResult(result.message, result);
      }

      return textResult(
        `Connected to ${result.server.name} (${result.server.host}) with session ${result.sessionId}.`,
        result
      );
    }
  );

  server.registerTool(
    "nextshell/exec",
    {
      title: "Execute Remote Command",
      description: "Run a single remote command over an existing NextShell-backed SSH session.",
      inputSchema: {
        sessionId: z.string().uuid(),
        command: z.string().trim().min(1)
      }
    },
    async ({ sessionId, command }) => {
      const result = await sessions.exec(sessionId, command);
      if (!result.ok) {
        return textResult(result.message, result);
      }

      return textResult(
        `Command finished with exit code ${result.exitCode}.`,
        result
      );
    }
  );

  server.registerTool(
    "nextshell/disconnect",
    {
      title: "Disconnect Session",
      description: "Close a previously created NextShell-backed SSH session.",
      inputSchema: {
        sessionId: z.string().uuid()
      }
    },
    async ({ sessionId }) => {
      const result = await sessions.disconnect(sessionId);
      return textResult(
        result.disconnected ? `Disconnected session ${sessionId}.` : `Session ${sessionId} was not active.`,
        result
      );
    }
  );

  return {
    server,
    close: async () => {
      await sessions.closeAll();
      options.context.close();
    }
  };
};
