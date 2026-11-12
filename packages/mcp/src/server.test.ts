import { afterEach, describe, expect, test, vi } from "vitest";
import { createServer } from "node:http";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { MissingTokenError, SETUP_HINT } from "./credentials";
import { FakeApp, success } from "./fake-app";
import { createStdioServer, type CredentialSource } from "./server";
import { toolDefinitions } from "./tools";

const TOKEN = "tok-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
const cleanup: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  vi.restoreAllMocks();
  for (const close of cleanup.splice(0).reverse()) await close();
});

async function harness(credentials: CredentialSource) {
  const bridge = createStdioServer(credentials);
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
async function app(options: Partial<ConstructorParameters<typeof FakeApp>[0]> = {}) {
  const fake = new FakeApp({ token: () => TOKEN, ...options });
  await fake.start();
  cleanup.push(() => fake.stop());
  return fake;
}
const to =
  (fake: { port: number }, token = TOKEN): CredentialSource =>
  async () => ({ token, port: fake.port });
const call = (client: Client, name = "session_list", args: Record<string, unknown> = {}) =>
  client.callTool({ name, arguments: args });

describe("stdio bridge lifecycle", () => {
  test("offline initialization, tools and ping touch neither the network nor the credentials", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const credentials = vi.fn<CredentialSource>(async () => {
      throw new MissingTokenError("");
    });
    const client = await harness(credentials);
    const { tools } = await client.listTools();
    expect(tools.map((tool) => tool.name).sort()).toEqual(
      toolDefinitions.map((tool) => tool.name).sort()
    );
    expect(tools.every((tool) => tool.description && tool.inputSchema.type === "object")).toBe(
      true
    );
    await client.ping();
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(credentials).not.toHaveBeenCalled();
  });

  test("the offline list is the generated tool metadata, schemas included", async () => {
    const client = await harness(async () => {
      throw new MissingTokenError("");
    });
    const { tools } = await client.listTools();
    for (const tool of tools) {
      const generated = toolDefinitions.find((definition) => definition.name === tool.name)!;
      expect(tool.description).toBe(generated.description);
      expect(tool.inputSchema).toEqual(generated.inputSchema);
      expect(tool.annotations).toMatchObject({ readOnlyHint: generated.annotations.readOnlyHint });
    }
    expect(client.getInstructions()).toContain("session_open");
  });

  test("arguments are forwarded as sent: the app validates, a requestId alone is a valid poll", async () => {
    const invoke = vi.fn(async () => success({ status: "pending" }));
    const fake = await app({ invoke });
    const client = await harness(to(fake));
    await call(client, "exec", { requestId: "req-1" });
    await call(client, "session_read", {
      target: "s1",
      sinceCursor: "42",
      waitFor: { pattern: "re:ok$", timeoutSec: 5 }
    });
    expect(invoke).toHaveBeenNthCalledWith(1, "exec", { requestId: "req-1" });
    expect(invoke).toHaveBeenNthCalledWith(2, "session_read", {
      target: "s1",
      sinceCursor: "42",
      waitFor: { pattern: "re:ok$", timeoutSec: 5 }
    });
  });

  test("a tool the package does not know is reported, not sent", async () => {
    const invoke = vi.fn(async () => success({}));
    const fake = await app({ invoke });
    const client = await harness(to(fake));
    const result = await call(client, "rm_rf_everything");
    expect(result).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "unknown_tool" } }
    });
    expect(invoke).not.toHaveBeenCalled();
  });

  test("every request to the app carries the bearer token", async () => {
    const fake = await app({ invoke: async () => success({ sessions: [] }) });
    const client = await harness(to(fake));
    expect(await call(client)).toMatchObject({ structuredContent: { ok: true } });
    await call(client, "host_list", { query: "x" });
    await client.close();
    await vi.waitFor(() => expect(fake.requests.some((r) => r.method === "DELETE")).toBe(true));
    const methods = new Set(fake.requests.map((r) => r.method));
    expect(methods).toContain("POST");
    expect(methods).toContain("DELETE");
    expect(fake.requests.length).toBeGreaterThanOrEqual(5);
    for (const request of fake.requests) expect(request.authorization).toBe(`Bearer ${TOKEN}`);
  });

  test("a wrong token is reported as unauthorized, without running anything", async () => {
    const invoke = vi.fn(async () => success({}));
    const fake = await app({ invoke });
    const client = await harness(to(fake, "wrong-token"));
    const result = await call(client, "exec", { target: "s", command: "pwd" });
    expect(result).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "unauthorized" } }
    });
    expect(JSON.stringify(result)).not.toContain("wrong-token");
    expect(invoke).not.toHaveBeenCalled();
  });

  test("the token is re-resolved on reconnect, so a rotation heals on the next call", async () => {
    let appToken = "token-one";
    let bridgeToken = "token-one";
    const fake = await app({
      token: () => appToken,
      invoke: async () => success({ sessions: [] })
    });
    const client = await harness(async () => ({ token: bridgeToken, port: fake.port }));
    expect((await call(client)).isError).not.toBe(true);
    appToken = "token-two"; // rotated in the app: sessions are revoked, the old token is refused
    await fake.stop();
    await fake.start();
    expect((await call(client)).isError).toBe(true);
    bridgeToken = "token-two"; // the app rewrote mcp.json
    const healed = await vi.waitFor(async () => {
      const r = await call(client);
      expect(r.isError).not.toBe(true);
      return r;
    });
    expect(healed.structuredContent).toEqual({ ok: true, data: { sessions: [] } });
  });

  test("no token gives a setup hint, never contacts the app and never leaks anything", async () => {
    const fake = await app();
    const client = await harness(async () => {
      throw new MissingTokenError("未能读取 /nowhere/mcp.json。");
    });
    const result = await call(client);
    expect(result).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "token_missing" } }
    });
    expect(JSON.stringify(result)).toContain(SETUP_HINT);
    expect(fake.requests).toHaveLength(0);
  });

  test("recovers once the app starts; unreachable app is app_unavailable", async () => {
    const fake = new FakeApp({ token: () => TOKEN, invoke: async () => success({ sessions: [] }) });
    await fake.start();
    const port = fake.port;
    await fake.stop();
    const client = await harness(async () => ({ token: TOKEN, port }));
    expect(await call(client)).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "app_unavailable" } }
    });
    const again = new FakeApp({
      token: () => TOKEN,
      port,
      invoke: async () => success({ sessions: [] })
    });
    await again.start();
    cleanup.push(() => again.stop());
    expect((await call(client)).structuredContent).toEqual({ ok: true, data: { sessions: [] } });
    // Simultaneous calls share one session.
    await Promise.all([call(client), call(client)]);
    expect(again.sessions.size).toBe(1);
  });

  test("passes pending and policy errors through without retry", async () => {
    let reply = success({ status: "pending", requestId: "approval-1" });
    const invoke = vi.fn(async () => reply);
    const fake = await app({ invoke });
    const client = await harness(to(fake));
    expect(await call(client, "exec", { target: "s", command: "pwd" })).toEqual(reply);
    for (const code of ["denied", "consent_required", "forbidden"]) {
      const payload = { ok: false, error: { code, message: "stop" } };
      reply = {
        isError: true,
        content: [{ type: "text", text: JSON.stringify(payload) }],
        structuredContent: payload
      };
      expect(await call(client, "exec", { requestId: "approval-1" })).toEqual(reply);
    }
    expect(invoke).toHaveBeenCalledTimes(4);
  });

  test("a requestId-only poll is forwarded as is", async () => {
    const invoke = vi.fn(async (_name: string, _args: Record<string, unknown>) => success({}));
    const fake = await app({ invoke });
    const client = await harness(to(fake));
    for (const name of [
      "exec",
      "session_open",
      "command_save",
      "session_send_signal",
      "session_focus"
    ])
      expect((await call(client, name, { requestId: "r1" })).isError).not.toBe(true);
    expect(invoke.mock.calls.map(([, args]) => args)).toEqual(Array(5).fill({ requestId: "r1" }));
  });

  test("an accepted command whose response is lost is never replayed", async () => {
    const invoke = vi.fn(async () => {
      // Simulate the socket disappearing after the app accepted the command.
      setTimeout(() => {
        void fake.stop();
      }, 0);
      return new Promise<CallToolResult>(() => {});
    });
    const fake = await app({ invoke });
    const client = await harness(to(fake));
    const result = await call(client, "exec", { target: "s", command: "echo once" });
    expect(result).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "connection_lost" } }
    });
    expect(invoke).toHaveBeenCalledTimes(1);
    await client.listTools();
    expect(invoke).toHaveBeenCalledTimes(1);
  });

  test("an app without the tool prevents submission", async () => {
    const invoke = vi.fn(async () => success({}));
    const fake = await app({ invoke, tools: ["session_list"] });
    const client = await harness(to(fake));
    expect(await call(client, "exec", { target: "s", command: "pwd" })).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "incompatible_version" } }
    });
    expect(invoke).not.toHaveBeenCalled();
  });

  test("a redirect is refused and the token never reaches the redirect target", async () => {
    const sink = vi.fn();
    const elsewhere = createServer((req) => {
      sink(req.headers.authorization);
    });
    await new Promise<void>((resolve) => elsewhere.listen(0, "127.0.0.1", resolve));
    cleanup.push(async () => {
      elsewhere.closeAllConnections();
      await new Promise<void>((resolve) => elsewhere.close(() => resolve()));
    });
    const target = (elsewhere.address() as { port: number }).port;
    const redirector = createServer((_, res) => {
      res.writeHead(307, { Location: `http://127.0.0.1:${target}/mcp` }).end();
    });
    await new Promise<void>((resolve) => redirector.listen(0, "127.0.0.1", resolve));
    cleanup.push(async () => {
      redirector.closeAllConnections();
      await new Promise<void>((resolve) => redirector.close(() => resolve()));
    });
    const client = await harness(to({ port: (redirector.address() as { port: number }).port }));
    expect(await call(client)).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "app_unavailable" } }
    });
    expect(sink).not.toHaveBeenCalled();
  });

  test("failure payloads and stderr never contain the token", async () => {
    const stderr = vi.spyOn(process.stderr, "write");
    const fake = await app();
    const results = [
      await call(await harness(to(fake, TOKEN + "x"))),
      await call(await harness(async () => ({ token: TOKEN, port: 1 })))
    ];
    for (const result of results) expect(JSON.stringify(result)).not.toContain(TOKEN);
    expect(stderr.mock.calls.flat().join("")).not.toContain(TOKEN);
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
  const client = await harness(to({ port: address.port }));
  const started = Date.now();
  expect(await call(client)).toMatchObject({
    isError: true,
    structuredContent: { error: { code: "app_unavailable" } }
  });
  expect(Date.now() - started).toBeLessThan(4500);
  await client.ping();
}, 6000);

test("a tool execution outlives the handshake deadline on the same connection", async () => {
  const fake = await app({
    invoke: async () => {
      await new Promise((resolve) => setTimeout(resolve, 3250));
      return success({ sessions: [] });
    }
  });
  const client = await harness(to(fake));
  expect(await call(client)).toMatchObject({
    structuredContent: { ok: true, data: { sessions: [] } }
  });
  expect(fake.sessions.size).toBe(1);
}, 6000);
