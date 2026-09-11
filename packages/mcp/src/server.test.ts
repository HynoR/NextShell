import { afterEach, describe, expect, test, vi } from "vitest";
import { z } from "zod";
import { createServer } from "node:http";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { McpEndpointServer } from "../../../apps/desktop/src/main/services/mcp/endpoint-server";
import { registerAgentTools } from "../../../apps/desktop/src/main/services/mcp/tools";
import type { AgentGateway } from "../../../apps/desktop/src/main/services/mcp/agent-gateway";
import { agentToolDefinitions } from "../../shared/src/mcp-tools";
import { createStdioServer } from "./server";

const cleanup: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  vi.restoreAllMocks();
  for (const close of cleanup.splice(0).reverse()) await close();
});
const success = (data: Record<string, unknown>): CallToolResult => {
  const structuredContent = { ok: true, data };
  return {
    content: [{ type: "text", text: JSON.stringify(structuredContent) }],
    structuredContent
  };
};
async function harness(port: number) {
  const bridge = createStdioServer(port);
  const [a, b] = InMemoryTransport.createLinkedPair();
  await bridge.server.connect(b);
  const client = new Client({ name: "test-harness", version: "1.2.3" });
  await client.connect(a);
  cleanup.push(async () => {
    await bridge.close();
    await client.close();
    await bridge.server.close();
  });
  return client;
}
function endpoint(
  port = 0,
  invoke: (name: string) => Promise<CallToolResult> = async () =>
    success({ sessions: [], truncated: false })
) {
  const app = new McpEndpointServer({
    port,
    createMcpServer: () => {
      const server = new McpServer({ name: "nextshell", version: "test" });
      for (const [name, definition] of Object.entries(agentToolDefinitions)) {
        server.registerTool(
          name,
          {
            ...definition,
            inputSchema: z.object(definition.inputSchema),
            outputSchema: z.object(definition.outputSchema)
          },
          async () => invoke(name)
        );
      }
      return server;
    }
  });
  cleanup.push(() => app.stop());
  return app;
}

