import { describe, expect, it, vi } from "vitest";

import { BRIDGE_NAME } from "./constants.js";
import type { EndpointTarget } from "./endpoint.js";
import { isRecord, type JsonRpcMessage, type JsonRpcResponse } from "./json-rpc.js";
import { BridgeServer, type BridgeServerDeps } from "./server.js";
import { BRIDGE_STATUS_TOOL } from "./tools.js";
import { UpstreamError, UpstreamRpcError, type UpstreamLike } from "./upstream.js";

const socketTarget: EndpointTarget = {
  transport: "socket",
  socketPath: "/tmp/nextshell-secret-path.sock",
  source: "test"
};

class FakeSession implements UpstreamLike {
  readonly transport = "socket";
  readonly serverInfo = { name: "nextshell", version: "9.9.9" };
  readonly calls: Array<{ method: string; params: unknown }> = [];
  closed = false;

  constructor(private readonly handler: (method: string, params: unknown) => Promise<unknown>) {}

  async request(method: string, params?: unknown): Promise<unknown> {
    this.calls.push({ method, params });
    return await this.handler(method, params);
  }

  async close(): Promise<void> {
    this.closed = true;
  }
}

const upstreamTools = {
  tools: [
    { name: "session_list", description: "real", inputSchema: { type: "object" } },
    { name: "nextshell_exec", description: "real exec", inputSchema: { type: "object" } }
  ]
};

interface Harness {
  server: BridgeServer;
  sent: JsonRpcMessage[];
  logs: string[];
  responses: () => JsonRpcResponse[];
}

const createHarness = (overrides: Partial<BridgeServerDeps> = {}): Harness => {
  const sent: JsonRpcMessage[] = [];
  const logs: string[] = [];
  const server = new BridgeServer({
    send: (message) => sent.push(message),
    log: (message) => logs.push(message),
    discover: () => [],
    openSession: async () => {
      throw new UpstreamError("connect ENOENT /tmp/nextshell-secret-path.sock", "UNREACHABLE");
    },
    ...overrides
  });
  return {
    server,
    sent,
    logs,
    responses: () => sent.filter((message) => !("method" in message)) as JsonRpcResponse[]
  };
};

const initialize = async (server: BridgeServer, id = 1): Promise<void> => {
  await server.handleMessage({
    jsonrpc: "2.0",
    id,
    method: "initialize",
    params: {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "t", version: "1" }
    }
  });
};

const resultOf = (message: JsonRpcMessage | undefined): Record<string, unknown> => {
  if (message === undefined || !isRecord(message) || !isRecord(message.result)) {
    throw new Error(`expected a result, received ${JSON.stringify(message)}`);
  }
  return message.result;
};

const textOf = (result: Record<string, unknown>): string => {
  const content = Array.isArray(result.content) ? result.content : [];
  const first = content[0];
  return isRecord(first) && typeof first.text === "string" ? first.text : "";
};

describe("BridgeServer without a running NextShell", () => {
  it("completes initialize on its own", async () => {
    const harness = createHarness();
    await initialize(harness.server);

    const result = resultOf(harness.sent[0]);
    expect(result.protocolVersion).toBe("2025-06-18");
    expect(result.capabilities).toEqual({ tools: { listChanged: true } });
    expect(result.serverInfo).toEqual({ name: BRIDGE_NAME, version: expect.any(String) });
  });

  it("falls back to the latest protocol version for unknown client versions", async () => {
    const harness = createHarness();
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: { protocolVersion: "1999-01-01" }
    });
    expect(resultOf(harness.sent[0]).protocolVersion).toBe("2025-11-25");
  });

  it("answers tools/list from the static manifest", async () => {
    const harness = createHarness();
    await initialize(harness.server);
    await harness.server.handleMessage({ jsonrpc: "2.0", id: 2, method: "tools/list" });

    const tools = resultOf(harness.sent[1]).tools;
    expect(Array.isArray(tools)).toBe(true);
    const names = (tools as Array<{ name: string }>).map((tool) => tool.name);
    expect(names).toContain(BRIDGE_STATUS_TOOL);
    expect(names).toContain("session_list");
  });

  it("returns a tool-level error for tools/call instead of failing the request", async () => {
    const harness = createHarness();
    await initialize(harness.server);
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 3,
      method: "tools/call",
      params: { name: "session_list", arguments: {} }
    });

    const result = resultOf(harness.sent[1]);
    expect(result.isError).toBe(true);
    const text = textOf(result);
    expect(text).toContain("NextShell");
    expect(text).toContain("UNREACHABLE");
    // Endpoint details must never reach stdout.
    expect(text).not.toContain("/tmp/nextshell-secret-path.sock");
  });

  it("reports failure codes without leaking the endpoint through bridge status", async () => {
    const harness = createHarness({
      discover: () => [socketTarget],
      openSession: async () => {
        throw new UpstreamError("connect ECONNREFUSED /tmp/nextshell-secret-path.sock", "REFUSED");
      }
    });
    await initialize(harness.server);
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 4,
      method: "tools/call",
      params: { name: BRIDGE_STATUS_TOOL }
    });

    const text = textOf(resultOf(harness.sent[1]));
    const payload: unknown = JSON.parse(text);
    expect(isRecord(payload) && payload.reachable).toBe(false);
    expect(isRecord(payload) && payload.lastFailure).toBe("REFUSED");
    expect(text).not.toContain("/tmp/nextshell-secret-path.sock");
  });

  it("rejects unknown methods with -32601 and keeps running", async () => {
    const harness = createHarness();
    await initialize(harness.server);
    await harness.server.handleMessage({ jsonrpc: "2.0", id: 5, method: "resources/list" });

    const response = harness.sent[1];
    expect(isRecord(response) && isRecord(response.error) && response.error.code).toBe(-32601);

    await harness.server.handleMessage({ jsonrpc: "2.0", id: 6, method: "ping" });
    expect(resultOf(harness.sent[2])).toEqual({});
  });
});

