import { afterEach, describe, expect, test } from "vitest";
import { request as httpRequest, type IncomingHttpHeaders } from "node:http";
import { randomUUID } from "node:crypto";
import { z } from "zod";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { buildEndpointUrl, McpEndpointServer } from "./endpoint-server";
import type { AgentClientIdentity } from "./agent-gateway";

interface HttpResult {
  status: number;
  headers: IncomingHttpHeaders;
  body: string;
}

const send = (
  port: number,
  options: { method: string; headers?: Record<string, string>; body?: unknown }
): Promise<HttpResult> =>
  new Promise((resolve, reject) => {
    const payload = options.body === undefined ? undefined : JSON.stringify(options.body);
    const req = httpRequest(
      {
        method: options.method,
        path: "/mcp",
        host: "127.0.0.1",
        port,
        agent: false,
        headers: {
          accept: "application/json, text/event-stream",
          ...(payload ? { "content-type": "application/json" } : {}),
          ...options.headers
        }
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () =>
          resolve({
            status: res.statusCode ?? 0,
            headers: res.headers,
            body: Buffer.concat(chunks).toString("utf8")
          })
        );
      }
    );
    req.on("error", reject);
    if (payload) {
      req.write(payload);
    }
    req.end();
  });

/** POST responses arrive as SSE frames by default. */
const parseMessage = (body: string): Record<string, unknown> => {
  const dataLine = body.split("\n").find((line) => line.startsWith("data:"));
  const json = dataLine ? dataLine.slice("data:".length).trim() : body;
  return JSON.parse(json) as Record<string, unknown>;
};

const initializeBody = {
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: {
    protocolVersion: "2025-06-18",
    capabilities: {},
    clientInfo: { name: "vitest-client", version: "9.9.9" }
  }
};

const createTestServer = (_identity: AgentClientIdentity): McpServer => {
  const server = new McpServer({ name: "nextshell-test", version: "0.0.0" });
  server.registerTool(
    "ping",
    { description: "ping", inputSchema: { value: z.string().optional() } },
    async () => ({ content: [{ type: "text", text: "pong" }] })
  );
  return server;
};

let endpointServer: McpEndpointServer | null = null;

const startEndpoint = async (options: {
  port?: number;
  maxSessions?: number;
  sessionIdleTimeoutMs?: number;
  now?: () => number;
}): Promise<McpEndpointServer> => {
  const server = new McpEndpointServer({
    port: options.port ?? 0,
    maxSessions: options.maxSessions,
    sessionIdleTimeoutMs: options.sessionIdleTimeoutMs,
    now: options.now,
    createMcpServer: createTestServer
  });
  await server.start();
  endpointServer = server;
  return server;
};

afterEach(async () => {
  await endpointServer?.stop();
  endpointServer = null;
});

describe("port binding", () => {
  test("a busy port fails with a message naming the port", async () => {
    const first = await startEndpoint({});
    const second = new McpEndpointServer({ port: first.port!, createMcpServer: createTestServer });

    await expect(second.start()).rejects.toThrow(new RegExp(`端口 ${first.port} 已被占用`));
    expect(second.listening).toBe(false);
  });

  test("the url is the loopback address plus /mcp", async () => {
    const server = await startEndpoint({});
    expect(server.url).toBe(buildEndpointUrl(server.port!));
    expect(server.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/mcp$/);
  });
});

describe("session routing", () => {
  test("initialize registers a client and answers tool calls", async () => {
    const server = await startEndpoint({});
    const port = server.port!;

    const initialized = await send(port, { method: "POST", body: initializeBody });
    expect(initialized.status).toBe(200);
    const sessionId = initialized.headers["mcp-session-id"];
    expect(typeof sessionId).toBe("string");
    expect(parseMessage(initialized.body).result).toBeDefined();

    const clients = server.getClients();
    expect(clients).toHaveLength(1);
    expect(clients[0]?.name).toBe("vitest-client");
    expect(clients[0]?.transport).toBe("http");

    const notified = await send(port, {
      method: "POST",
      headers: { "mcp-session-id": String(sessionId) },
      body: { jsonrpc: "2.0", method: "notifications/initialized" }
    });
    expect(notified.status).toBe(202);

    const listed = await send(port, {
      method: "POST",
      headers: { "mcp-session-id": String(sessionId) },
      body: { jsonrpc: "2.0", id: 2, method: "tools/list", params: {} }
    });
    expect(listed.status).toBe(200);
    const result = parseMessage(listed.body).result as { tools: Array<{ name: string }> };
    expect(result.tools.map((tool) => tool.name)).toContain("ping");
  });

  test.each(["POST", "GET", "DELETE"])(
    "an unknown session id returns 404 for %s",
    async (method) => {
      const server = await startEndpoint({});
      const response = await send(server.port!, {
        method,
        headers: { "mcp-session-id": randomUUID() },
        body:
          method === "POST"
            ? { jsonrpc: "2.0", id: 3, method: "tools/list", params: {} }
            : undefined
      });

      expect(response.status).toBe(404);
      expect(server.getClients()).toHaveLength(0);
    }
  );

  test("a tool request without a session id still returns 400", async () => {
    const server = await startEndpoint({});
    const response = await send(server.port!, {
      method: "POST",
      body: { jsonrpc: "2.0", id: 3, method: "tools/list", params: {} }
    });

    expect(response.status).toBe(400);
    expect(server.getClients()).toHaveLength(0);
  });

  test("DELETE closes the session and drops the client", async () => {
    const server = await startEndpoint({});
    const port = server.port!;
    const initialized = await send(port, { method: "POST", body: initializeBody });
    const sessionId = String(initialized.headers["mcp-session-id"]);
    expect(server.getClients()).toHaveLength(1);

    const deleted = await send(port, {
      method: "DELETE",
      headers: { "mcp-session-id": sessionId }
    });

    expect(deleted.status).toBeLessThan(300);
    expect(server.getClients()).toHaveLength(0);
  });
});

