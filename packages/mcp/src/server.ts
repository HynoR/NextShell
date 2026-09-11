import { isDeepStrictEqual } from "node:util";
import { z } from "zod";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import {
  CallToolResultSchema,
  type CallToolResult,
  type Tool
} from "@modelcontextprotocol/sdk/types.js";
import { agentToolDefinitions, AGENT_INSTRUCTIONS } from "../../shared/src/mcp-tools";
import { version } from "../package.json";

export const CONNECTION_TIMEOUT_MS = 3_000;
// The app permits a 3600s background command; approvals may take another 5 minutes.
export const CALL_TIMEOUT_MS = 3_910_000;

const failure = (code: string, message: string): CallToolResult => {
  const payload = { ok: false, error: { code, message } };
  return {
    isError: true,
    content: [{ type: "text", text: JSON.stringify(payload) }],
    structuredContent: payload
  };
};

interface Connection {
  client: Client;
  transport: StreamableHTTPClientTransport;
  tools: Tool[];
}

export function createStdioServer(port: number) {
  const server = new McpServer(
    { name: "nextshell", version },
    { instructions: AGENT_INSTRUCTIONS }
  );
  let current: Connection | undefined;
  let connecting: Promise<Connection> | undefined;
  let closed = false;

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
      const transport = new StreamableHTTPClientTransport(new URL(`http://127.0.0.1:${port}/mcp`), {
        // Never follow redirects away from the local application or replay requests.
        fetch: (url, init) =>
          fetch(url, {
            ...init,
            redirect: "error",
            signal: AbortSignal.any([
              ...(init?.signal ? [init.signal] : []),
              ...(init?.method === "DELETE" ? [AbortSignal.timeout(1_000)] : [])
            ])
          }),
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

  for (const [name, definition] of Object.entries(agentToolDefinitions)) {
    const inputSchema = z.object(definition.inputSchema);
    const outputSchema = z.object(definition.outputSchema);
    const expectedInput = z.toJSONSchema(inputSchema, { target: "draft-7", io: "input" });
    const expectedOutput = z.toJSONSchema(outputSchema, { target: "draft-7", io: "output" });
    server.registerTool(name, { ...definition, inputSchema, outputSchema }, async (args, extra) => {
      let connection: Connection;
      try {
        connection = await connect();
      } catch {
        return failure(
          "app_unavailable",
          `无法连接 NextShell。请启动应用、启用 Agent MCP 并检查端口 ${port}；稍后调用会重新连接。`
        );
      }
      const remote = connection.tools.find((tool) => tool.name === name);
      if (
        !remote ||
        !isDeepStrictEqual(remote.inputSchema, expectedInput) ||
        !isDeepStrictEqual(remote.outputSchema, expectedOutput)
      ) {
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
        return failure(
          "connection_lost",
          "调用未完成，执行结果可能未知；请检查 NextShell 中的执行状态，不要自动重复命令。下次调用会重新连接。"
        );
      }
    });
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