describe("stdio bridge lifecycle", () => {
  test("offline initialization, tools and ping never fetch", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const client = await harness(1);
    expect((await client.listTools()).tools).toHaveLength(Object.keys(agentToolDefinitions).length);
    await client.ping();
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "app_unavailable" } }
    });
    await client.ping();
  });

  test("offline discovery matches the actual desktop registrations", async () => {
    const desktop = new McpServer({ name: "desktop", version: "test" });
    registerAgentTools(desktop, {
      gateway: {} as AgentGateway,
      client: { id: "test", name: "test", version: "1", transport: "http" }
    });
    const [a, b] = InMemoryTransport.createLinkedPair();
    await desktop.connect(b);
    const direct = new Client({ name: "test", version: "1" });
    await direct.connect(a);
    cleanup.push(async () => {
      await direct.close();
      await desktop.close();
    });
    expect(await (await harness(1)).listTools()).toEqual(await direct.listTools());
  });

  test("recovers after app starts and restarts; simultaneous calls share a session", async () => {
    const reservation = endpoint();
    await reservation.start();
    const port = reservation.port!;
    await reservation.stop();
    const app = endpoint(port);
    const client = await harness(port);
    expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
      isError: true
    });
    await app.start();
    const results = await Promise.all([
      client.callTool({ name: "session_list", arguments: {} }),
      client.callTool({ name: "session_list", arguments: {} })
    ]);
    for (const result of results)
      expect(result.structuredContent).toEqual({
        ok: true,
        data: { sessions: [], truncated: false }
      });
    expect(app.getClients()).toHaveLength(1);
    expect(app.getClients()[0]).toMatchObject({ name: "test-harness", version: "1.2.3" });
    // HTTP remains independently usable at the same time.
    const direct = new Client({ name: "direct", version: "1" });
    await direct.connect(new StreamableHTTPClientTransport(new URL(app.url!)));
    cleanup.push(() => direct.close());
    expect((await direct.callTool({ name: "session_list", arguments: {} })).isError).not.toBe(true);
    expect(app.getClients()).toHaveLength(2);
    await app.stop();
    await app.start();
    // If the old session has not observed EOF yet, one call reports the lost session.
    const first = await client.callTool({ name: "session_list", arguments: {} });
    if (first.isError)
      expect(first.structuredContent).toMatchObject({ error: { code: "connection_lost" } });
    expect((await client.callTool({ name: "session_list", arguments: {} })).isError).not.toBe(true);
  });

  test("passes pending and policy errors through without retry", async () => {
    let reply = success({ status: "pending", requestId: "approval-1" });
    const invoke = vi.fn(async () => reply);
    const app = endpoint(0, invoke);
    await app.start();
    const client = await harness(app.port!);
    expect(
      await client.callTool({ name: "exec", arguments: { target: "s", command: "pwd" } })
    ).toEqual(reply);
    for (const code of ["denied", "consent_required", "human_intervention"]) {
      const payload = { ok: false, error: { code, message: "stop" } };
      reply = {
        isError: true,
        content: [{ type: "text", text: JSON.stringify(payload) }],
        structuredContent: payload
      };
      expect(
        await client.callTool({ name: "exec", arguments: { requestId: "approval-1" } })
      ).toEqual(reply);
    }
    expect(invoke).toHaveBeenCalledTimes(4);
  });

  test("an accepted command whose response is lost is never replayed", async () => {
    const invoke = vi.fn(async () => {
      // Simulate the socket disappearing after the app accepted the command.
      setTimeout(() => {
        void app.stop();
      }, 0);
      return new Promise<CallToolResult>(() => {});
    });
    const app = endpoint(0, invoke);
    await app.start();
    const client = await harness(app.port!);
    const result = await client.callTool({
      name: "exec",
      arguments: { target: "s", command: "echo once" }
    });
    expect(result).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "connection_lost" } }
    });
    expect(invoke).toHaveBeenCalledTimes(1);
    await client.listTools();
    expect(invoke).toHaveBeenCalledTimes(1);
  });

  test("mismatched schema prevents submission", async () => {
    const invoke = vi.fn(async () => success({}));
    const app = new McpEndpointServer({
      port: 0,
      createMcpServer: () => {
        const server = new McpServer({ name: "old", version: "0" });
        server.registerTool("session_list", { inputSchema: {} }, invoke);
        return server;
      }
    });
    cleanup.push(() => app.stop());
    await app.start();
    const client = await harness(app.port!);
    expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "incompatible_version" } }
    });
    expect(invoke).not.toHaveBeenCalled();
  });
});

test("an unresponsive app has a bounded handshake, without killing stdio", async () => {
  const stuck = createServer(() => {});
  await new Promise<void>((resolve) => stuck.listen(0, "127.0.0.1", resolve));
  cleanup.push(async () => {
    stuck.closeAllConnections();
    await new Promise<void>((resolve) => stuck.close(() => resolve()));
  });
  const address = stuck.address();
  if (!address || typeof address === "string") throw new Error("Missing test port");
  const client = await harness(address.port);
  const started = Date.now();
  expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
    isError: true,
    structuredContent: { error: { code: "app_unavailable" } }
  });
  expect(Date.now() - started).toBeLessThan(4500);
  await client.ping();
}, 6000);

test("a tool execution outlives the handshake deadline on the same connection", async () => {
  const app = endpoint(0, async () => {
    await new Promise((resolve) => setTimeout(resolve, 3250));
    return success({ sessions: [], truncated: false });
  });
  await app.start();
  const client = await harness(app.port!);
  expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
    structuredContent: { ok: true, data: { sessions: [] } }
  });
  expect(app.getClients()).toHaveLength(1);
}, 6000);