describe("session reclamation", () => {
  test("a client that never sends DELETE is evicted once it goes idle", async () => {
    let clock = 1_000_000;
    const server = await startEndpoint({
      sessionIdleTimeoutMs: 60_000,
      now: () => clock
    });
    const port = server.port!;

    await send(port, { method: "POST", body: initializeBody });
    expect(server.getClients()).toHaveLength(1);

    clock += 30_000;
    await server.sweepIdleSessions();
    expect(server.getClients()).toHaveLength(1);

    // Stands in for a SIGKILLed client: no DELETE ever arrives.
    clock += 61_000;
    await server.sweepIdleSessions();
    expect(server.getClients()).toHaveLength(0);
  });

  test("concurrent sessions are capped instead of growing without bound", async () => {
    const server = await startEndpoint({ maxSessions: 2 });
    const port = server.port!;

    for (let index = 0; index < 2; index += 1) {
      const accepted = await send(port, { method: "POST", body: initializeBody });
      expect(accepted.status).toBe(200);
    }

    const refused = await send(port, { method: "POST", body: initializeBody });
    expect(refused.status).toBe(503);
    expect(server.getClients()).toHaveLength(2);
  });
});

describe("request origin", () => {
  test("a foreign Host header is rejected (DNS rebinding)", async () => {
    const server = await startEndpoint({});

    const rebound = await send(server.port!, {
      method: "POST",
      headers: { host: "attacker.example" },
      body: initializeBody
    });
    expect(rebound.status).toBe(403);
  });

  test("the loopback Host with the bound port is accepted", async () => {
    const server = await startEndpoint({});
    const accepted = await send(server.port!, {
      method: "POST",
      headers: { host: `localhost:${server.port}` },
      body: initializeBody
    });
    expect(accepted.status).toBe(200);
  });

  test("a non-loopback Origin header is rejected", async () => {
    const server = await startEndpoint({});
    const response = await send(server.port!, {
      method: "POST",
      headers: { origin: "https://evil.example" },
      body: initializeBody
    });

    expect(response.status).toBe(403);
  });
});

describe("lifecycle", () => {
  test("a client can reinitialize and call a tool after a server restart on the same port", async () => {
    const first = await startEndpoint({});
    const port = first.port!;
    const initialized = await send(port, { method: "POST", body: initializeBody });
    const oldSessionId = String(initialized.headers["mcp-session-id"]);
    await first.stop();
    const restarted = await startEndpoint({ port });

    const expired = await send(port, {
      method: "POST",
      headers: { "mcp-session-id": oldSessionId },
      body: { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "ping", arguments: {} } }
    });
    expect(expired.status).toBe(404);
    expect(parseMessage(expired.body)).toMatchObject({
      error: { code: -32000, message: expect.stringContaining("without Mcp-Session-Id") }
    });
    expect(restarted.getClients()).toHaveLength(0);

    const reinitialized = await send(port, { method: "POST", body: initializeBody });
    expect(reinitialized.status).toBe(200);
    const newSessionId = reinitialized.headers["mcp-session-id"];
    expect(typeof newSessionId).toBe("string");
    expect(newSessionId).not.toBe(oldSessionId);
    const headers = { "mcp-session-id": String(newSessionId) };
    const notified = await send(port, {
      method: "POST",
      headers,
      body: { jsonrpc: "2.0", method: "notifications/initialized" }
    });
    expect(notified.status).toBe(202);
    const called = await send(port, {
      method: "POST",
      headers,
      body: { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "ping", arguments: {} } }
    });
    expect(called.status).toBe(200);
    expect(parseMessage(called.body)).toMatchObject({
      result: { content: [{ type: "text", text: "pong" }] }
    });
    expect(restarted.getClients()).toHaveLength(1);
  });

  test("stop drops clients and frees the port", async () => {
    const server = await startEndpoint({});
    const port = server.port!;

    await send(port, { method: "POST", body: initializeBody });
    expect(server.getClients()).toHaveLength(1);

    await server.stop();
    endpointServer = null;

    await expect(send(port, { method: "POST", body: initializeBody })).rejects.toThrow();
    expect(server.getClients()).toHaveLength(0);
    expect(server.listening).toBe(false);
    expect(server.url).toBeNull();
  });

  test("start is idempotent", async () => {
    const server = await startEndpoint({});
    const port = server.port;
    await server.start();
    expect(server.port).toBe(port);
  });
});
