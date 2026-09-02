import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";

import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { discoverEndpointTargets } from "./endpoint.js";
import { isRecord, type JsonRpcMessage, type JsonRpcResponse } from "./json-rpc.js";
import { BridgeServer } from "./server.js";
import { StdioTransport } from "./stdio.js";
import { UpstreamSession } from "./upstream.js";

const UPSTREAM_TOOLS = {
  tools: [
    { name: "session_list", description: "upstream", inputSchema: { type: "object" } },
    { name: "nextshell_exec", description: "upstream exec", inputSchema: { type: "object" } }
  ]
};

const tempDirs: string[] = [];

const makeTempDir = (): string => {
  // macOS caps sun_path at 104 bytes, so sockets have to live under os.tmpdir().
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nsb-"));
  tempDirs.push(dir);
  return dir;
};

afterAll(() => {
  for (const dir of tempDirs) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

const readBody = (request: http.IncomingMessage): Promise<string> =>
  new Promise((resolve, reject) => {
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk: string) => {
      body += chunk;
    });
    request.on("end", () => resolve(body));
    request.on("error", reject);
  });

interface FakeUpstream {
  socketPath: string;
  requests: Array<{ method: string; headers: http.IncomingHttpHeaders }>;
  close: () => Promise<void>;
}

const startFakeUpstream = async (dir: string): Promise<FakeUpstream> => {
  const socketPath = path.join(dir, "u.sock");
  const requests: Array<{ method: string; headers: http.IncomingHttpHeaders }> = [];

  const server = http.createServer((request, response) => {
    void (async () => {
      const raw = await readBody(request);
      if (request.method === "DELETE") {
        response.writeHead(200).end();
        return;
      }
      let message: Record<string, unknown>;
      try {
        message = JSON.parse(raw) as Record<string, unknown>;
      } catch {
        response.writeHead(400).end();
        return;
      }
      const method = typeof message.method === "string" ? message.method : "";
      requests.push({ method, headers: request.headers });

      if (method === "initialize") {
        response.writeHead(200, {
          "content-type": "application/json",
          "mcp-session-id": "session-1"
        });
        response.end(
          JSON.stringify({
            jsonrpc: "2.0",
            id: message.id,
            result: {
              protocolVersion: "2025-06-18",
              capabilities: { tools: { listChanged: true } },
              serverInfo: { name: "nextshell", version: "0.1.6" }
            }
          })
        );
        return;
      }

      if (message.id === undefined) {
        response.writeHead(202).end();
        return;
      }

      if (method === "tools/list") {
        response.writeHead(200, { "content-type": "text/event-stream" });
        const payload = JSON.stringify({ jsonrpc: "2.0", id: message.id, result: UPSTREAM_TOOLS });
        // Deliberately split mid-frame to exercise the incremental SSE reader.
        response.write(`: keep-alive\nevent: message\ndata: ${payload.slice(0, 12)}`);
        setTimeout(() => {
          response.write(`${payload.slice(12)}\n\n`);
        }, 5);
        return;
      }

      if (method === "tools/call") {
        const params = isRecord(message.params) ? message.params : {};
        response.writeHead(200, { "content-type": "application/json" });
        response.end(
          JSON.stringify({
            jsonrpc: "2.0",
            id: message.id,
            result: { content: [{ type: "text", text: `ran:${String(params.name)}` }] }
          })
        );
        return;
      }

      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          jsonrpc: "2.0",
          id: message.id,
          error: { code: -32601, message: `unknown method ${method}` }
        })
      );
    })();
  });

  await new Promise<void>((resolve) => server.listen({ path: socketPath }, resolve));
  fs.chmodSync(socketPath, 0o600);

  return {
    socketPath,
    requests,
    close: () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve());
      })
  };
};

const writeEndpointFile = (home: string, entries: unknown): void => {
  const dir = path.join(home, ".config", "NextShell", "mcp");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "endpoint.json"), JSON.stringify(entries));
};

