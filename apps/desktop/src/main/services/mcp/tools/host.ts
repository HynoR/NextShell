import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { toCallToolResult, type AgentToolContext } from "./shared";
import { agentToolDefinitions } from "@nextshell/shared";

/**
 * Discovery beyond the open tabs, on the user's terms: without a query the
 * agent only sees the ten hosts the user connected to most recently, and
 * opening one always goes through an authorization dialog in NextShell.
 */
export const registerHostTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool("host_list", agentToolDefinitions.host_list, async (args) =>
    toCallToolResult(await ctx.gateway.hostList(ctx.client, args))
  );

  server.registerTool("session_open", agentToolDefinitions.session_open, async (args) =>
    toCallToolResult(await ctx.gateway.openSession(ctx.client, args))
  );
};
