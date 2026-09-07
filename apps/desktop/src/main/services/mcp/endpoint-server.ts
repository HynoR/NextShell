import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo, Socket } from "node:net";
import { randomUUID } from "node:crypto";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest } from "@modelcontextprotocol/sdk/types.js";
import type { AgentConnectedClient } from "@nextshell/shared";

import type { AgentClientIdentity } from "./agent-gateway";

/** Loopback only: the endpoint is never reachable from another machine. */
export const ENDPOINT_HOST = "127.0.0.1";
export const MCP_PATH = "/mcp";

const MAX_REQUEST_BODY_BYTES = 4 * 1024 * 1024;
const MCP_PATHS = new Set(["/", MCP_PATH]);

/**
 * A client that is SIGKILLed never sends the DELETE that closes its transport,
 * so sessions have to expire on their own or every crashed client leaks an
 * `McpServer` for the lifetime of the app.
 */
export const DEFAULT_MAX_SESSIONS = 32;
export const DEFAULT_SESSION_IDLE_TIMEOUT_MS = 10 * 60_000;
const SESSION_SWEEP_INTERVAL_MS = 30_000;

export interface AgentLogger {
  info?: (message: string, meta?: Record<string, unknown>) => void;
  warn?: (message: string, meta?: Record<string, unknown>) => void;
  error?: (message: string, meta?: Record<string, unknown>) => void;
}

export interface McpEndpointServerOptions {
  /** Loopback TCP port; 0 lets the OS pick one (tests). */
  port: number;
  createMcpServer: (identity: AgentClientIdentity) => McpServer;
  onClientsChanged?: (clients: AgentConnectedClient[]) => void;
  /** Extra exact-match Host header values accepted on top of the loopback set. */
  extraAllowedHosts?: string[];
  /** Concurrent MCP sessions; further `initialize` requests get 503. */
  maxSessions?: number;
  /** Sessions with no request and no open stream for this long are torn down. */
  sessionIdleTimeoutMs?: number;
  /** Test seam for the idle sweep. */
  now?: () => number;
  logger?: AgentLogger;
}

export class AgentEndpointError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AgentEndpointError";
  }
}

interface SessionEntry {
  id: string;
  transport: StreamableHTTPServerTransport;
  server: McpServer;
  identity: AgentClientIdentity;
  connectedAt: string;
  lastSeenAt: number;
  /** Responses still streaming (SSE); an idle session with one is not dead. */
  openStreams: number;
}

export const buildEndpointUrl = (port: number): string =>
  `http://${ENDPOINT_HOST}:${port}${MCP_PATH}`;

const readRequestBody = async (req: IncomingMessage): Promise<unknown> => {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of req) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk));
    size += buffer.byteLength;
    if (size > MAX_REQUEST_BODY_BYTES) {
      throw new AgentEndpointError("Request body too large");
    }
    chunks.push(buffer);
  }
  if (chunks.length === 0) {
    return undefined;
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
};

const jsonRpcError = (res: ServerResponse, status: number, code: number, message: string): void => {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify({ jsonrpc: "2.0", error: { code, message }, id: null }));
};

const readClientInfo = (body: unknown): { name: string | null; version: string | null } => {
  const params = (body as { params?: { clientInfo?: { name?: unknown; version?: unknown } } })
    ?.params;
  const info = params?.clientInfo;
  return {
    name: typeof info?.name === "string" ? info.name : null,
    version: typeof info?.version === "string" ? info.version : null
  };
};

/**
 * The MCP endpoint is a plain Streamable HTTP server bound to 127.0.0.1, so
 * every MCP client dials it with one URL and no bridge process. There is no
 * token: any process on this machine may drive the tabs the user opened, the
 * same trust the user extends to every local program. Host/Origin checks keep
 * browser pages out.
 */
export class McpEndpointServer {
  private readonly options: McpEndpointServerOptions;
  private readonly sessions = new Map<string, SessionEntry>();
  private readonly openSockets = new Set<Socket>();
  private readonly maxSessions: number;
  private readonly idleTimeoutMs: number;
  private httpServer: Server | null = null;
  private activePort: number | null = null;
  private sweepTimer: ReturnType<typeof setInterval> | null = null;

