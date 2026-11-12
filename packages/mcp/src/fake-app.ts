import { createServer, type IncomingMessage, type Server } from "node:http";
import { randomUUID } from "node:crypto";
import { z } from "zod";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest, type CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { toolDefinitions } from "./tools";

/** Test stand-in for the NextShell 2 endpoint: loopback Streamable HTTP with a bearer check. */
export interface FakeAppOptions {
  /** Bearer token the app accepts; read per request so tests can rotate it. */
  token: () => string;
  port?: number;
  tools?: string[];
  invoke?: (name: string, args: Record<string, unknown>) => Promise<CallToolResult>;
}

export const success = (data: Record<string, unknown>): CallToolResult => {
  const structuredContent = { ok: true, data };
  return {
    content: [{ type: "text", text: JSON.stringify(structuredContent) }],
    structuredContent
  };
};

export class FakeApp {
  readonly requests: { method: string; authorization: string | undefined }[] = [];
  readonly sessions = new Map<
    string,
    { transport: StreamableHTTPServerTransport; server: McpServer }
  >();
  private http: Server | undefined;
  port = 0;

  constructor(private readonly options: FakeAppOptions) {}

  async start(): Promise<void> {
    this.http = createServer((req, res) => {
      void this.handle(req, res).catch(() => {
        if (!res.headersSent) res.writeHead(500);
        res.end();
      });
    });
    await new Promise<void>((resolve) =>
      this.http!.listen(this.options.port ?? this.port, "127.0.0.1", resolve)
    );
    this.port = (this.http.address() as { port: number }).port;
  }

  async stop(): Promise<void> {
    const http = this.http;
    this.http = undefined;
    for (const { transport } of this.sessions.values()) await transport.close().catch(() => {});
    this.sessions.clear();
    if (!http) return;
    http.closeAllConnections();
    await new Promise<void>((resolve) => http.close(() => resolve()));
  }

  private createSession() {
    const server = new McpServer({ name: "NextShell-v2", version: "test" });
    for (const name of this.options.tools ?? Object.keys(toolDefinitions)) {
      server.registerTool(name, { inputSchema: z.looseObject({}) }, async (args) =>
        (this.options.invoke ?? (async () => success({})))(name, args)
      );
    }
    const transport: StreamableHTTPServerTransport = new StreamableHTTPServerTransport({
      sessionIdGenerator: randomUUID,
      enableJsonResponse: true,
      onsessioninitialized: (id) => {
        this.sessions.set(id, { transport, server });
      }
    });
    return { server, transport };
  }

  private async handle(req: IncomingMessage, res: import("node:http").ServerResponse) {
    const authorization = req.headers.authorization;
    this.requests.push({ method: req.method ?? "", authorization });
    if (authorization !== `Bearer ${this.options.token()}`) {
      res.writeHead(401, { "WWW-Authenticate": "Bearer" }).end("Unauthorized");
      return;
    }
    let body: unknown;
    if (req.method === "POST") {
      const chunks: Buffer[] = [];
      for await (const chunk of req) chunks.push(chunk as Buffer);
      body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    }
    const id = req.headers["mcp-session-id"];
    const existing = typeof id === "string" ? this.sessions.get(id) : undefined;
    if (existing) return existing.transport.handleRequest(req, res, body);
    if (req.method === "POST" && isInitializeRequest(body)) {
      const { server, transport } = this.createSession();
      await server.connect(transport);
      return transport.handleRequest(req, res, body);
    }
    res.writeHead(404).end("Session revoked or expired");
  }
}