describe.skipIf(process.platform === "win32")("bridge over a real unix socket", () => {
  let dir: string;
  let upstream: FakeUpstream;

  beforeEach(async () => {
    dir = makeTempDir();
    upstream = await startFakeUpstream(dir);
  });

  afterEach(async () => {
    await upstream.close();
  });

  const createBridge = (home: string) => {
    const sent: JsonRpcMessage[] = [];
    const server = new BridgeServer({
      send: (message) => sent.push(message),
      log: () => undefined,
      discover: () => discoverEndpointTargets({ platform: "linux", env: { HOME: home } }),
      openSession: (target, options) => UpstreamSession.open(target, options),
      connectTimeoutMs: 2000,
      requestTimeoutMs: 2000,
      callTimeoutMs: 2000
    });
    return { server, sent };
  };

  const lastResult = (sent: JsonRpcMessage[]): Record<string, unknown> => {
    const responses = sent.filter((message) => !("method" in message)) as JsonRpcResponse[];
    const last = responses[responses.length - 1];
    if (last === undefined || !isRecord(last.result)) {
      throw new Error(`expected a result, received ${JSON.stringify(last)}`);
    }
    return last.result;
  };

  it("skips a stale instance, connects to the live one and forwards calls", async () => {
    const home = makeTempDir();
    writeEndpointFile(home, {
      endpoints: [
        {
          pid: 999999,
          socketPath: path.join(dir, "u.sock"),
          updatedAt: Date.now() + 60000
        },
        {
          pid: process.pid,
          socketPath: upstream.socketPath,
          updatedAt: Date.now()
        }
      ]
    });

    const { server, sent } = createBridge(home);
    await server.handleMessage({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: { protocolVersion: "2025-06-18" }
    });
    await server.handleMessage({ jsonrpc: "2.0", method: "notifications/initialized" });

    await vi.waitFor(() => {
      expect(
        sent.some(
          (message) => isRecord(message) && message.method === "notifications/tools/list_changed"
        )
      ).toBe(true);
    });

    await server.handleMessage({ jsonrpc: "2.0", id: 2, method: "tools/list" });
    const tools = lastResult(sent).tools as Array<{ name: string }>;
    expect(tools.map((tool) => tool.name)).toContain("nextshell_exec");

    await server.handleMessage({
      jsonrpc: "2.0",
      id: 3,
      method: "tools/call",
      params: { name: "nextshell_exec", arguments: { command: "uptime" } }
    });
    const content = lastResult(sent).content as Array<{ text: string }>;
    expect(content[0]?.text).toBe("ran:nextshell_exec");

    const initializeRequest = upstream.requests.find((entry) => entry.method === "initialize");
    // Socket authorization is the 0600 file mode; no token is minted or sent.
    expect(initializeRequest?.headers.authorization).toBeUndefined();
    const listRequest = upstream.requests.find((entry) => entry.method === "tools/list");
    expect(listRequest?.headers["mcp-session-id"]).toBe("session-1");
    expect(listRequest?.headers["mcp-protocol-version"]).toBe("2025-06-18");

    await server.close();
  });

  it("keeps serving tools/list after the app goes away and reports the failure per call", async () => {
    const home = makeTempDir();
    writeEndpointFile(home, {
      pid: process.pid,
      socketPath: upstream.socketPath,
      updatedAt: Date.now()
    });

    const { server, sent } = createBridge(home);
    await server.handleMessage({ jsonrpc: "2.0", id: 1, method: "initialize", params: {} });
    await server.handleMessage({ jsonrpc: "2.0", method: "notifications/initialized" });
    await vi.waitFor(() => {
      expect(upstream.requests.some((entry) => entry.method === "tools/list")).toBe(true);
    });

    await upstream.close();
    fs.rmSync(upstream.socketPath, { force: true });

    await server.handleMessage({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: "session_list" }
    });
    const failed = lastResult(sent);
    expect(failed.isError).toBe(true);
    expect(String((failed.content as Array<{ text: string }>)[0]?.text)).toContain("NextShell");

    await server.handleMessage({ jsonrpc: "2.0", id: 3, method: "tools/list" });
    expect((lastResult(sent).tools as Array<{ name: string }>).length).toBeGreaterThan(0);
  });
});

describe("StdioTransport", () => {
  it("frames newline-delimited JSON across chunk boundaries", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const messages: unknown[] = [];
    const parseErrors: string[] = [];
    const transport = new StdioTransport({
      input,
      output,
      onMessage: (message) => messages.push(message),
      onParseError: (line) => parseErrors.push(line)
    });
    transport.start();

    const payload = Buffer.from(
      JSON.stringify({ jsonrpc: "2.0", id: 1, method: "初始化" }),
      "utf8"
    );
    input.write(payload.subarray(0, 20));
    input.write(payload.subarray(20));
    input.write("\n{oops}\n");
    await new Promise((resolve) => setImmediate(resolve));

    expect(messages).toEqual([{ jsonrpc: "2.0", id: 1, method: "初始化" }]);
    expect(parseErrors).toEqual(["{oops}"]);

    transport.send({ jsonrpc: "2.0", id: 1, result: {} });
    expect(output.read()).toEqual(Buffer.from('{"jsonrpc":"2.0","id":1,"result":{}}\n'));
  });
});