  constructor(options: McpEndpointServerOptions) {
    this.options = options;
    this.maxSessions = Math.max(1, options.maxSessions ?? DEFAULT_MAX_SESSIONS);
    this.idleTimeoutMs = Math.max(
      1_000,
      options.sessionIdleTimeoutMs ?? DEFAULT_SESSION_IDLE_TIMEOUT_MS
    );
  }

  private now(): number {
    return this.options.now?.() ?? Date.now();
  }

  get listening(): boolean {
    return this.httpServer !== null;
  }

  /** Bound port (differs from the option only when it was 0). */
  get port(): number | null {
    return this.activePort;
  }

  get url(): string | null {
    return this.activePort === null ? null : buildEndpointUrl(this.activePort);
  }

  getClients(): AgentConnectedClient[] {
    return [...this.sessions.values()].map((entry) => ({
      id: entry.id,
      name: entry.identity.name,
      version: entry.identity.version,
      transport: entry.identity.transport,
      connectedAt: entry.connectedAt
    }));
  }

  async start(): Promise<void> {
    if (this.listening) {
      return;
    }

    try {
      await this.startListener();
    } catch (error) {
      await this.stop();
      throw error;
    }

    this.sweepTimer = setInterval(() => {
      void this.sweepIdleSessions();
    }, SESSION_SWEEP_INTERVAL_MS);
    // Never keep the event loop (and therefore the app quit) alive.
    this.sweepTimer.unref?.();
  }

  /** Exposed for tests; the interval calls it. */
  async sweepIdleSessions(): Promise<void> {
    const now = this.now();
    const expired = [...this.sessions.values()].filter(
      (entry) => entry.openStreams === 0 && now - entry.lastSeenAt >= this.idleTimeoutMs
    );
    if (expired.length === 0) {
      return;
    }
    for (const entry of expired) {
      this.sessions.delete(entry.id);
    }
    await Promise.all(expired.map((entry) => this.closeSession(entry)));
    this.options.logger?.info?.("Evicted idle MCP sessions", { count: expired.length });
    this.options.onClientsChanged?.(this.getClients());
  }

  private async closeSession(entry: SessionEntry): Promise<void> {
    try {
      await entry.transport.close();
    } catch {
      // Already gone.
    }
    try {
      await entry.server.close();
    } catch {
      // Already gone.
    }
  }

  private async startListener(): Promise<void> {
    const server = createServer(this.createRequestListener());
    this.trackConnections(server);
    await new Promise<void>((resolve, reject) => {
      const onError = (error: NodeJS.ErrnoException): void =>
        reject(
          error.code === "EADDRINUSE"
            ? new AgentEndpointError(
                `端口 ${this.options.port} 已被占用（另一个 NextShell 实例或其他程序）；请在设置里换一个端口`
              )
            : error
        );
      server.once("error", onError);
      server.listen(this.options.port, ENDPOINT_HOST, () => {
        server.off("error", onError);
        resolve();
      });
    });

    this.httpServer = server;
    this.activePort = (server.address() as AddressInfo).port;
    this.options.logger?.info?.("MCP endpoint listening", { url: this.url });
  }

  private trackConnections(server: Server): void {
    server.on("connection", (socket: Socket) => {
      this.openSockets.add(socket);
      socket.on("close", () => {
        this.openSockets.delete(socket);
      });
    });
  }

  /**
   * chmod and file removal only affect *new* connections, so revoking access
   * has to tear down the live sockets as well.
   */
  async disconnectClients(): Promise<void> {
    const entries = [...this.sessions.values()];
    this.sessions.clear();
    await Promise.all(entries.map((entry) => this.closeSession(entry)));
    for (const socket of this.openSockets) {
      socket.destroy();
    }
    this.openSockets.clear();
    this.options.onClientsChanged?.(this.getClients());
  }

  async stop(): Promise<void> {
    if (this.sweepTimer) {
      clearInterval(this.sweepTimer);
      this.sweepTimer = null;
    }
    await this.disconnectClients();

    if (this.httpServer) {
      await new Promise<void>((resolve) => {
        this.httpServer?.close(() => resolve());
      });
      this.httpServer = null;
    }
    this.activePort = null;
  }

  // ─── Request handling ─────────────────────────────────────────────────────

  private allowedHosts(): Set<string> {
    const hosts = new Set<string>(["localhost", "127.0.0.1", "[::1]"]);
    if (this.activePort !== null) {
      for (const bare of [...hosts]) hosts.add(`${bare}:${this.activePort}`);
    }
    for (const host of this.options.extraAllowedHosts ?? []) {
      hosts.add(host);
    }
    return hosts;
  }