describe("BridgeServer with a reachable NextShell", () => {
  const connectedHarness = (
    handler: (method: string, params: unknown) => Promise<unknown>
  ): Harness & { session: FakeSession } => {
    const session = new FakeSession(handler);
    const harness = createHarness({
      discover: () => [socketTarget],
      openSession: async () => session
    });
    return { ...harness, session };
  };

  it("replaces the static manifest and notifies the client", async () => {
    const harness = connectedHarness(async (method) =>
      method === "tools/list" ? upstreamTools : {}
    );
    await initialize(harness.server);
    await harness.server.handleMessage({ jsonrpc: "2.0", method: "notifications/initialized" });

    await vi.waitFor(() => {
      expect(
        harness.sent.some(
          (message) => isRecord(message) && message.method === "notifications/tools/list_changed"
        )
      ).toBe(true);
    });

    await harness.server.handleMessage({ jsonrpc: "2.0", id: 2, method: "tools/list" });
    const tools = resultOf(harness.sent[harness.sent.length - 1]).tools as Array<{ name: string }>;
    expect(tools.map((tool) => tool.name)).toEqual([
      BRIDGE_STATUS_TOOL,
      "session_list",
      "nextshell_exec"
    ]);
  });

  it("forwards tools/call and returns the upstream result verbatim", async () => {
    const harness = connectedHarness(async (method, params) => {
      if (method === "tools/list") {
        return upstreamTools;
      }
      return { content: [{ type: "text", text: JSON.stringify(params) }] };
    });
    await initialize(harness.server);
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: "nextshell_exec", arguments: { command: "uptime" } }
    });

    const text = textOf(resultOf(harness.sent[1]));
    expect(JSON.parse(text)).toEqual({
      name: "nextshell_exec",
      arguments: { command: "uptime" }
    });
  });

  it("surfaces an upstream JSON-RPC error as a tool error", async () => {
    const harness = connectedHarness(async (method) => {
      if (method === "tools/list") {
        return upstreamTools;
      }
      throw new UpstreamRpcError({ code: -32000, message: "主机未授权" });
    });
    await initialize(harness.server);
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: "session_list" }
    });

    const result = resultOf(harness.sent[1]);
    expect(result.isError).toBe(true);
    expect(textOf(result)).toContain("主机未授权");
  });

  it("re-dials once when the cached session died", async () => {
    let opened = 0;
    const sessions: FakeSession[] = [];
    const harness = createHarness({
      discover: () => [socketTarget],
      openSession: async () => {
        opened += 1;
        const attempt = opened;
        const session = new FakeSession(async (method) => {
          if (method === "tools/list") {
            return upstreamTools;
          }
          if (attempt === 1) {
            throw new UpstreamError("upstream session is gone", "SESSION_EXPIRED");
          }
          return { content: [{ type: "text", text: "ok" }] };
        });
        sessions.push(session);
        return session;
      }
    });

    await initialize(harness.server);
    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: "session_list" }
    });

    expect(opened).toBe(2);
    expect(sessions[0]?.closed).toBe(true);
    expect(textOf(resultOf(harness.sent[1]))).toBe("ok");
  });

  it("rejects a tool the running app does not expose", async () => {
    const harness = connectedHarness(async (method) =>
      method === "tools/list" ? upstreamTools : {}
    );
    await initialize(harness.server);
    await harness.server.handleMessage({ jsonrpc: "2.0", method: "notifications/initialized" });
    await vi.waitFor(() => {
      expect(harness.session.calls.some((call) => call.method === "tools/list")).toBe(true);
    });

    await harness.server.handleMessage({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: "no_such_tool", arguments: {} }
    });

    const result = resultOf(harness.sent[harness.sent.length - 1]);
    expect(result.isError).toBe(true);
    expect(textOf(result)).toContain("no_such_tool");
  });
});
