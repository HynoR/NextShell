import { z } from "zod";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import {
  CallToolResultSchema,
  type CallToolResult,
  type Tool
} from "@modelcontextprotocol/sdk/types.js";
import { MissingTokenError, SETUP_HINT, type Credentials } from "./credentials";
import { INSTRUCTIONS, toolDefinitions } from "./tools";
import { version } from "../package.json";

export const CONNECTION_TIMEOUT_MS = 3_000;
// The app caps a command at 120s; approvals are polled with short follow-up calls.
export const CALL_TIMEOUT_MS = 130_000;

/** Where the bridge gets its port and bearer token. Called on every (re)connect, never at startup. */
export type CredentialSource = () => Promise<Credentials>;

const unauthorized = () =>
  failure(
    "unauthorized",
    `NextShell 拒绝了 token（可能已被轮换或指向另一个配置）。${SETUP_HINT}，必要时用 --config 指向正确的 mcp.json；下次调用会重新读取。`
  );
function failure(code: string, message: string): CallToolResult {
  const payload = { ok: false, error: { code, message } };
  return {
    isError: true,
    content: [{ type: "text", text: JSON.stringify(payload) }],
    structuredContent: payload
  };
}

interface Connection {
  client: Client;
  transport: StreamableHTTPClientTransport;
  tools: Tool[];
}

export function createStdioServer(credentials: CredentialSource) {
  const server = new McpServer({ name: "nextshell", version }, { instructions: INSTRUCTIONS });
  let current: Connection | undefined;
  let connecting: Promise<Connection> | undefined;
  let closed = false;
  let rejected = false; // the app answered 401: wrong, stale or rotated token
  let port = 0; // last resolved port, for error messages only

  const discard = async (connection: Connection) => {
    if (current === connection) current = undefined;
    await connection.client.close().catch(() => undefined);
  };

  const connect = async (): Promise<Connection> => {
    if (closed) throw new Error("MCP server closed");
    if (connecting) return connecting;
    if (current) return current;
    connecting = (async () => {
      const deadline = AbortSignal.timeout(CONNECTION_TIMEOUT_MS);
      const resolved = await credentials();
      const token = resolved.token;
      port = resolved.port;
      const transport = new StreamableHTTPClientTransport(new URL(`http://127.0.0.1:${port}/mcp`), {
        // Every request carries the token. Never follow redirects away from the local application
        // (the token must not leave it) or replay requests.
        fetch: async (url, init) => {
          const headers = new Headers(init?.headers);
          headers.set("Authorization", `Bearer ${token}`);
          const response = await fetch(url, {
            ...init,
            headers,
            redirect: "error",
            signal: AbortSignal.any([
              ...(init?.signal ? [init.signal] : []),
              ...(init?.method === "DELETE" ? [AbortSignal.timeout(1_000)] : [])
            ])
          });
          if (response.status === 401) rejected = true;
          return response;
        },
        reconnectionOptions: {
          maxRetries: 0,
          initialReconnectionDelay: 1000,
          maxReconnectionDelay: 1000,
          reconnectionDelayGrowFactor: 1
        }
      });
      const client = new Client(
        server.server.getClientVersion() ?? { name: "nextshell-mcp", version }
      );
      const connection: Connection = { client, transport, tools: [] };
      current = connection;
      client.onerror = () => {
        void discard(connection);
      };
      client.onclose = () => {
        if (current === connection) current = undefined;
      };
      const timeout = setTimeout(() => {
        void discard(connection);
      }, CONNECTION_TIMEOUT_MS);
      try {
        await client.connect(transport);
        let cursor: string | undefined;
        do {
          const page = await client.listTools({ cursor }, { signal: deadline });
          connection.tools.push(...page.tools);
          cursor = page.nextCursor;
        } while (cursor);
        if (closed || current !== connection) throw new Error("Connection closed");
        return connection;
      } catch (error) {
        await discard(connection);
        throw error;
      } finally {
        clearTimeout(timeout);
      }
    })();
    try {
      return await connecting;
    } finally {
      connecting = undefined;
    }
  };

  for (const [name, definition] of Object.entries(toolDefinitions)) {
    // No outputSchema: the app's `{ ok, data?, error? }` reply is forwarded exactly as received.
    server.registerTool(
      name,
      { ...definition, inputSchema: z.object(definition.inputSchema) },
      async (args, extra) => {
        rejected = false;
        let connection: Connection;
        try {
          connection = await connect();
        } catch (error) {
          if (error instanceof MissingTokenError) return failure("token_missing", error.message);
          if (rejected) return unauthorized();
          return failure(
            "app_unavailable",
            `无法连接 NextShell。请启动应用、${SETUP_HINT}并检查端口${port ? ` ${port}` : ""}；稍后调用会重新连接。`
          );
        }
        if (!connection.tools.some((tool) => tool.name === name)) {
          return failure(
            "incompatible_version",
            "NextShell 与 MCP 包的工具定义不兼容，请更新应用及 @nextshell/mcp 后重启 MCP。"
          );
        }
        try {
          return CallToolResultSchema.parse(
            await connection.client.callTool({ name, arguments: args }, CallToolResultSchema, {
              timeout: CALL_TIMEOUT_MS,
              signal: extra.signal
            })
          );
        } catch {
          await discard(connection);
          // A 401 means the app refused the request before running anything.
          if (rejected) return unauthorized();
          return failure(
            "connection_lost",
            "调用未完成，执行结果可能未知；请检查 NextShell 中的执行状态，不要自动重复命令。下次调用会重新连接。"
          );
        }
      }
    );
  }

  const close = async () => {
    closed = true;
    const connection = current;
    if (connection) {
      await connection.transport.terminateSession().catch(() => undefined);
      await discard(connection);
    }
  };
  server.server.onclose = () => {
    void close();
  };
  return { server, close };
}