  /**
   * Exact-match Host allowlist (DNS-rebinding guard) plus a loopback-only
   * Origin check. A missing Host header is rejected.
   */
  private isRequestOriginAllowed(req: IncomingMessage): boolean {
    const host = req.headers.host;
    if (!host || !this.allowedHosts().has(host.toLowerCase())) {
      return false;
    }
    const origin = req.headers.origin;
    if (typeof origin === "string" && origin.length > 0 && origin !== "null") {
      try {
        const parsed = new URL(origin);
        if (
          parsed.hostname !== "localhost" &&
          parsed.hostname !== "127.0.0.1" &&
          parsed.hostname !== "::1"
        ) {
          return false;
        }
      } catch {
        return false;
      }
    }
    return true;
  }

  private createRequestListener(): (req: IncomingMessage, res: ServerResponse) => void {
    return (req, res) => {
      void this.handleRequest(req, res).catch((error: unknown) => {
        this.options.logger?.error?.("MCP endpoint request failed", {
          error: error instanceof Error ? error.message : String(error)
        });
        if (!res.headersSent) {
          jsonRpcError(res, 500, -32603, "Internal error");
        } else {
          res.end();
        }
      });
    };
  }

  private async handleRequest(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const url = (req.url ?? "/").split("?")[0] ?? "/";
    if (!MCP_PATHS.has(url)) {
      jsonRpcError(res, 404, -32601, "Not found");
      return;
    }
    if (!this.isRequestOriginAllowed(req)) {
      jsonRpcError(res, 403, -32600, "Forbidden");
      return;
    }

    let body: unknown;
    if (req.method === "POST") {
      try {
        body = await readRequestBody(req);
      } catch {
        jsonRpcError(res, 400, -32700, "Parse error");
        return;
      }
    }

    const sessionId = req.headers["mcp-session-id"];
    const existing = typeof sessionId === "string" ? this.sessions.get(sessionId) : undefined;
    if (existing) {
      this.trackActivity(existing, res);
      await existing.transport.handleRequest(req, res, body);
      return;
    }

    if (req.method !== "POST" || !isInitializeRequest(body)) {
      jsonRpcError(res, 400, -32000, "No valid MCP session; send an initialize request first");
      return;
    }

    if (this.sessions.size >= this.maxSessions) {
      // One last chance to reclaim whatever the sweep timer has not yet noticed.
      await this.sweepIdleSessions();
    }
    if (this.sessions.size >= this.maxSessions) {
      this.options.logger?.warn?.("Refused an MCP session: concurrency limit reached", {
        limit: this.maxSessions
      });
      jsonRpcError(res, 503, -32000, "Too many MCP sessions; close an existing one and retry");
      return;
    }

    const entry = await this.createSession(body);
    this.trackActivity(entry, res);
    await entry.transport.handleRequest(req, res, body);
  }

  /**
   * Marks the session live and counts responses that stay open (SSE streams), so
   * a client that only listens is not mistaken for a dead one.
   */
  private trackActivity(entry: SessionEntry, res: ServerResponse): void {
    entry.lastSeenAt = this.now();
    entry.openStreams += 1;
    let released = false;
    res.once("close", () => {
      if (released) {
        return;
      }
      released = true;
      entry.openStreams = Math.max(0, entry.openStreams - 1);
      entry.lastSeenAt = this.now();
    });
  }

  private async createSession(body: unknown): Promise<SessionEntry> {
    const sessionId = randomUUID();
    const clientInfo = readClientInfo(body);
    const identity: AgentClientIdentity = {
      id: sessionId,
      name: clientInfo.name,
      version: clientInfo.version,
      transport: "http"
    };

    const httpTransport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => sessionId
    });
    const server = this.options.createMcpServer(identity);
    const entry: SessionEntry = {
      id: sessionId,
      transport: httpTransport,
      server,
      identity,
      connectedAt: new Date().toISOString(),
      lastSeenAt: this.now(),
      openStreams: 0
    };

    httpTransport.onclose = () => {
      if (this.sessions.delete(sessionId)) {
        void server.close().catch(() => undefined);
        this.options.onClientsChanged?.(this.getClients());
      }
    };

    await server.connect(httpTransport);
    this.sessions.set(sessionId, entry);
    this.options.onClientsChanged?.(this.getClients());
    return entry;
  }
}
